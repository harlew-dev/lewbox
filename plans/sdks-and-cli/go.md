# Go client

> Status: Draft · Milestone: Alpha (M0 transport, M2 to M4 surface) · Updated: 2026-10-29

`github.com/harlew-dev/lewbox/client` is a public Go package for the Lewbox API. The [CLI](cli.md) is built on it, the server's end-to-end tests drive the server through it, and Go programs that run agents can import it. It's the Go SDK, in the same module as the server, so it changes in the same pull request as the API.

## Today

Nothing public. The gateway has a Go client for the *app's internal* API (`lewbox-gateway-legacy/internal/lewbox/client.go`, about 50 methods) and one for AerolVM (`internal/aerolvm`), both internal. The server tests drive the gateway with `net/http/httptest` requests built by hand.

## Goals

- A typed, idiomatic Go client for the whole API: `context.Context` everywhere, `io.Reader` and `io.Writer` for files and streams, iterators for pages, errors you can `errors.As`.
- No dependencies beyond the standard library and `coder/websocket` (which the server uses already).
- The CLI needs nothing the client doesn't offer.

### Non-goals

- A separate module or repository. It's a package in the main module (`github.com/harlew-dev/lewbox/client`), importable on its own; Go's module graph keeps importers from compiling the server. If the server's dependencies ever make importing it heavy, it moves to its own module (`…/lewbox/client/v1`) then.

## Design

### Package layout

```
client/
  client.go         Client, New, options, the transport
  admin.go          AdminClient, NewAdmin
  errors.go         *Error and sentinel codes
  pages.go          Iterator[T] (iter.Seq2[T, error])
  operations.go     Wait
  sse.go            event stream reader with resume
  ws.go             WebSocket dial with lewbox.bearer
  types.go          request and response types, generated from the spec
  sandboxes.go commands.go sessions.go files.go ports.go ssh.go keys.go
  activity.go conversations.go templates.go profile.go repositories.go
  admin_*.go        profiles, clusters, nodes, installations, webhooks, …
  webhook/          Verify, Event types
```

### Types

Generated from `api/openapi.yaml` with `oapi-codegen` (types only, no client) into `types.go`, then the methods are written by hand ([api/openapi-and-codegen](../api/openapi-and-codegen.md)). Fields use Go names with JSON tags in snake_case. Optional fields are pointers; `null`-able but required fields use a small `Nullable[T]`, since the API distinguishes `null` from absent in `PATCH` (`idle_timeout: null` means "the limit") ([api/conventions](../api/conventions.md)).

Durations are `time.Duration` in Go, converted to the API's seconds.

### Clients

```go
c, err := client.New(
    client.WithAPIKey(os.Getenv("LEWBOX_API_KEY")),   // default: env
    client.WithBaseURL("https://api.lewbox.com/v0"),  // default: LEWBOX_API_URL
    client.WithCluster("eu-west"),                    // default: LEWBOX_CLUSTER
    client.WithHTTPClient(hc),
    client.WithRetries(3),
)

sbx, err := c.Sandboxes.Create(ctx, client.SandboxCreate{
    Template: "claude-code",
    Persist:  true,
    Metadata: map[string]string{"task": "42"},
})

res, err := sbx.Commands.Run(ctx, "npm test", client.RunOptions{
    Directory: "/root/workspace",
    Stdout:    os.Stdout,
    Stderr:    os.Stderr,
})
fmt.Println(res.ExitCode)

err = sbx.Stop(ctx) // waits for the operation
```

- `Client` services: `Profile`, `Sandboxes`, `Conversations`, `Templates`, `Repositories`, `Activity`, `Operations`. A `*Sandbox` value carries the resource's fields and handles for `Commands`, `Sessions`, `Files`, `Ports`, `SSH`, `Keys`, `Activity`, `Conversations`, `Agents`.
- `AdminClient` (`client.NewAdmin(client.WithAdminKey(…))`): `Profiles` (and their keys, limits, agents, Git, telemetry, SSH keys, setup links), `Clusters`, `Installations`, `Nodes`, `Sandboxes`, `Templates`, `Webhooks`, `Activity`, `Keys`.
- Both refuse the wrong key prefix in `New`.

### Transport

- `Authorization: Bearer`, `User-Agent: lewbox-go/<version> (go1.27; linux/amd64)`, `Accept: application/json`.
- **Retries** like every SDK: network errors, `429`, `502`, `503`, `504`, with backoff and jitter, honouring `Retry-After`; safe methods and `POST` with an `Idempotency-Key` only; never `limit_exceeded`. Request bodies are buffered, or re-opened via `GetBody`, so retries resend them; streamed uploads aren't retried.
- **Idempotency keys** on creates, from `crypto/rand`, or `client.WithIdempotencyKey(ctx, key)`.
- **Request ids:** every `*Error` has `RequestID`; `client.RequestID(resp)` for successful calls too, for logs.
- **Hooks:** `client.WithRequestHook(func(*http.Request))` and `WithResponseHook`, used by the CLI's `--debug` (with redaction) and by tests to record fixtures.

### Pages, waiting, streams

- **Pages:** `for sbx, err := range c.Sandboxes.List(ctx, client.SandboxFilter{Status: "running"}) { … }`, an `iter.Seq2[*Sandbox, error]` that follows `links.next`. `client.Collect(it, limit)` for the CLI's `--limit`.
- **Waiting:** lifecycle calls wait by default (polling 1 s → 5 s, until `ctx` ends or `WithWaitTimeout`), returning `*OperationFailedError` for a failed operation. `client.NoWait` as an option returns the `*Operation` at once; `c.Operations.Wait(ctx, sbx, op)` waits later.
- **Command output:** `Commands.Run` reads SSE and writes to the `Stdout` and `Stderr` writers as bytes arrive; returns `ExitCode`, and the captured output only when no writers were given.
- **Interactive:** `Commands.Stream(ctx, cmd, StreamOptions{PTY: true, Cols, Rows})` returns a `*Stream` with `Stdin io.WriteCloser`, `Stdout io.Reader`, `Stderr io.Reader`, `Resize(cols, rows)`, `Signal(sig)`, `Wait() (int, error)`. `Sessions.Attach` returns the same shape. The CLI's `shell` is `Stream` plus raw mode.
- **Files:** `Files.Read(ctx, path) (io.ReadCloser, error)`, `Files.Write(ctx, path, io.Reader)`, streamed both ways.
- **Turn events:** `Conversations.Events(ctx, cnv, turn, after)` → `iter.Seq2[Event, error]`, resuming with `Last-Event-ID` across drops; `Conversations.Follow` with the same reconnect rules as the TypeScript SDK ([typescript](typescript.md#conversations)). Events are a tagged union (`Event.Type` plus typed payload accessors).

### Errors

```go
var apiErr *client.Error
if errors.As(err, &apiErr) && apiErr.Code == client.CodeOperationInProgress { … }
```

`*Error` has `Status`, `Code`, `Message`, `Errors map[string][]string`, `RequestID`, and the extra fields some codes carry (`Operation`, `Limit`, `Template`, `Cluster`, `SandboxStatus`). Codes are typed string constants generated from the spec's error code list ([api/errors](../api/errors.md)). `client.IsNotFound(err)`, `IsRetryable(err)`, `IsPluginDisabled(err)` (with `Plugin` on the error: name and scope) helpers. A plugin's operations are under its name (`admin.Credits`), and `client.Do(ctx, method, path, body, out)` reaches routes the client doesn't know, such as a third-party plugin's ([sdks-and-cli](../sdks-and-cli.md#plugins)). Waiting returns `*OperationFailedError` and `context.DeadlineExceeded`.

### Webhooks

`webhook.Verify(body []byte, header http.Header, secret string, opts ...Option) (*webhook.Event, error)` with Standard Webhooks rules, constant-time comparison, several signatures during a roll, and a 5-minute tolerance. The server's webhook sender uses the same package's `Sign` in its tests, so signing and verifying can't disagree.

### Used by the server's tests

The server's end-to-end tests ([foundation/testing](../foundation/testing.md)) start `lewbox serve` in-process with the fake cluster and drive it with this client. That makes the client the most-tested SDK, and any API change that breaks it fails the server's own CI.

## Testing

- Unit tests with `httptest.Server` for retries, errors, pages, SSE resume, and WebSocket frames.
- Recorded fixtures, as the other SDKs ([sdks-and-cli](../sdks-and-cli.md#testing-shared)); this client's response hook is what records them.
- `go vet`, `staticcheck`, and an API compatibility check (`gorelease` or `apidiff`) on the `client` package from the first tagged release.

## Build plan

1. Transport, errors, pages, generated types, `Profile`, `Sandboxes`, `Operations`. (M0 to M1)
2. Commands (SSE), files, sessions and streams (WebSocket), ports, SSH, keys. (M2)
3. Conversations, events, follow; agents. (M3)
4. Templates, repositories, activity; `AdminClient`; webhooks. (M4)
5. Docs: a Go tab in the docs' examples, after the alpha.

## Open questions

1. **Same module or separate?** (Proposed: same module, package `client`, until it hurts.)
2. **Generated or hand-written types?** `oapi-codegen` handles OpenAPI 3.1 only partly; if it struggles, we write the types by hand and check them against the spec in CI.
3. **Show Go in the docs** next to cURL, CLI, TypeScript and PHP, from the alpha?
