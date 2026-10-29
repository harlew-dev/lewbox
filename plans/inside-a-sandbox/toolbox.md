# Toolbox

> Status: Draft · Milestone: Alpha (M2), fork fixes through Beta · Updated: 2026-10-29

The toolbox is AerolVM's daemon inside every sandbox, `toolboxd`. It's bind-mounted into each container at `/usr/local/bin/toolboxd`, runs as process 1, listens on port 2280, and starts the image's own entrypoint and command as its child. It runs commands, sessions and file operations, and AerolVM's node API proxies to it under `/v1/sandboxes/{id}/toolbox/…` and `/v1/sandboxes/{id}/sessions/…`. This plan covers Lewbox's client for it (`internal/aerolvm`), the request path every endpoint inside a sandbox shares, and which toolbox problems we fix in our [AerolVM fork](../clusters/aerolvm-fork.md) instead of around.

## Today

- **Client:** `lewbox-gateway-legacy/internal/aerolvm/toolbox.go` (376 lines), with the node's API client in `client.go`. It authenticates with the cluster's PAT, and dials WebSockets with `coder/websocket`, the PAT in `Authorization`.
- **Request path:** `lewbox-gateway-legacy/internal/server/inside.go`:
  - `runningSandbox` loads the sandbox from the app with the consumer's key, requires `running`, and finds its node.
  - `withToolbox` retries while the toolbox warms up: 250 ms doubling to 2 s, for up to `GATEWAY_TOOLBOX_WAIT` (30 s).
  - `writeAerolVMError` maps errors.
  - `loginShell` wraps commands, `workingDirectory` resolves `~`, `missingDirectory` checks a directory exists before running in it.
  - `activity` touches the sandbox while streams move.
- **Reference:** `lewbox-gateway-legacy/docs/aerolvm.md` § *Inside a sandbox* and § *Known upstream issues*, against `aerol-ai/microvm` at `cf5173d` (v0.6.x). Clusters are pinned to `v0.6.4`, as `v0.6.5` has no release assets.

### The routes Lewbox uses

All on the node that runs the sandbox, with the PAT.

| Route | Use |
|---|---|
| `POST …/toolbox/process/execute` | Run to completion with `bash -lc`. Takes `command`, `workdir`, `env`, `timeout_seconds` (5 minutes when 0). Answers `stdout`, `stderr`, `exit_code`, `duration_ms`, holding output in memory until the command ends |
| `GET …/toolbox/process/exec/stream` | WebSocket: run one command with `/bin/sh -c`, interactively |
| `POST, GET …/sessions`; `GET, DELETE …/sessions/{id}` | Create (`name`, `command` or a login shell, `workdir`, `env`, `pty`, `cols`, `rows`), list (`{"sessions": […]}`), get, kill and forget |
| `POST …/sessions/{id}/resize`, `…/signal` | `{cols, rows}`; `{signal}` of `INT`, `TERM`, `KILL`, `HUP`, `QUIT` |
| `GET …/sessions/{id}/attach`, `…/log` | Follow a session over a WebSocket; read its buffered output (1 MiB, `SB_SESSION_BUFFER_BYTES`) |
| `GET, POST …/toolbox/envd/files?path=` | Download; upload a raw `application/octet-stream` body, streamed to disk, making directories |
| `POST …/toolbox/envd/filesystem.Filesystem/{Stat,ListDir,MakeDir,Move,Remove}` | JSON in and out |
| `GET …/toolbox/health` | Answers `ok`, and counts as activity: the "touch" |
| `POST, DELETE …/ports/{port}` | Expose (`http` or `tcp`) and stop exposing. These belong to AerolVM, not the toolbox, but go to the same node |

Unused but present: Daytona-style `toolbox/files/*` (search, find), `git/…`, envd's `process.Process/…` and `Filesystem/WatchDir`.

### Problems, and where each was seen

| # | Problem | Seen | Legacy workaround |
|---|---|---|---|
| T1 | **Exit statuses lost under gVisor.** The toolbox, as init, reaps exited children, and can reap a command before it waits for it. It recovers from `ECHILD`, but gVisor answers `ESRCH`, so a command that ran answers `exit_code: -1` and `waitid: no such process`. Still so in `v0.6.8` | A restarted desktop sandbox's setup and its private port proxy failed | `ExitStatusTrap` before every Execute writes `__lewbox_exit_status=N` to stderr, parsed back out (`febb176`). Sessions write theirs to `/root/.lewbox/sessions/<ses_id>` (`cf9813a`, `590bbc2`). `chmod` checked by stat'ing the mode (`4f0da62`). **Exec streams aren't fixed**, and login-shell sessions keep `-1` |
| T2 | **Session names aren't unique.** Every create starts a new session, even with a running one's name | A second private-port proxy was started, couldn't bind, and its failed expose stopped the working one (`5f3d19e`, Lewagent's `sbx_S2NLeK88raX9nw`) | List sessions by name before starting one that must run once |
| T3 | **Activity counts only when a request starts.** A long stream doesn't keep the sandbox awake | | Touch `toolbox/health` at most every `GATEWAY_ACTIVITY_INTERVAL` (60 s) while data moves |
| T4 | **Downloads are read whole into memory** (`os.ReadFile`) | | Documented as a limit |
| T5 | **A dropped exec stream leaves its command running** | | Send `KILL` before closing |
| T6 | **No pings** on the toolbox's WebSockets | | Lewbox pings its own client side only |
| T7 | **502/503 for a few seconds after a start:** the proxy's plain-text `toolbox unavailable`, or the toolbox's `sandbox not adopted` | Every start | `withToolbox` retries |
| T8 | **Three error shapes:** AerolVM's `{"error"}`, envd's `{"code", "message"}`, the proxy's plain text | | `writeAerolVMError` matches each |
| T9 | **A session's end isn't pushed.** It's only seen by reading the session | | Watched sessions polled every 5 s, which itself counts as activity |
| T10 | **Exec stream runs `/bin/sh -c`**, not a login shell | | `loginShell` wraps the command |
| T11 | **No conditional writes or file events** exposed | Lewagent's editor (`lewagent-legacy/docs/editor.md`) | None: it checks `modified_at` first, and loses a race |

## Goals

- One client, `internal/aerolvm`, for the node API and the toolbox, with every route above typed.
- One request path for every endpoint inside a sandbox, so each handler only does its own work.
- Each toolbox problem fixed in the fork, with a pull request upstream. **Lewbox carries no workaround** for any of them ([D15](../README.md#decisions)): it requires the fork release with the fixes, and has one code path per behaviour.
- Docker in the alpha; the same behaviour on gVisor (beta) and Firecracker (later) when they come.

## Design

### The request path

```go
// In internal/httpapi.
in, ok := h.runningSandbox(w, r)          // steps 1–5 of the overview
if !ok { return }
err := h.withToolbox(ctx, func() error {  // step 6
    result, err = in.Toolbox.Execute(ctx, req)
    return err
})
if err != nil { h.writeToolboxError(w, r, in, err, "…not found message…"); return }
```

- **`runningSandbox`** reads the sandbox in one query scoped to the key's project, profile and clusters (one sandbox for a sandbox key), and the cluster connection from the [cluster cache](../clusters.md). It returns an `inSandbox`: the sandbox row, AerolVM's id for it (it changes on every resume), the node's client, the cluster's architecture and runtime.
- **No HTTP to an app.** Legacy's `GET sandboxes/{sbx}?key=` and its 60 s key cache go; the key is checked against Postgres on every request, with a short in-process cache only if profiling says so ([api/authentication](../api/authentication.md)).
- **`withToolbox`** waits for the toolbox to be ready, then calls it. Readiness comes from the fork (F4): the sandbox's `toolbox_ready`, set by the `started` event on the [event stream](../sandboxes/reconciler.md#events), so a request right after a start waits for that signal, up to `toolbox_wait` (30 s), rather than retrying on `502`/`503` to guess. Network errors on calls that are safe to repeat are retried as any client would, 250 ms doubling to 2 s; a streamed upload's body is never sent twice.
- **A node that doesn't answer at all** is forgotten in the cluster cache, so the next request finds the cluster's nodes again, and answers `503 cluster_unavailable`.
- **A `410` from AerolVM** means the sandbox's node died and was evicted: answer `409 sandbox_not_running` ("lost"), and tell the [reconciler](../sandboxes/reconciler.md) to look at the sandbox now rather than within a minute.

### Login shells and directories

- **Every command runs in a login shell, as root.** Execute already uses `bash -lc`. The exec stream and sessions run `/bin/sh -c` (T10), so Lewbox wraps their commands:

  ```sh
  if command -v bash >/dev/null 2>&1; then exec bash -lc '<command>'; fi; exec sh -lc '<command>'
  ```

  So every path loads `/etc/profile.d`, where [profile setup](../sandboxes/profile-setup.md) writes the agents' keys and Git's settings. One shell quoter (`setup.Quote` today) is used for everything; it's tested with every byte value.
- **Directories.** `~` and `~/…` resolve to `/root/…`; anything else must be absolute (`422 validation_failed` on `directory`). A missing directory is `validation_failed` ("There's no directory at …"): the fork's toolbox answers it with the code `directory_not_found` (F6), so Lewbox doesn't stat first.
- **The default directory** is the sandbox's own: `/root/workspace` when it was created with repositories, else `/root`. (Legacy leaves it to the toolbox, which uses the image's `WORKDIR`; we make it explicit and documented.)
- **Terminals** get `TERM=xterm-256color` and `COLORTERM=truecolor` unless `env` sets them.

### Exit codes

On Docker the toolbox reports exit codes correctly, so Lewbox takes them as they come: no traps, no status files, no markers in stderr. Legacy's traps were for gVisor (T1), where toolboxd, as init, lost statuses to `ESRCH`; the fork fixes that in toolboxd (F1) before `gvisor` is offered in the beta ([clusters/runtimes](../clusters/runtimes.md)).

### Activity

- **The fork counts it (F5, in the minimum release):** the node's proxy counts activity while bytes move on any toolbox stream (exec stream, attach, file transfer), at most once a minute, the way it counts a request's start; and requests marked as not activity (status reads, Lewbox's own maintenance) don't count. Lewbox never touches `toolbox/health` to keep a sandbox awake.
- **What doesn't count:** reading the sandbox's own record, listing ports or SSH keys, links. These are reads of Lewbox's database, not the sandbox.
- **Watched sessions** keep their sandbox awake while they run ([sessions](sessions.md#watch)), on purpose: Lewbox learns of their end from the fork's session events (F36), not by polling.

### Streams

- **Upstream WebSockets** are dialled with the PAT in `Authorization`, and a read limit of 4 MiB, since attaching replays a session's 1 MiB buffer as one message.
- **Kill on drop.** When a command's client goes, Lewbox ends the command: it sends `{"type":"signal","signal":"KILL"}`, then closes. That's Lewbox's documented behaviour for a streamed command whose caller left, not a workaround (T5 is AerolVM leaving it running, which a session is for).
- **Pings.** Lewbox pings its own clients every 30 s. Pings on the toolbox's side (F3, P3) would let a node notice a stream whose Lewbox replica died.
- **Replica shutdown.** On `SIGTERM`, every relayed stream ends: commands are killed and their clients get `1001`; attached sessions carry on and their clients get `1001` to reattach ([api/streaming](../api/streaming.md)).

### Errors

The fork answers every toolbox, envd and proxy error in one shape with a stable code (F6, in the minimum release), and Lewbox maps codes, never wording. Callers pass the `404` message, since a 404 means a file, a session or the sandbox depending on the call.

| From the fork | Lewbox answers |
|---|---|
| `toolbox_unavailable` (still not ready after `toolbox_wait`) | `503 toolbox_unavailable` |
| `410`, `sandbox_gone` | `409 sandbox_not_running`, the sandbox lost with its node |
| `not_found` | `404 not_found`, with the caller's message |
| `path_forbidden` | `403 path_forbidden` |
| `already_exists` | `409 already_exists` |
| `directory_not_found` | `422 validation_failed` on `directory` |
| `not_a_directory`, `is_a_directory`, other client errors | `422 sandbox_refused`, with the fork's message |
| `precondition_failed` | `412 precondition_failed` ([files](files.md#uploads)) |
| a server error with a code | `502 sandbox_error`, with the fork's message |
| No answer, `401`, anything unexpected | `503 cluster_unavailable`; forget the cluster's cached nodes; log at `error`, as an unexpected answer is a fork bug to fix, not to match |

### The minimum fork release

Every toolbox problem legacy worked around is a fix in the fork, and Lewbox requires the release that has it ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox)). There's no capability table and no per-node fallback:

| Problem | Fork fix | When |
|---|---|---|
| T1 exit statuses (gVisor) | F1 | With gVisor (beta); Docker doesn't have the bug |
| T2 session names | F2 `if_absent` | Alpha (P0) |
| T3 activity on streams | F5 | Alpha (P0) |
| T4 downloads in memory | F7 streamed, with `Range` | Alpha (P1); until it lands, downloads have a documented size limit |
| T5 dropped streams | None needed: Lewbox kills on drop ([Streams](#streams)) | |
| T6 pings | F3 | P3 |
| T7 readiness | F4 `toolbox_ready` | Alpha (P0) |
| T8 errors | F6 coded errors | Alpha (P0) |
| T9 session ends | F36 events | Alpha (P0) |
| T10 login shell | None needed: commands run in a login shell by Lewbox's own design ([above](#login-shells-and-directories)) | |
| T11 conditional writes, watching | F51 conditional writes, F9 watching | P1 and P2: the [files](files.md) features that need them wait for them |

### Lewbox's helpers in the sandbox

Lewbox runs two of its own programs inside sandboxes: `lewbox portd` ([private ports](private-ports.md)) and `lewbox agentd` ([agents/agentd](../agents/agentd.md)).

- **Today**, the gateway uploads each build into the sandbox (`/usr/local/lib/lewbox/portd-<sha12>`, `agentd-<version>`), writing a `.part-…` file, `chmod`, then a move, so two requests can't run a half-written file. Every sandbox, every start, through the files API, with the gVisor `chmod` problem.
- **The alpha uploads them,** as legacy does, through the files API, with the part-file-then-move so two requests can't run a half-written file. On Docker `chmod` behaves, so there's no mode check after it (that was gVisor's).
- **Later, a mount (F52):** the fork mounts a node directory read-only into every sandbox, as it mounts `toolboxd`: `/opt/lewbox` → `/usr/local/lib/lewbox`. `lewbox node` keeps the right helper build there. When it's in the minimum release, uploads go entirely; there's no period with both.
- **Which binary.** The helpers are subcommands of `lewbox`, but the full binary carries the dashboard and server. Build a slim helper binary from the same module with only `agentd` and `portd` (a build tag), a few MB, embedded in the main binary for upload and shipped with `lewbox node` for the mount ([foundation/build-and-release](../foundation/build-and-release.md)).

### Which fixes go to the fork

All of them, with the priorities in [clusters/aerolvm-fork](../clusters/aerolvm-fork.md#fix-backlog). For the alpha's toolbox: F2 unique sessions, F4 readiness, F5 stream activity, F6 coded errors and F36 session events (P0), then F7 streamed downloads and F51 conditional writes (P1). Each is a branch in the fork, a pull request upstream, and an entry in [lessons](../lessons.md) with the commit that found it.

## Security

- **The PAT never leaves the control plane.** Clients reach the toolbox only through Lewbox. WebSockets to nodes carry the PAT in a header, never in a URL.
- **Plain-HTTP nodes are refused** outside development, as the PAT would cross the network unencrypted ([clusters/connectivity](../clusters/connectivity.md)).
- **Nothing in the sandbox can call Lewbox with power.** agentd gets a token that only uploads its own conversation; portd gets only public keys.
- **Helpers mounted read-only**, so a sandbox can't replace the binary another start runs.
- **Quoting.** Every value put into a shell command (commands for sessions and helpers, paths, names) goes through one quoter, fuzz-tested.

## Observability

- **Metrics:** `lewbox_toolbox_requests_total{route, outcome}`, `lewbox_toolbox_request_seconds{route}`, `lewbox_toolbox_retries_total`, `lewbox_toolbox_unavailable_total`, `lewbox_streams_open{kind}` (command, interactive, attach, sse), `lewbox_stream_bytes_total{kind, direction}`, `lewbox_toolbox_unexpected_total{route}` (answers the fork's contract doesn't allow: each one is a fork bug to fix).
- **Logs:** each toolbox call at `debug` with sandbox, node, route and duration; failures to reach a node at `warn`.
- **Traces:** a span per toolbox call, child of the request's.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_TOOLBOX_WAIT` | `30s` | How long a request waits for the toolbox after a start |
| `LEWBOX_WEBSOCKET_PING_INTERVAL` | `30s` | Pings to clients |
| `LEWBOX_SSE_KEEPALIVE` | `15s` | Keepalive comments on quiet SSE streams |
| `LEWBOX_AEROLVM_TIMEOUT` | `30s` | One ordinary call to a node |

Names follow [foundation/configuration](../foundation/configuration.md).

## Testing

- **The fake toolbox** (`internal/testing/aerolvmfake`), ported from legacy's `aerolvmtest/toolbox.go`:
  - **Exec:** `SetExec` scripts Execute results. The exec stream runs `cat` (echo), `sleep` (until killed), or scripted output.
  - **Sessions** echo their input, exit on `KILL`, and replay then follow on attach. Every create makes a new session, as toolboxd does.
  - **Files** live in memory, with modes.
  - It plays the **minimum fork release**: coded errors, `toolbox_ready`, `if_absent` sessions, session-end events, stream activity. It doesn't model legacy's bugs, as Lewbox has no code for them; a contract test runs the same cases against a real node so the fake can't drift.
  - **Faults that are real:** a node that doesn't answer, a toolbox not ready for a while, a sandbox lost with its node.
  - **Recording:** `Killed` and `StreamStarts` record what happened.
- **Live checks** before each release, on a Docker cluster (gVisor and Firecracker when they're supported): run a command, stream one, kill one, a session's exit code, a 2 GB download, a private port in an iframe ([foundation/testing](../foundation/testing.md)).

## Build plan

1. `internal/aerolvm`: port legacy's client and types, without its workarounds; the minimum-release check.
2. `internal/httpapi`: `runningSandbox`, `withToolbox`, `writeToolboxError`, `loginShell`, `workingDirectory`, `missingDirectory`, `activity`, `terminalEnv`; the SSE writer with its UTF-8 decoder, and the WebSocket relay (port `sse.go`, `websocket.go`).
3. The fake toolbox, playing the minimum release.
4. Fork work, by the backlog's priorities, tracked in [clusters/aerolvm-fork](../clusters/aerolvm-fork.md).

## Open questions

1. ~~**Capabilities endpoint.**~~ Decided (2026-10-29): none. Lewbox requires a minimum fork release instead of detecting fixes per node ([D15](../README.md#decisions)).
2. **Helper mount and Firecracker (later).** A bind mount (F52) works for containers; for Firecracker microVMs it needs a second block device or virtiofs.
3. **Default directory.** `/root/workspace` with repositories, `/root` without? Or always `/root/workspace`, made at create?
4. **Key checks per request.** Is a Postgres read per request inside a sandbox fine at our scale (interactive terminals don't re-check after connecting), or do we want a short in-process cache with invalidation over `LISTEN/NOTIFY` when a key is revoked?
