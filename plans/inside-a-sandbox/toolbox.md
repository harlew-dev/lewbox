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
- Each toolbox problem fixed in the fork, with a pull request upstream, and Lewbox's workaround kept only for nodes that don't have the fix yet.
- The same behaviour on Docker, gVisor and Firecracker.

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

- **`runningSandbox`** reads the sandbox in one query scoped to the key's project, profile and clusters (one sandbox for a sandbox key), and the cluster connection from the [cluster cache](../clusters.md). It returns an `inSandbox`: the sandbox row, AerolVM's id for it (it changes on every resume), the node's client, the cluster's architecture and runtime, and the node's [capabilities](#capabilities).
- **No HTTP to an app.** Legacy's `GET sandboxes/{sbx}?key=` and its 60 s key cache go; the key is checked against Postgres on every request, with a short in-process cache only if profiling says so ([api/authentication](../api/authentication.md)).
- **`withToolbox`** retries only calls that are safe to repeat and whose bodies can be sent again: everything except a streamed upload's body, which waits for the toolbox with a `health` call first (as legacy's `writeFile` does). 250 ms doubling to 2 s, up to `toolbox_wait` (30 s). Only `ToolboxUnavailable` errors are retried (T7); with the fork's [error codes](#errors) it matches a code, not words.
- **A node that doesn't answer at all** is forgotten in the cluster cache, so the next request finds the cluster's nodes again, and answers `503 cluster_unavailable`.
- **A `410` from AerolVM** means the sandbox's node died and was evicted: answer `409 sandbox_not_running` ("lost"), and tell the [reconciler](../sandboxes/reconciler.md) to look at the sandbox now rather than within a minute.

### Login shells and directories

- **Every command runs in a login shell, as root.** Execute already uses `bash -lc`. The exec stream and sessions run `/bin/sh -c` (T10), so Lewbox wraps their commands:

  ```sh
  if command -v bash >/dev/null 2>&1; then exec bash -lc '<command>'; fi; exec sh -lc '<command>'
  ```

  So every path loads `/etc/profile.d`, where [profile setup](../sandboxes/profile-setup.md) writes the agents' keys and Git's settings. One shell quoter (`setup.Quote` today) is used for everything; it's tested with every byte value.
- **Directories.** `~` and `~/…` resolve to `/root/…`; anything else must be absolute (`422 validation_failed` on `directory`). Before running in a directory, Lewbox stats it, so a missing one is `validation_failed` ("There's no directory at …") rather than the toolbox's `fork/exec /usr/bin/bash: no such file or directory`. With the fork, the toolbox answers this itself with a code, and the extra stat goes.
- **The default directory** is the sandbox's own: `/root/workspace` when it was created with repositories, else `/root`. (Legacy leaves it to the toolbox, which uses the image's `WORKDIR`; we make it explicit and documented.)
- **Terminals** get `TERM=xterm-256color` and `COLORTERM=truecolor` unless `env` sets them.

### Exit codes

Fixes T1.

The fix belongs in `toolboxd`, and goes to the fork first:

- **Fork fix:** as init, `toolboxd` reaps every child, so it must remember the statuses it reaps. A reaper loop records `pid → status` in a map; a waiter that gets `ECHILD` *or* `ESRCH` takes the status from the map. This covers Execute, the exec stream, sessions and login shells alike, on every runtime. Upstream issue and pull request: [clusters/aerolvm-fork](../clusters/aerolvm-fork.md).
- **Until a node has it** (capability `exit_status`):
  - **Execute:** prefix `trap 'printf "\n__lewbox_exit_status=%s\n" "$?" >&2' EXIT; ` on the command's first line, so its line numbers stay; take the last marker line from stderr and remove it, and anything after it. No marker, as for a command its timeout killed or one that sets its own EXIT trap, leaves the toolbox's answer.
  - **Sessions:** a trap that writes the status to `/root/.lewbox/sessions/<ses_id>`, read when the toolbox says `-1` or `killed` with signal `waitid: no such process`, and removed once the end is recorded ([sessions](sessions.md#exit-codes)).
  - **Exec streams** (commands streamed or interactive): a trap that writes the marker to a file, as for sessions, read once the stream ends with `-1`. Legacy doesn't fix these at all. With `pty`, stderr is the terminal, so writing to it would show the marker.
  - **Installing helpers:** check a file's mode with `Stat` after `chmod` rather than trusting its exit (`4f0da62`).

### Activity

Fixes T3.

- **Fork fix:** the node's proxy counts activity while bytes move on any toolbox stream (exec stream, attach, file transfer), at most once a minute, the way it already counts a request's start. Capability `stream_activity`.
- **Until then:** Lewbox counts bytes on each relayed stream, and at most every `activity_interval` (60 s) calls `toolbox/health` in the background, never holding up the stream. Pings and keepalives don't count.
- **What doesn't count:** reading the sandbox's own record, listing ports or SSH keys, links. These are reads of Lewbox's database, not the sandbox.
- **Watched sessions** keep their sandbox awake ([sessions](sessions.md#watch)). Today that's a side effect of polling them. With the fork's session events, Lewbox touches the sandbox on purpose while a watched session runs.

### Streams

- **Upstream WebSockets** are dialled with the PAT in `Authorization`, and a read limit of 4 MiB, since attaching replays a session's 1 MiB buffer as one message.
- **Kill on drop (T5).** A command's stream sends `{"type":"signal","signal":"KILL"}` before closing when its client goes. Fork fix: an `on_disconnect: kill` option on the exec stream, default for Lewbox; capability `exec_kill_on_close`.
- **Pings (T6).** Lewbox pings its own clients every 30 s. The fork adds pings on the toolbox's side, so a node-side stream whose Lewbox replica died is noticed and its command killed.
- **Replica shutdown.** On `SIGTERM`, every relayed stream ends: commands are killed and their clients get `1001`; attached sessions carry on and their clients get `1001` to reattach ([api/streaming](../api/streaming.md)).

### Errors

How toolbox and node errors map to Lewbox's. Callers pass the `404` message, since a 404 means a file, a session or the sandbox depending on the call.

| From AerolVM | Lewbox answers |
|---|---|
| `502`/`503` with `toolbox unavailable` or `sandbox not adopted` (after retries) | `503 toolbox_unavailable` |
| `410` | `409 sandbox_not_running`, the sandbox lost with its node |
| `404` | `404 not_found`, with the caller's message |
| `403` | `403 path_forbidden` |
| `409` | `409 already_exists` |
| Other `4xx` (not `401`) | `422 sandbox_refused`, with the toolbox's message |
| `500` with a message | `502 sandbox_error`, with the toolbox's message |
| No answer, `401`, anything else | `503 cluster_unavailable`; forget the cluster's cached nodes; log at `warn` |

**Fork fix (T8):** one error shape across the toolbox, envd and the proxy, `{"error": "…", "code": "…"}`, with codes Lewbox can match: `toolbox_unavailable`, `not_found`, `path_forbidden`, `already_exists`, `not_a_directory`, `is_a_directory`, `directory_not_found`, `precondition_failed`. Matching words stays as a fallback for older nodes.

### Capabilities

Each fix in the fork is a **capability** the node reports, so Lewbox applies a workaround only where it's needed and drops it once every node has moved on.

- The fork adds `GET /v0/capabilities` (or capabilities on `GET /v0/version`): the fork's version and a list of names.
- `lewbox node` reports the AerolVM version with its heartbeat ([clusters/health-and-metrics](../clusters/health-and-metrics.md)); the control plane fetches capabilities when a node's version changes, and caches them on the node's row.
- `inSandbox.Capabilities` carries them into every handler.

| Capability | Fixes | Removes from Lewbox |
|---|---|---|
| `exit_status` | T1 | Execute's stderr trap, sessions' status files, stream traps, the `chmod` stat |
| `unique_session_names` | T2 | Listing sessions before starting a proxy or agentd. `POST …/sessions` with `if_absent: true` answers the running session of that name |
| `stream_activity` | T3 | Touches while streams move |
| `streamed_downloads` | T4 | The memory limit on downloads; adds `Range` |
| `exec_kill_on_close` | T5 | Sending `KILL` before closing |
| `stream_pings` | T6 | |
| `error_codes` | T8 | Matching error words |
| `session_events` | T9 | Polling watched sessions |
| `login_shell` | T10 | Wrapping commands in `bash -lc` |
| `conditional_writes` | T11 | Lewbox's own compare-then-write ([files](files.md#uploads)) |
| `watch` | T11 | Enables file watching ([files](files.md#watching)) |
| `helpers_mount` | | Uploading `portd` and `agentd` ([below](#lewboxs-helpers-in-the-sandbox)) |

The alpha supports nodes with none of these, and every workaround is tested against a fake toolbox that has the bug ([testing](#testing)).

### Lewbox's helpers in the sandbox

Lewbox runs two of its own programs inside sandboxes: `lewbox portd` ([private ports](private-ports.md)) and `lewbox agentd` ([agents/agentd](../agents/agentd.md)).

- **Today**, the gateway uploads each build into the sandbox (`/usr/local/lib/lewbox/portd-<sha12>`, `agentd-<version>`), writing a `.part-…` file, `chmod`, then a move, so two requests can't run a half-written file. Every sandbox, every start, through the files API, with the gVisor `chmod` problem.
- **Proposed:** the fork mounts a node directory read-only into every sandbox, as it mounts `toolboxd`: `/opt/lewbox` → `/usr/local/lib/lewbox`. `lewbox node` keeps the right helper build there for the node's architecture, and updates it with the control plane ([clusters/node-agent](../clusters/node-agent.md)). No uploads, no `chmod`, one copy per node, and the version is the node's. Capability `helpers_mount`.
- **Which binary.** The helpers are subcommands of `lewbox`, but the full binary carries the dashboard and server. Build a slim helper binary from the same module with only `agentd` and `portd` (a build tag), a few MB, embedded in the main binary for upload and shipped with `lewbox node` for the mount ([foundation/build-and-release](../foundation/build-and-release.md)).
- **Upload stays as the fallback** for nodes without `helpers_mount`, with legacy's part-file-then-move and the mode check.

### Which fixes go to the fork

All of T1–T11, in this order, as each blocks something in the alpha:

1. **T1 exit statuses**: wrong exit codes break setup, `git` in Lewagent's Git panel, and port proxies.
2. **T2 unique session names**: double proxies and double agentd daemons.
3. **T9 session events**: removes polling, and makes watched setup steps finish within a second.
4. **T3 stream activity**, **T5 kill on close**, **T6 pings**.
5. **T4 streamed downloads**, **T11 conditional writes and watching**: Lewagent's editor and artifacts.
6. **T8 error codes**, **T10 login shell**, **`helpers_mount`**.

Each is a branch in the fork, a pull request upstream, and an entry in [lessons](../lessons.md) with the commit that found it.

## Security

- **The PAT never leaves the control plane.** Clients reach the toolbox only through Lewbox. WebSockets to nodes carry the PAT in a header, never in a URL.
- **Plain-HTTP nodes are refused** outside development, as the PAT would cross the network unencrypted ([clusters/connectivity](../clusters/connectivity.md)).
- **Nothing in the sandbox can call Lewbox with power.** agentd gets a token that only uploads its own conversation; portd gets only public keys.
- **Helpers mounted read-only**, so a sandbox can't replace the binary another start runs.
- **Quoting.** Every value put into a shell command (commands for sessions and helpers, paths, names) goes through one quoter, fuzz-tested.

## Observability

- **Metrics:** `lewbox_toolbox_requests_total{route, outcome}`, `lewbox_toolbox_request_seconds{route}`, `lewbox_toolbox_retries_total`, `lewbox_toolbox_unavailable_total`, `lewbox_streams_open{kind}` (command, interactive, attach, sse), `lewbox_stream_bytes_total{kind, direction}`, `lewbox_workarounds_applied_total{capability}` (so we know when one can be deleted).
- **Logs:** each toolbox call at `debug` with sandbox, node, route and duration; failures to reach a node at `warn`.
- **Traces:** a span per toolbox call, child of the request's.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_TOOLBOX_WAIT` | `30s` | How long a request waits for the toolbox after a start |
| `LEWBOX_ACTIVITY_INTERVAL` | `60s` | Most often a moving stream counts as activity |
| `LEWBOX_WEBSOCKET_PING_INTERVAL` | `30s` | Pings to clients |
| `LEWBOX_SSE_KEEPALIVE` | `15s` | Keepalive comments on quiet SSE streams |
| `LEWBOX_AEROLVM_TIMEOUT` | `30s` | One ordinary call to a node |

Names follow [foundation/configuration](../foundation/configuration.md).

## Testing

- **The fake toolbox** (`internal/testing/aerolvmfake`), ported from legacy's `aerolvmtest/toolbox.go`:
  - **Exec:** `SetExec` scripts Execute results. The exec stream runs `cat` (echo), `sleep` (until killed), or scripted output.
  - **Sessions** echo their input, exit on `KILL`, and replay then follow on attach. Every create makes a new session, as toolboxd does.
  - **Files** live in memory, with modes.
  - **Fault injection** for every problem above, each switchable by capability: `SetExecLosesStatus`, `SetSessionsLoseStatus` (gVisor), `ToolboxWarmUp` (502/503 at start), duplicate session names, whole-file downloads, streams that don't kill on close.
  - **Recording:** `Touches`, `Killed` and `StreamStarts` record what happened.
- **Every workaround** has a test with the bug on and with the capability on, so deleting a workaround is a test change, not a guess.
- **Live checks** before each release, on a Docker, a gVisor and (once supported) a Firecracker cluster: run a command, stream one, kill one, a session's exit code, a 2 GB download, a private port in an iframe ([foundation/testing](../foundation/testing.md)).

## Build plan

1. `internal/aerolvm`: port legacy's client and types; add capability fetching.
2. `internal/httpapi`: `runningSandbox`, `withToolbox`, `writeToolboxError`, `loginShell`, `workingDirectory`, `missingDirectory`, `activity`, `terminalEnv`; the SSE writer with its UTF-8 decoder, and the WebSocket relay (port `sse.go`, `websocket.go`).
3. The fake toolbox, with the faults.
4. The capability table, and each workaround behind it.
5. Fork work, in the order above, tracked in [clusters/aerolvm-fork](../clusters/aerolvm-fork.md).

## Open questions

1. **Capabilities endpoint.** Does upstream want a capabilities endpoint, or do we keep it to the fork and key off the fork's version string?
2. **Helper mount and Firecracker.** A bind mount works for containers; for Firecracker microVMs it needs a second block device or virtiofs. Does the fork's Firecracker support give us a way to share a read-only directory?
3. **Default directory.** `/root/workspace` with repositories, `/root` without? Or always `/root/workspace`, made at create?
4. **Key checks per request.** Is a Postgres read per request inside a sandbox fine at our scale (interactive terminals don't re-check after connecting), or do we want a short in-process cache with invalidation over `LISTEN/NOTIFY` when a key is revoked?
