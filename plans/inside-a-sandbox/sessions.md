# Sessions

> Status: Draft · Milestone: Alpha (M2) · Updated: 2026-10-29

A session is a process that outlives the request that started it: a dev server, `sshd`, a setup step, a terminal you come back to, and Lewbox's own `agentd` and `portd`. It keeps running after its clients go, keeps its last 1 MiB of output, and lets any number of clients attach, follow it and type into it. Sessions are recorded in Lewbox (`ses_…`) and run in the toolbox; they don't survive a stop.

## Today

- **Handlers:** `lewbox-gateway-legacy/internal/server/sessions.go` (587 lines). Routes are create, list, get, update (resize or signal), delete, `log` and `attach`.
- **Records:** the app's `sandbox_sessions` table (`ses_`), through `/api/gateway/v1/sandboxes/{sbx}/sessions…`.
  - Columns: `aerolvm_id`, `command`, `directory`, `pty`, `watch`, `status` (`running`, `exited`, `killed`, `lost`), `exit_code`, `exit_signal`, `ended_at`.
- **The watcher:** `lewbox-gateway-legacy/internal/reconcile/sessions.go`.
  - Every `GATEWAY_SESSION_WATCH_INTERVAL` (5 s), on every replica, it asks the app for running watched sessions (`GET sessions?status=running&watch=1`).
  - It checks each with AerolVM, at most 8 sandboxes at once, and records the ended ones through `RecordEndedSession`.
  - The app writes `session.ended` to the activity log, and webhooks send `activity.session.ended` (`cf9813a`).
- **Exit codes under gVisor:**
  - A trap before the command writes its shell's status to `/root/.lewbox/sessions/<ses_id>`. The status is read when the toolbox answers `-1`, or `killed` with signal `waitid: no such process`, and the file is removed once the end is recorded (`cf9813a`, `590bbc2`, seen live on `ses_EkHSv8yXFQKkHn`).
  - A login shell has no command to trap, and keeps `-1`.
- **Docs:** [sessions](https://lewbox.com/docs/inside-a-sandbox/sessions).

**Who uses it:**
- **Lewagent's terminal panel:** PTY login shells in `/root/workspace`, attached again with output replayed.
- **Its setup and build steps:** watched sessions, carried on by `activity.session.ended` webhooks, with `log?tail=` for the Build panel (`lewagent-legacy/docs/todo.md`, 14a).
- **Its service starts.**
- **Lewbox itself:** `agentd` and each private port's `portd` run as sessions.

**What's wrong:**

1. **Polling.** The 5 s poll runs on every replica. It also counts as activity, so a watched session keeps its sandbox awake by accident rather than by design.
2. **Toolbox names aren't unique.** A create with a running session's name starts another (T2 in [toolbox](toolbox.md#problems-and-where-each-was-seen)). That's harmless for user sessions, which are named by id, but it broke private ports (`5f3d19e`).
3. **Exit codes are lost under gVisor**, as above (gVisor isn't in the alpha; the fork fixes it, F1, before it returns).
4. **A session's log is gone** once it's killed or lost, and when its sandbox stops. That's fine, but Lewagent's Build panel would like the last lines of a failed step after a restart.

## Goals

- Keep the documented API: fields, statuses, log and `tail`, attach replay, resize, signal, kill.
- A watched session's end reaches the activity log and webhooks within a second or two, without polling, once nodes have the fork's session events.
- The right exit code, every runtime, login shells included.
- Sessions that must run once (Lewbox's helpers, a project's `sshd`) can be started idempotently, by name.

### Non-goals

- Sessions surviving a stop. Processes don't survive a stop; persisted ports and templates' own commands are how things come back ([sandboxes/persistence](../sandboxes/persistence.md)).
- Storing session output in Lewbox.

## Design

### Data model

`sandbox_sessions`:

| Column | Type | |
|---|---|---|
| `id` | `bigint` pk | |
| `public_id` | `text` unique | `ses_…` |
| `project_id`, `sandbox_id` | fk | |
| `name` | `text` null | A caller's name, unique among the sandbox's running sessions (partial unique index `WHERE status = 'running' AND name IS NOT NULL`). Lewbox's own: `lewbox.agentd`, `lewbox.portd.<port>` |
| `toolbox_id` | `text` null | The toolbox's id (`ses-<16 hex>`), null until started |
| `command` | `text` null | `null` for a login shell |
| `directory` | `text` null | |
| `pty`, `watch` | `bool` | |
| `cols`, `rows` | `int` | |
| `internal` | `bool` | Lewbox's own (agentd, portd): hidden from lists unless `?internal=true` with an admin key |
| `status` | `text` | `running`, `exited`, `killed`, `lost` |
| `exit_code` | `int` null | `-1` only when the status couldn't be recovered |
| `exit_signal` | `text` null | |
| `started_by` | `jsonb` | The actor: user, profile key, sandbox key's maker, admin key, or Lewbox |
| `label` | `text` null | |
| `created_at`, `ended_at` | `timestamptz` | |

Index on `(sandbox_id, created_at desc)`, and a partial index on `(watch) WHERE status = 'running'`.

### Start

`POST /v0/sandboxes/{sbx}/sessions`

| Field | Type | |
|---|---|---|
| `command` | string | Run in a login shell, as root. Left out: a login shell of its own |
| `directory` | string | Absolute or `~`; must exist |
| `env` | object | At most 100. Never stored |
| `pty` | boolean | Default `false` |
| `cols`, `rows` | integer | 1–1000. Default 80 × 24 |
| `watch` | boolean | Record its end within seconds, and keep the sandbox awake while it runs. Default `false` |
| `name` | string | **New.** 1–64 of `[a-z0-9._-]`. With a name, a running session of that name is answered as it is (`200`) rather than started again; without one, every call starts a session (`201`) |
| `label` | string | For the activity log |

Steps:

1. Validate. Check the directory exists ([toolbox](toolbox.md#login-shells-and-directories)).
2. **In one transaction:** with a `name`, look for a running session of that name and answer it if there is one; otherwise insert the row as `running` with no `toolbox_id`. The partial unique index makes two racing starts of the same name end with one row: the loser answers the winner's.
3. **Start it in the toolbox,** named after its `ses_` id, with `if_absent: true` (F2), so the toolbox itself never starts a second session of that name. The command is wrapped in a login shell.
4. **Record** its `toolbox_id`.
5. **If the toolbox refuses**, mark the row `exited` with `exit_code: -1` and answer the error.

A crash between steps 2 and 4 leaves a row without a `toolbox_id`. Reading it looks the session up in the toolbox by name, which is its `ses_` id, as legacy's `aerolvmSessionID` does, and records what it finds. A row still without one after 60 s is `lost`.

Answers `201` (or `200` for a named one already running) with the session. Writes `session.started`.

### The session object

As documented: `id`, `command`, `directory`, `pty`, `watch`, `status`, `exit_code`, `exit_signal`, `created_at`, `ended_at`. Adds `name` and `label`.

### Statuses

| Status | |
|---|---|
| `running` | The process is running |
| `exited` | It ended on its own, with `exit_code`, or by a signal, with `exit_signal` too |
| `killed` | Killed with a delete |
| `lost` | It was running when its sandbox stopped, or its node went |

### List and get

- `GET /v0/sandboxes/{sbx}/sessions`: newest first. Filter by `status` and `name`. Cursor pages, `per_page` up to 100.
- `GET /v0/sandboxes/{sbx}/sessions/{ses}`.
- Both work whatever the sandbox's status.
- **Reading brings sessions up to date** while the sandbox runs. Running ones are checked with the toolbox (one list call per request, not one per session), and ended ones are recorded. If the toolbox can't be asked, Lewbox's view is answered. A session the toolbox doesn't know is left running if it's under 60 s old, as it may be starting; otherwise it's `lost`.

### Exit codes

Recording an end (`internal/sessions.RecordEnd`), shared by reads and events:

1. **The toolbox says how it ended:** an exit code, or a signal. Record that, as it is. On Docker it's right, login shells included; there are no status files or traps ([toolbox](toolbox.md#exit-codes)).
2. **Record conditionally:** `UPDATE … SET status = …, ended_at = now() WHERE id = $1 AND status = 'running'`. A second recorder, whether a racing read or another replica, changes nothing and sends nothing.
3. **Only the recorder that changed the row** writes `session.ended` (with `exit_code`, `exit_signal`, `duration_ms`).

### Watch

`watch: true` asks Lewbox to record the session's end within seconds, so `session.ended` reaches the activity log and `activity.session.ended` reaches webhooks without anyone reading it. Lewagent's setup waits on these.

- **From the fork's events (F36, in the minimum release):** session ends arrive on the node's event stream, forwarded by `lewbox node` ([clusters/node-agent](../clusters/node-agent.md#lifecycle-events)), with `session_ended` for every session on the node. Each is recorded as above. Events for unwatched sessions are recorded too, cheaply, which keeps statuses right without reads. There's no polling watcher.
- **Keeping the sandbox awake.** While a watched session runs, its sandbox counts as active: the fork counts a running session started with `keep_awake: true` (part of F36's change) as activity, and Lewbox starts watched sessions that way. It's deliberate and documented, not a side effect of polling. Unwatched sessions don't keep a sandbox awake, so a dev server can still let its sandbox go idle.

### Log

`GET /v0/sandboxes/{sbx}/sessions/{ses}/log` answers `text/plain`: the toolbox's buffer, the last 1 MiB of stdout and stderr together.

- `tail=N` (1–10000) answers only the last `N` lines. A trailing newline doesn't start a new line (legacy's `lastLines`).
- An exited session keeps its log until the sandbox stops. A killed or lost session's log is gone: `409 session_not_running`.
- **Later:** keep the last 64 KB of a watched session's log in Lewbox when it ends, so a failed setup step's output survives a stop. Lewagent's Build panel wants this.

### Attach

`GET /v0/sandboxes/{sbx}/sessions/{ses}/attach`, a WebSocket with the same auth, pings and messages as [interactive commands](commands.md#interactive), with these differences:

- **No first message.** The buffered output (up to 1 MiB) is replayed first, then the session is followed live.
- **Binary messages are stdin**; `resize` and `signal` work.
- **`{"type":"close"}` detaches** rather than closing stdin.
- **Several clients at once** see the same output; input from any of them goes to the process.
- **Detaching never stops a session,** whether on purpose or because the connection dropped.
- **When it ends:** `{"type":"exit","code":…,"signal":"…"}`, then `1000`, and the end is recorded.
- **`1001`** means the replica is shutting down; the session carries on, so reattach.
- **Ended sessions can't be attached to:** `409 session_not_running`. Read the log instead.
- **Data moving counts as activity**; an idle terminal doesn't.

### Resize, signal, kill

- `PATCH /v0/sandboxes/{sbx}/sessions/{ses}` with `{cols, rows}` or `{signal}`, never both (`validation_failed`). Answers the session.
  - A signal is only sent; the status changes once the process ends.
  - Writes `session.signalled`.
- `DELETE /v0/sandboxes/{sbx}/sessions/{ses}` kills the session and records it `killed`.
  - Answers `204`, even for one that had already ended, which is left as it ended.
  - Writes `session.killed`.

### Stops and starts

- **Before a stop** that's asked for, nothing is done to sessions: processes end with the sandbox.
- **When a sandbox stops**, for any reason (asked for, idle, lifetime, credits, or a lost node), its running sessions become `lost` in the same transaction that records the stop ([sandboxes/lifecycle](../sandboxes/lifecycle.md)). One `session.ended` event per watched session, with `outcome: lost`.
- **After a start** nothing is started again, except Lewbox's own:
  - `portd` for persisted private ports ([private ports](private-ports.md));
  - `agentd` on the next prompt ([agents/agentd](../agents/agentd.md)).
- **No `lewbox.command` session.** Legacy started the image's command itself for sandboxes whose toolbox nested on resume; the fork fixes resumes, including legacy snapshots (F10), so Lewbox doesn't.

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.exec`, even to list or read: commands and logs can hold secrets |
| `404` | `not_found` | No such sandbox, or no such session in it |
| `409` | `sandbox_not_running` | Starting, resizing, signalling, killing, attaching or reading the log while the sandbox isn't running |
| `409` | `session_not_running` | Resizing, signalling, attaching or reading the log of a killed or lost session |
| `422` | `validation_failed` | `cols`/`rows` out of range, a missing directory, both a size and a signal, `tail` of 0, a bad `name` |
| `422` | `sandbox_refused` | |
| `503` | `toolbox_unavailable` | |

## Security

- **`env` is never stored.** The toolbox has it for the process's life only.
- **Lewbox's own sessions** (`internal`) are hidden from profile and sandbox keys. Killing one would break private ports or agents, so a profile key's delete of an internal session is `403 permission_denied`.
- **Any client with `sandboxes.exec` can attach to any session in the sandbox** and type into it, including one another client started. That's by design: it's one machine.

## Observability

- **Metrics:**
  - `lewbox_sessions_started_total{watch, pty}`;
  - `lewbox_sessions_ended_total{status}`;
  - `lewbox_sessions_running`;
  - `lewbox_session_end_lag_seconds`, from the process ending to it being recorded: polling versus events;
  - `lewbox_session_exit_status_recovered_total`;
  - `lewbox_session_attaches_open`.
- **Activity:** `session.started`, `session.ended`, `session.killed`, `session.signalled`.
- **Webhooks:** `activity.session.ended` and the others.

## Configuration

| Setting | Default | |
|---|---|---|
| `LEWBOX_SESSION_WATCH_INTERVAL` | `5s` | Polling, for nodes without `session_events` |
| `LEWBOX_SESSION_WATCH_CONCURRENCY` | `8` | Sandboxes polled at once |
| `LEWBOX_SESSION_START_GRACE` | `60s` | How long a session the toolbox doesn't know yet stays `running` |

## Testing

- **Create:** with and without `name` (idempotent, racing starts end with one session); with a login shell; with a missing directory; the toolbox refusing.
- **Crash recovery:** a crash between insert and start, then a read finds it by name.
- **Ends:** an exit, a signal, a kill, and a sandbox stop that loses every session.
- **Exit codes:** as the toolbox reports them, login shells included.
- **Recording:** conditional, so two recorders write one event.
- **Events:** session ends from the fake event stream, replayed after a reconnect, recorded once.
- **Log and tail:** edge cases ported from `lastLines`.
- **Attach:** replay then follow; several clients; detach leaves it running; `1001` on shutdown; ended sessions refused.
- **Internal sessions:** hidden from lists, refused to profile keys.

## Build plan

1. Migration and `internal/sessions` (store, `RecordEnd`).
2. Handlers, ported from `sessions.go`, on the shared request path.
3. Session ends from the node's events (needs fork F36); `if_absent` starts (F2).
5. Named sessions used by portd and agentd.

## Open questions

1. **Keep the last lines of a watched session's log** in Lewbox when it ends (64 KB?), so a step's failure is still readable after a stop?
2. ~~**Who holds the node's event stream.**~~ Decided: `lewbox node` forwards it over its own connection ([clusters/node-agent](../clusters/node-agent.md#lifecycle-events)), which also works through the tunnel (beta).
3. **Should unwatched sessions' ends also be webhooks?** They'd be recorded once events are pushed, so it costs little, but it changes what `watch` means.
4. **Sessions' `env`.** Lewagent wants per-conversation env (`LEWAGENT_SESSION`). That's an agents concern ([agents/conversations](../agents/conversations.md)), but if sessions grow a `name`, should they also carry visible metadata?
