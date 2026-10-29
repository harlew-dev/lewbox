# TypeScript SDK

> Status: Draft · Milestone: Alpha (M3 core, M4 complete) · Updated: 2026-10-29

`@lewbox/sdk` is the TypeScript and JavaScript client for Lewbox, for Node.js, Bun and Deno, and for browsers with a sandbox key. It has two clients: `Lewbox` (a profile or sandbox key) and `LewboxAdmin` (an admin key). Types are generated from `api/openapi.yaml`; the clients are written by hand so they read well. The dashboard and Lewagent's frontend use its conversation helpers rather than keeping their own.

## Today

- No TypeScript SDK exists. Its design is in `lewbox-legacy/docs/sdks.md` (surface, transport, errors, versioning, examples), and the public docs show a TypeScript example for every operation (`/tmp/lewbox-docs/*.md`, the `typescript` blocks).
- Two partial clients exist instead, each in a frontend:
  - `lewbox-legacy/resources/js/lib/gateway.ts` (313 lines): fetch with sandbox keys; `lib/conversation.ts` (430 lines): `mergeEvent`, `promptText`, `agentOptions`, `summarizeToolCall`.
  - `lewagent-legacy/resources/js/lib/agents.ts` (1282 lines): `ComputerAgents` with key renewal, commands, files, conversations, `followTurn` (resume with `?after=`, reconnect after 20 s of quiet, give up after 3 reconnects without progress), `mergeEvent`, `promptBlocks`; and `lib/git.ts` (2330 lines) running `git` and `gh` through commands.
- The docs' [building a chat page](https://lewbox.com/docs/agents/building-a-chat-page) has a reference implementation of following a turn and merging events, in plain TypeScript.

## Goals

- Every API operation, typed end to end, with names that match the docs.
- No runtime dependencies. Global `fetch` and `WebSocket` (Node 22+, Bun, Deno, browsers).
- Waiting, retrying, paging and streaming handled, with escape hatches.
- **One implementation of the conversation model** (following turns, merging events into chunks), used by the dashboard, Lewagent and anyone building a chat page.
- Tree-shakeable ESM, with CommonJS for older tooling; small enough for a browser bundle.

## Design

### Package

```
sdks/typescript/
  package.json              "@lewbox/sdk", ESM + CJS, "exports" for "." and "./webhooks"
  src/
    generated/schema.d.ts   openapi-typescript output (not edited)
    transport.ts            fetch, retries, idempotency, errors, timeouts, user agent
    errors.ts               LewboxError and subclasses
    pagination.ts           Page<T>, async iteration over links.next
    operations.ts           waitForOperation
    sse.ts                  server-sent events parser (UTF-8 safe across chunks), resume
    websocket.ts            lewbox.bearer subprotocol, frames, reconnect
    lewbox.ts               class Lewbox
    admin.ts                class LewboxAdmin
    resources/              sandboxes, commands, sessions, files, ports, ssh, keys,
                            activity, conversations, templates, profile, repositories
    conversations/          follow, mergeEvent, chunks, replies
    webhooks.ts             Webhook.verify
  test/
```

Built with `tsup` (or `tsdown`) to `dist/` as ESM and CJS with `.d.ts`. Published to npm from CI on a `sdk-typescript/v*` tag.

### Clients

```ts
import { Lewbox, LewboxAdmin } from '@lewbox/sdk';

const lewbox = new Lewbox({
  apiKey: process.env.LEWBOX_API_KEY,   // lbp_… or lbs_…; default from env
  apiUrl: 'https://api.lewbox.com/v0',  // default from LEWBOX_API_URL
  cluster: 'eu-west',                   // default from LEWBOX_CLUSTER
  timeoutMs: 30_000,
  waitTimeoutMs: 600_000,
  maxRetries: 3,
  fetch: customFetch,                   // optional, for proxies and tests
});

const admin = new LewboxAdmin({ adminKey: process.env.LEWBOX_ADMIN_KEY });
```

- Each constructor checks its key's prefix and throws a `LewboxError` with `wrong_key_kind` for the other kind.
- A **sandbox key** client (`lbs_`) can reach only `sandboxes.get(id)` and what's inside that sandbox; the rest throws without a request.
- `getKey`, instead of `apiKey`: an async function the client calls when it needs a key, and again with `{ renew: true }` after a `401`. For browsers with short-lived sandbox keys, so the page never handles expiry itself (what Lewagent's `getKey(renew)` does today).

### Surface

Names follow the docs (`/tmp/lewbox-docs`), which follow `lewbox-legacy/docs/sdks.md`:

| Area | Methods |
|---|---|
| Sandboxes | `lewbox.sandboxes.create(opts, { idempotencyKey? })` → `Sandbox`; `.get(id)`; `.list({ status, cluster, metadata, perPage })` (async iterable) |
| Hooks | `create({ …, hooks: { onCreate, onStart, onStop }, stopGraceMs, scrub, warnBeforeMs })` (each hook a string or `{ command, directory, env, timeoutMs, background, required, label }`); `sandbox.update({ hooks, stopGraceMs, scrub })`; `sandbox.hooks` (the report); `sandbox.touch()` ([sandboxes/hooks](../sandboxes/hooks.md)) |
| A sandbox | `sandbox.refresh()`, `.start()`, `.stop()`, `.restart()`, `.kill()` (each `{ wait }`), `.update({ metadata, idleTimeoutMs, lifetimeMs })`, `.waitUntilRunning()`, `.getUrl(port)`; fields as camelCase: `id`, `status`, `cluster`, `memoryMb`, `operation`, `ports`, `setup`, `repositories` |
| Commands | `sandbox.commands.run(cmd, { directory, env, timeoutMs, label, onStdout, onStderr, throwOnExit, signal })` → `{ exitCode, stdout, stderr }`; `.stream(cmd, { pty, cols, rows, onData })` → handle with `sendStdin`, `closeStdin`, `resize`, `signal`, `wait`, `kill` |
| Sessions | `sandbox.sessions.create({ command, directory, env, pty, cols, rows, watch })`, `.list()`, `.get(id)`, `.log(id, { tail })`, `.attach(id, { onData })` → handle, `.resize`, `.signal`, `.kill` |
| Files | `sandbox.files.read(path, { format: 'text' | 'bytes' | 'stream' })`, `.write(path, data | ReadableStream | Blob)`, `.list(path, { depth })`, `.stat(path)`, `.exists(path)`, `.makeDir(path)`, `.rename(from, to)`, `.remove(path)` |
| Ports | `sandbox.ports.expose(port, { protocol, access, persist })` → `Port`, `.close(port)`, `.list()`, `.link(port, { path })` → `{ url, expiresAt, sessionExpiresAt }` |
| SSH keys | `sandbox.ssh.list()`, `.add(name, publicKey)`, `.remove(name)` |
| Sandbox keys | `sandbox.keys.create({ permissions })` → `{ token, expiresAt }` (profile key only) |
| Activity | `sandbox.activity.list(filters)`, `.record({ kind: 'app.…', … })`; `lewbox.activity(filters)` |
| Agents | `sandbox.agents()` → models, modes, thinking levels; `sandbox.prompt(text | blocks, opts)` → `Conversation`; `sandbox.resume(id, text, opts)` |
| Conversations | `lewbox.conversations.list(filters)`, `.get(id)`; `conversation.prompt(text, { queue, steer, label, outputSchema })`, `.events({ turn, after, types })` (async iterable), `.follow(turn, handlers)`, `.output()`, `.reply(request, { option | answers | decline })`, `.cancel(turn?)`, `.steer(turn)`, `.queued()`, `.messages({ after })`, `.tasks()`, `.stopTask(id)`, `.delete({ force, keepWorktree })` |
| Templates | `lewbox.templates.create({ id, name, dockerfile, clusters, ports })`, `.list({ cluster })`, `.get(id)`, `.rebuild(id, { dockerfile })`, `.delete(id)`, `template.waitUntilReady({ cluster })` |
| Profile | `lewbox.profile.get()`, `.update({ defaultCluster })`; `lewbox.repositories.list({ search })`, `.branches(owner, repo)` |
| Admin | `admin.profiles.*` (CRUD, suspend, `apiKeys(id)`, `limits(id)`, `agents(id)`, `git(id)`, `telemetry(id)`, `sshKeys(id)`, `setupLinks(id)`), `admin.clusters.*`, `admin.nodes.*`, `admin.sandboxes.*`, `admin.templates.*`, `admin.webhookEndpoints.*`, `admin.activity(filters)`, `admin.plugins.list()`, `.enable(name)`, `.disable(name)`; `admin.credits.*` in Beta (the `credits` plugin) |
| Plugins | Operations grouped under their plugin's name ([sdks-and-cli](../sdks-and-cli.md#plugins)); `lewbox.plugins()` from `/v0/meta`; `PluginDisabledError`; `lewbox.request(method, path, body)` for third-party plugins' routes |
| Webhooks | `import { Webhook } from '@lewbox/sdk/webhooks'`; `Webhook.verify(body, headers, secret, { toleranceMs })` → typed event |

Exact paths and fields follow the final API ([api](../api.md)); method names are the stable part.

### Transport

- **Requests:** `fetch` with `Authorization: Bearer`, `Accept: application/json`, `User-Agent: lewbox-typescript/<v> (node 22.8; linux x64)` where the runtime allows setting it (not in browsers: `X-Lewbox-Client` instead).
- **Responses:** unwrap `data`; lists return `Page<T>` with `items`, `next()` and async iteration; camelCase on the way out, snake_case on the way in, by a mapping generated from the schema (not a blind recursive converter, which would mangle `metadata` and `env` keys).
- **Retries:** up to `maxRetries` on network errors and `429`, `502`, `503`, `504`, with exponential backoff and jitter (0.5 s, 1 s, 2 s, capped at 8 s), honouring `Retry-After`. Only for `GET`, `PUT`, `DELETE`, `PATCH` that are idempotent by the spec, and `POST` with an `Idempotency-Key`. Never for `429` `limit_exceeded` (as the PHP SDK's `FINAL_CODES` do).
- **Idempotency:** every create (`sandboxes.create`, `templates.create`, `conversations` start, `prompt`, `activity.record`) sends an `Idempotency-Key` from `crypto.randomUUID()`, kept across retries; callers may pass their own.
- **Timeouts:** `timeoutMs` per request via `AbortSignal.timeout`, combined with the caller's `signal`. Streams have no overall timeout, only keepalive checks.
- **Errors** are classes ([Errors](#errors)), each with `status`, `code`, `message`, `requestId` and the body.

### Waiting

Hooks and scrub paths, as the user first sketched them (`execute: {onStart, onStop}`), in the SDK's words:

```ts
const sandbox = await lewbox.sandboxes.create({
  template: 'claude-code',
  persist: true,
  hooks: {
    onStart: [{ command: 'npm run dev', directory: '~/workspace/web', background: true }],
    onStop: ['rm -f ~/project/.env'],
  },
  scrub: ['~/.npmrc'],
})
```

`create()` sends `wait: true` and uses the API's own wait (up to 60 s). On a `202`, it polls the operation (1 s, backing off to 5 s) until `succeeded` or `failed`, up to `waitTimeoutMs`. `start`, `stop`, `restart` and `kill` do the same. A failed operation throws `OperationFailedError` with the operation; running out throws `TimeoutError` and leaves the operation running. `{ wait: false }` returns the `Operation` at once. When a call meets `operation_in_progress`, waiting calls wait for that operation first, then retry once.

If the API adds operation events to the activity stream or a long-poll (`GET …/operations/{op}?wait=30`), the waiter uses that and stops polling ([sandboxes/operations](../sandboxes/operations.md)).

### Streams

- **SSE** (`commands.run` with `Accept: text/event-stream`, a turn's events, activity follow): a parser that handles events split across chunks and UTF-8 sequences split across reads (the gateway's `internal/server/sse.go` decodes on boundaries; the client must too), comments as keepalives, `id:` for resume.
- **Resume:** reconnect with `Last-Event-ID` where the runtime can set headers (Node, Bun, Deno), else with `?after=` (browsers, cross-origin), which the API accepts for that reason.
- **WebSockets** (`commands.stream`, `sessions.attach`): connect with subprotocols `['lewbox.bearer', key]` (browsers can't set headers on a WebSocket), binary frames for stdin and output (`0x01` stdout, `0x02` stderr prefixes on the stream endpoint), JSON text frames for control (`resize`, `signal`, `close`) and the end (`exit`, `error`). Close codes map to errors (1011 upstream ended, 1001 server restarting, 1008 bad start message) ([api/streaming](../api/streaming.md)).

### Conversations

The part both dashboards wrote twice. The SDK owns it now:

- **`mergeEvent(chunks, event)`**: folds a live event into the stored chunk model (`text` and `reasoning` appended to the open chunk; `tool_call` and `tool_output` updating the call's chunk by its id; `plan` replacing; `approval_request`, `question` and `answered` updating the request; `error` appended), so a turn renders the same live and stored. Ported from `lewbox-legacy/resources/js/lib/conversation.ts` and `lewagent-legacy/resources/js/lib/agents.ts`, reconciled, with recorded fixtures from both.
- **`conversation.follow(turn, handlers)`**: follows a turn to its end through drops: resumes after the last event id; reopens a stream that's been silent for 20 s, as one can stop without closing; after 3 reconnects without new events, reads the conversation and its messages and finishes from those; handles `stream_interrupted` and `server_restarting` (was `gateway_restarting`) by reconnecting at once. Handlers: `onEvent`, `onChunks(chunks)`, `onRequest` (approvals and questions), `onFinished(message)`, `onError`.
- **`conversation.output()`**: waits for a turn with an `outputSchema` and returns its parsed JSON.
- **Peer and agent-started turns:** `conversation.watch({ onTurn })` polls `messages({ after })` while idle, until the API announces them on a stream ([agents/turns-and-events](../agents/turns-and-events.md)).
- **Helpers for UIs:** `summarizeToolCall(chunk)` (title, brief input, diff for edits), `promptText(blocks)`, `promptBlocks(message)`, `agentOptions(agents, agent)`. These carry no React; the [dashboard's chat](../dashboard/sandbox-pages.md#the-chat) builds on them.

### Browsers

The SDK works in browsers with a **sandbox key** (`lbs_`), which reaches one sandbox for 15 minutes and is made by the app's server. That's how the docs' chat page and Lewagent's frontend work.

- `new Lewbox({ apiUrl, getKey })`, with `getKey` fetching a fresh sandbox key from the app's own endpoint. The client renews before expiry and after a `401`.
- No Node-only APIs in the default entry; `files.read({ format: 'stream' })` returns a web `ReadableStream`.
- A profile key in a browser isn't blocked (we can't tell), but the docs say not to, and the constructor warns once in the console when it sees an `lbp_` key with `window` defined.

### Errors

| Response | Class | Carries |
|---|---|---|
| Any `401` | `AuthenticationError` | |
| `403` `permission_denied` | `PermissionDeniedError` | the permission, in the message |
| `404`; on a sandbox | `NotFoundError`; `SandboxNotFoundError` | `id` |
| `409` `operation_in_progress` | `OperationInProgressError` | `operation` |
| Other `409` | `ConflictError` | `code` |
| Any `422` | `ValidationError` | `errors` |
| `429` `limit_exceeded` | `LimitExceededError` | `limit` |
| `429` `rate_limited` | `RateLimitError` | `retryAfterMs` |
| `503` `cluster_unavailable` | `ClusterUnavailableError` | `cluster` |
| Other `5xx` | `ServerError` | |
| A failed operation | `OperationFailedError` | `operation` with `step`, `error`, `errorCode` |
| Non-zero exit with `throwOnExit` | `CommandExitError` | `exitCode`, `stdout`, `stderr` |
| A client-side timeout | `TimeoutError` | which |
| A bad webhook | `WebhookVerificationError` | |

All extend `LewboxError`. A code the SDK doesn't know is still a `LewboxError` with its `code`, so new codes don't break anything.

### Webhooks

`Webhook.verify(rawBody, headers, secret, { toleranceMs = 300_000 })` checks the Standard Webhooks signature (`webhook-id`, `webhook-timestamp`, `webhook-signature`; HMAC-SHA256 over `id.timestamp.body`, base64; several `v1,` signatures accepted during a secret roll), in constant time, and the timestamp. Returns a typed `WebhookEvent` (`id`, `type`, `createdAt`, `data`), with `event.sandbox()` for `sandbox.*` events and `event.activityEvent()` for `activity.*`. Uses Web Crypto, so it runs on Node, Deno, Bun and edge runtimes. Event types and payloads come from [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md).

## Testing

- **Unit** (Vitest): transport retries and `Retry-After`; `POST` retry rules; every error mapping; pagination; SSE split across chunks and inside UTF-8 sequences; key renewal on `401`; camelCase mapping leaves `metadata` keys alone.
- **Fixtures:** the shared recorded exchanges ([sdks-and-cli](../sdks-and-cli.md#testing-shared)), including recorded turns from both agents for `mergeEvent` and `follow`, with drops inserted.
- **Contract:** against `lewbox serve --fake-cluster` in CI, on Node 22, Bun and Deno.
- **Browser:** Playwright runs a page that uses a sandbox key against the fake server: commands, files, a terminal over WebSocket, a turn followed through a dropped connection.
- **Types:** `tsd` tests that the public types don't change by accident; `publint` and `arethetypeswrong` on the package.

## Build plan

1. Package scaffold, generated types, transport, errors, pagination. (M3)
2. Sandboxes, operations and waiting; commands (SSE); files (streams). (M3)
3. Sessions and `commands.stream` (WebSocket); ports; SSH; sandbox keys; activity. (M3)
4. Conversations: start, prompt, events, `mergeEvent`, `follow`, replies, tasks; fixtures from both dashboards. (M3 to M4)
5. Templates, profile, repositories. (M4)
6. `LewboxAdmin`. (M4)
7. Webhooks. (M4)
8. Docs examples tested; first npm release with the alpha. (M4)

## Open questions

1. **Admin in the same package?** (Proposed: yes; tree-shaking drops it from browser bundles.)
2. **camelCase mapping:** generate a key map from the schema, or keep snake_case in the SDK's types like Stripe does? The docs already promise camelCase.
3. **Python next?** Its shape would follow this SDK closely.
4. **Should `@lewbox/sdk` include React hooks** (`useConversation`, `useTerminal`), or keep those in `@lewbox/react` ([dashboard/stack-and-structure](../dashboard/stack-and-structure.md#shared-components))? (Proposed: separate package.)
