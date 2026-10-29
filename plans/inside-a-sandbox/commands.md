# Commands

> Status: Draft · Milestone: Alpha (M2) · Updated: 2026-10-29

A command runs in a running sandbox and ends: a build, a test, `git status`. Lewbox runs it three ways. **Whole**: the request answers once it ends, with its exit code and output. **Streamed**: the same request with `Accept: text/event-stream` sends output as server-sent events while it runs. **Interactive**: a WebSocket carries stdin, output as bytes, resizes and signals, with or without a terminal. A command lives only as long as its request; anything that should outlive it is a [session](sessions.md).

## Today

- **Handlers:** `lewbox-gateway-legacy/internal/server/commands.go` (477 lines).
  - `runCommand` runs a command whole, or streams it when asked.
  - `streamCommandOutput` does SSE over the toolbox's exec stream.
  - `streamCommand` is the interactive WebSocket.
  - `validateCommand` checks fields; `ranToCompletion` and `exited` work out how a command ended for the activity log.
- **Transport:** `sse.go` (event writer, keepalives, the UTF-8 `textDecoder`), `websocket.go` (relay, pings, kill on drop, `closeWithError`).
- **Toolbox:** Execute, with the gVisor exit trap (`febb176`), and the exec stream ([toolbox](toolbox.md)).
- **Docs:** [commands](https://lewbox.com/docs/inside-a-sandbox/commands), [streaming output](https://lewbox.com/docs/inside-a-sandbox/streaming-output), [interactive commands](https://lewbox.com/docs/inside-a-sandbox/interactive-commands).
- **Activity:** `command.ran` with outcome, exit code, duration, and the redacted end of stderr (`6027533`); `terminal.opened` and `terminal.closed` for terminals.

**Who uses it:**
- Lewagent's whole Git panel (`lewagent-legacy/resources/js/lib/git.ts`, ~2300 lines) runs `git` and `gh` as commands from the browser with a sandbox key, labelled for the activity log.
- Its services panel drives `lewagent-services`.
- Its setup writes and runs scripts.
- Its terminal panel uses [sessions](sessions.md), not interactive commands.

**What's wrong:**
1. **Exit codes on streams are wrong under gVisor.** The trap only covers Execute, so a streamed or interactive command can end with `-1` (T1 in [toolbox](toolbox.md#problems-and-where-each-was-seen)). Lewagent reads printed sentinels instead of exit codes because of it (`RunSetupStep`, `CheckPort`).
2. **Timeouts can't be told apart from failures.** Execute doesn't say a command was killed for its timeout; the gateway guesses from "failed and ran for the whole timeout".
3. **Long whole commands hit edge timeouts.** Laravel Cloud's edge cut requests at about 20 s; that platform goes, but any proxy in front of `lewbox serve` can do the same (`lewagent-legacy/docs/todo.md`).

## Goals

- Keep the three ways, their fields, events, messages and close codes, as documented.
- Exit codes are right on every path and runtime.
- A command killed for its timeout says so, every way it's run.
- A command never outlives its request: a client that goes has it killed.

### Non-goals

- Keeping a command's text, output or env anywhere. Only how it ended is logged.
- Detached commands. Start a [session](sessions.md) with `watch: true` and wait for its end.

## Design

### Whole

`POST /v0/sandboxes/{sbx}/commands`

| Field | Type | |
|---|---|---|
| `command` | string | Required, not blank. Run with `bash -lc`, as root |
| `directory` | string | Absolute, or `~`/`~/…`. Must exist and be a directory. Default the sandbox's ([toolbox](toolbox.md#login-shells-and-directories)) |
| `env` | object | String values, at most 100. For this command only. Never logged |
| `timeout` | integer | Seconds, 1 to 3600. Default 60 |
| `label` | string | For the activity log. 1–64 of `[a-z0-9._-]`, starting with a letter or digit. Wins over `X-Lewbox-Label` |

Answers `200` with:

```json
{ "data": { "exit_code": 0, "stdout": "…", "stderr": "", "duration_ms": 412, "timed_out": false } }
```

- **`timed_out`** is new: `true` when the command was killed for its timeout. Then `exit_code` is the toolbox's (usually `137` or `-1`), and the activity outcome is `timed_out`. The fork's Execute reports this itself; until it does, Lewbox infers it, as legacy does, from a failure that ran for the whole timeout.
- **A failing command still answers `200`.** The request worked; the command didn't.
- **Output size.** The toolbox holds output in memory until the command ends. Lewbox caps each of `stdout` and `stderr` at 16 MiB (configurable). Past that it keeps the start and end and marks the result `"truncated": true`, rather than failing or running out of memory. To read more, stream it or write it to a file.
- **The request stays open** for as long as the command runs. SDKs set their HTTP timeout to the command's plus a margin. Deployments behind a proxy must allow long requests ([foundation/binary-and-commands](../foundation/binary-and-commands.md)); the docs say to stream anything longer than a minute.

### Streamed

The same request with `Accept: text/event-stream`. It answers `200` at once, then events, each with JSON data:

| Event | Data | |
|---|---|---|
| `stdout` | `{"data"}` | UTF-8 text |
| `stderr` | `{"data"}` | UTF-8 text |
| `exit` | `{"exit_code", "signal"}` | It ended. `signal` names the signal that ended it, e.g. `KILL`, or `null` |
| `error` | `{"code", "message"}` | The stream ended without an exit |

Error codes: `command_timeout`, `command_failed` (couldn't start), `stream_interrupted` (the sandbox's stream ended first), `server_restarting` (was `gateway_restarting`; the replica carrying the stream is shutting down, and the command was killed).

- **Exactly one** `exit` or `error` ends every stream.
- **Text, not bytes.** A character split across output chunks is held until it's whole; invalid bytes become U+FFFD. For bytes, use interactive.
- **Keepalives.** A `: keepalive` comment every 15 s while quiet. Keepalives don't count as activity; output does, at most once a minute.
- **Stdin is closed** before the command starts, so a command reading it sees end of input.
- **Errors before the stream** are ordinary JSON errors with their status.
- **How it runs.** Over the toolbox's exec stream, not Execute: the command, wrapped in a login shell, is sent as the first message, then `{"type":"close"}` to close stdin. Output relays as it comes. On the timeout, the client going, or shutdown, Lewbox sends `KILL` first.

### Interactive

`GET /v0/sandboxes/{sbx}/commands/stream`, a WebSocket.

- **Auth:** the `Authorization` header, or the subprotocols `lewbox.bearer` and the key; the response chooses only `lewbox.bearer` ([api/authentication](../api/authentication.md)). Refusals before the upgrade are JSON with their status; a plain HTTP request is `426 upgrade_required`.
- **First message**, text, within 10 s:

  | Field | Type | |
  |---|---|---|
  | `command` | string | Required |
  | `directory` | string | As above |
  | `env` | object | At most 100 |
  | `pty` | boolean | Run in a terminal. Default `false` |
  | `cols`, `rows` | integer | 1–1000. Default 80 × 24 |
  | `label` | string | As above |

  Late, not JSON, or invalid: a text message `{"type":"error","code":"invalid_start_message","message":"…"}`, then close `1008`.

- **Output:** binary messages, first byte `0x01` stdout or `0x02` stderr, then the bytes as written. With `pty`, all output is `0x01`.
- **Input:** binary messages are stdin, unprefixed. Text messages are control:
  - `{"type":"resize","cols":160,"rows":50}` (pty only)
  - `{"type":"signal","signal":"INT"}`: `INT`, `TERM`, `KILL`, `HUP`, `QUIT`
  - `{"type":"close"}` closes stdin
- **The end:** `{"type":"exit","code":N,"signal":"…"}` then `1000`; or `{"type":"error","code":"…","message":"…"}`.
- **Close codes:** `1000` ended; `1001` the replica is shutting down, the command was killed; `1008` bad first message; `1011` the sandbox's stream ended before the command did.
- **Pings** every 30 s; a client that stops answering is gone, and its command is killed.
- **Terminals:** `TERM=xterm-256color`, `COLORTERM=truecolor` unless `env` sets them. Input is keystrokes (`\r`, `\x03`, escape sequences).
- **Activity log:** a terminal writes `terminal.opened` and `terminal.closed` (with exit and duration); a command without `pty` writes `command.ran` with `interactive: true` and its stderr tail.

### Exit codes on every path

Whole, streamed and interactive commands all take the exit code the toolbox reports. On Docker, the alpha's only runtime, it's right on every path, so there are no traps, markers or status files. Legacy's traps were for gVisor's lost statuses (T1); the fork fixes those (F1) before `gvisor` is offered in the beta, so Lewbox never needs them ([toolbox](toolbox.md#exit-codes)).

### Timeouts

- **Whole:** the toolbox's own timeout kills the command; Lewbox's HTTP call allows the timeout plus `aerolvm_timeout`.
- **Streamed:** Lewbox's timer kills it with `KILL`, then sends `error` `command_timeout`.
- **Interactive:** no timeout. The command runs as long as the client stays.
- **Maximum** is `LEWBOX_COMMAND_MAX_TIMEOUT` (1 hour); beyond that, a session.

### The activity log

`command.ran`, written when a command ends, never with its text, output or env:

| Field | |
|---|---|
| `outcome` | `succeeded` (exit 0), `failed` (non-zero, or couldn't start), `timed_out`, `killed` (client went, or shutdown) |
| `details` | `exit_code`, `signal`, `duration_ms`, `interactive`, `streamed` |
| `label` | From the field or `X-Lewbox-Label` |
| `stderr` | When it didn't succeed: the last 2 KB of stderr, redacted |
| `request_id`, `actor` | As every event |

**Redaction** (`internal/activity/redact.go`), applied before anything is stored:
- `Authorization:` and `Bearer …` values;
- passwords in URLs (`https://user:pass@`);
- Lewbox tokens (`lb[apsnil]_[0-9a-f]{64}`) and GitHub tokens (`gh[pousr]_…`, `github_pat_…`);
- Anthropic and OpenAI keys (`sk-ant-…`, `sk-…`);
- AWS keys, JWTs;
- values of variables whose names contain `TOKEN`, `SECRET`, `PASSWORD` or `KEY`.

A project may turn stderr recording off (legacy's `record_command_stderr`, [projects](../projects.md)). Successful commands are kept for less time than other events, as polling commands such as `git status` make most of a log ([activity log](../activity-and-webhooks/activity-log.md)).

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.exec` |
| `404` | `not_found` | No such sandbox the key sees |
| `409` | `sandbox_not_running` | With its `status` |
| `422` | `validation_failed` | Blank `command`, `timeout` out of range, more than 100 `env`, bad `label`, a `directory` that isn't absolute, doesn't exist or is a file |
| `422` | `sandbox_refused` | The toolbox refused it |
| `426` | `upgrade_required` | `commands/stream` without a WebSocket upgrade |
| `502` | `sandbox_error` | The toolbox failed to run it |
| `503` | `toolbox_unavailable`, `cluster_unavailable` | |

## Security

- **Commands run as root** in an untrusted sandbox; they can read anything in it, including the profile's agent credentials and Git token. That's by design, and documented ([sandboxes/profile-setup](../sandboxes/profile-setup.md)).
- **`env` is never stored or logged.** It goes straight to the toolbox.
- **Browsers** run commands with [sandbox keys](../sandboxes/sandbox-keys.md), never profile keys. CORS is open to any origin, since keys travel in a header, never a cookie.
- **Stderr in the activity log is redacted** before it's stored, and can be turned off per project.

## Observability

- **Metrics:** `lewbox_commands_total{mode, outcome}` (mode `whole`, `streamed`, `interactive`), `lewbox_command_seconds{mode}`, `lewbox_command_output_truncated_total`.
- **Access log** has the label, never the command.
- **Webhook:** `activity.command.ran` reaches projects that subscribe ([webhooks](../activity-and-webhooks/webhooks.md)). Most won't, given the volume.

## Configuration

| Setting | Default | |
|---|---|---|
| `LEWBOX_COMMAND_DEFAULT_TIMEOUT` | `60s` | |
| `LEWBOX_COMMAND_MAX_TIMEOUT` | `1h` | |
| `LEWBOX_COMMAND_MAX_OUTPUT` | `16MiB` | Per stream, whole commands |
| `LEWBOX_STREAM_START_TIMEOUT` | `10s` | First message on `commands/stream` |

## Testing

Against the [fake toolbox](toolbox.md#testing):

- Whole: exit codes 0 and non-zero; `timed_out`; truncation; a missing directory; `~`; env reaching the command; a label from the field and the header.
- Exit codes on every path as the toolbox reports them, including `-1` passed through unchanged (a fork bug to report, not to repair).
- Streamed: UTF-8 split across chunks; invalid bytes; keepalives on a quiet command; exactly one terminal event; client disconnect kills the command; timeout kills it; shutdown sends `server_restarting`.
- Interactive: start-message timeout and validation (`1008`); stdin round trip with `cat`; resize; each signal; `close`; pings; client drop kills; shutdown `1001`; `1011` when the toolbox's stream dies.
- Activity: one `command.ran` per command with the right outcome, and redaction on a stderr full of every kind of token.
- Auth on the WebSocket by header and by subprotocol, and the key never echoed.

## Build plan

1. Port `commands.go`, `sse.go`, `websocket.go` into `internal/httpapi`, renamed and using the shared request path.
2. Add `timed_out` and output caps.
3. Redaction in `internal/activity`, with a table of patterns and tests.
4. OpenAPI: the whole and streamed request on one path (two responses by `Accept`), and the WebSocket documented as a protocol ([api/openapi-and-codegen](../api/openapi-and-codegen.md)).

## Open questions

1. **`timed_out`.** Is a new field on the result right, or should a timed-out whole command answer an error (`408`-like) instead? A field keeps the output, which is useful for seeing why it hung.
2. **Output cap.** Is 16 MiB per stream right? Lewagent reads file diffs through `git` commands, which can be large.
3. **Should streamed commands carry event ids** so a client can resume after a dropped connection? That would need the command to outlive the connection, which is what sessions are for, so probably not.
4. **Rename `gateway_restarting`** to `server_restarting`, or keep the documented code? Lewagent already handles the old one.
