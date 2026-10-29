# Streaming

> Status: Draft · Milestone: Alpha (M2 commands and sessions; M3 turn events; M4 live changes) · Updated: 2026-10-29

Some of the API isn't request and response: a command's output as it runs, a terminal, an agent's turn, a sandbox changing state. Lewbox streams these over **server-sent events** (one-way, plain HTTP, resumable) and **WebSockets** (two-way, for input). This plan sets the conventions every stream follows, so the protocols documented at [lewbox.com/docs](https://lewbox.com/docs/inside-a-sandbox/streaming-output) stay consistent as streams are added, and says how streams work across replicas, when credentials are revoked, and when a server shuts down.

## Today

| Stream | Kind | Source | Code |
|---|---|---|---|
| Command output, `POST …/commands` with `Accept: text/event-stream` | SSE | toolbox exec stream | `lewbox-gateway-legacy/internal/server/commands.go`, `sse.go` |
| Interactive commands, `GET …/commands/stream` | WebSocket | toolbox exec stream | `commands.go`, `websocket.go` |
| Session attach, `GET …/sessions/{ses}/attach` | WebSocket | toolbox session attach (replays buffer, then follows) | `sessions.go` |
| Turn events, `GET /v1/conversations/{cnv}/messages/{msg}/events` | SSE | `lewbox-agentd events --follow` run over the toolbox exec stream, stdout lines relayed | `conversations.go` |

Lessons and gaps:

- **`Last-Event-ID` wasn't in the CORS allowed headers**, so browsers resume with `?after=` (Lewagent's `resources/js/lib/agents.ts`). Both work; the header should too.
- **`finished` is held back until uploaded** (up to 5 s), so a client that sees it can prompt again at once; a good rule to keep.
- **Shutdown** sends `gateway_restarting` and closes WebSockets with `1001`; commands die with the stream.
- **No live changes stream.** Lewagent polls sandbox status every minute server-side and every few seconds in the browser, alongside webhooks, and re-reads conversation status to catch a missed `finished` (`lewagent-legacy/routes/console.php`, `resources/js/components/computer-polling.tsx`).
- **Each turn-event subscriber runs its own `agentd events --follow`** in the sandbox, so ten tabs following one turn are ten exec streams.
- **Exit codes are lost on exec streams under gVisor** (the trap that fixes `Execute` wasn't applied to streams; [lessons](../lessons.md)).
- Activity is counted only when a toolbox request *starts*, so long streams touch the toolbox's health endpoint at most once a minute while data moves.

## Goals

- One set of conventions for every SSE stream and every WebSocket.
- Every stream that *can* resume does, by event id, with the header or the query parameter.
- Streams survive the replica they're on going away, where the work behind them survives (turns, live changes); and say clearly when it didn't (commands).
- One upstream per thing being watched per replica, however many clients follow it.
- A live changes stream, so clients can stop polling.

---

## Design

### When SSE, when WebSocket

| Use | When |
|---|---|
| **SSE** | Server to client only; resumable; works through every proxy; works with `fetch` in browsers (with headers) and with `curl`. Command output, turn events, live changes, file changes |
| **WebSocket** | Input from the client too: stdin, resize, signals. Interactive commands, session attach. Node tunnels (internal) |

### SSE conventions

```
id: 15
event: text
data: {"id":15,"type":"text","at":"2026-10-29T18:40:12.345Z","turn":2,"message":"msg_…","text":"Hello"}

: keepalive

```

- **`Content-Type: text/event-stream`**, `Cache-Control: no-store`, `X-Accel-Buffering: no` (so nginx and similar don't buffer), no compression.
- **Each event**: `id:` where the stream is resumable, `event:` the type, one `data:` line holding the whole event as one line of JSON, with its `id` and `type` again so clients that ignore SSE framing still have them. A blank line ends it.
- **Keepalive**: a `: keepalive` comment every 15 s while quiet (`LEWBOX_SSE_KEEPALIVE`). Comments never count as sandbox activity.
- **Text, not bytes**: data is UTF-8; a character split across upstream chunks is held back until whole; invalid bytes become U+FFFD. Binary output belongs on a WebSocket.
- **Errors before the stream** (bad key, sandbox not running, invalid field) are ordinary JSON errors with their status. Once the response is `200`, problems arrive as an `error` event.
- **Every stream ends with exactly one terminal event** (`exit`, `finished`, or `error`), then closes. A client that sees the connection close without one knows it was cut, and resumes or reports.
- **`?types=`** filters events by type where offered; terminal events always come.

### Resuming

Resumable streams number their events. The numbers are positions the client already holds, not cursors:

| Stream | Event ids | Resume window |
|---|---|---|
| Turn events | The event's number in the conversation (across turns) | The conversation's whole log while its sandbox runs; the stored chunks after ([agents](../agents.md)) |
| Live changes | The outbox sequence number | 24 hours (`LEWBOX_EVENTS_RETENTION`) |
| Command output | None: not resumable (the command dies with the stream) | — |
| File changes (Later) | Sequence per watch | While the watch lives |

Resume with **either** the `Last-Event-ID` header **or** `?after=<id>`; both mean "start after this one". If both are sent, the larger wins. `Last-Event-ID` is now in the CORS allowed headers ([conventions](conventions.md#cors)), so browsers using `fetch` can send it; `EventSource` sends it on its own reconnects. An id from before the resume window answers `error` with `code: events_expired` and closes; the client re-reads state instead (`GET` the conversation, or the resources it watches).

### WebSocket conventions

Kept as documented for interactive commands and attach:

- **Authenticate** with `Authorization: Bearer`, or subprotocols `lewbox.bearer, <token>` (the server selects `lewbox.bearer` only), or the session cookie from the instance's own origin ([authentication](authentication.md#presenting-credentials)).
- **Refusals before the upgrade** are JSON errors with their status (`404`, `409 sandbox_not_running`, `403`, `503 toolbox_unavailable`, `426 upgrade_required`).
- **A first text message** configures the stream where needed (`{command, directory, env, pty, cols, rows, label}`), within 10 s, else an error message and close `1008`.
- **Binary messages carry bytes**: output prefixed with one byte for the stream (`0x01` stdout, `0x02` stderr); input unprefixed.
- **Text messages carry control**: `{"type": "resize"|"signal"|"close"}` from the client; `{"type": "exit", "code", "signal"}` or `{"type": "error", "code", "message"}` from the server as the last message.
- **Pings** every 30 s (`LEWBOX_WEBSOCKET_PING_INTERVAL`); a client that misses two is gone.
- **Messages up to 1 MiB**; output is chunked below that.

**Close codes**:

| Code | Means |
|---|---|
| `1000` | Ended normally, after the last message |
| `1001` | The server is shutting down; reconnect (a command was killed; a session carries on and can be attached again) |
| `1008` | The first message was late or invalid, or the stream broke protocol |
| `1011` | The sandbox's stream ended before the command or session did, so how it ended isn't known (also when the sandbox stopped) |
| `4403` | The credential was revoked or expired as its parent was, or the profile or project suspended (new) |
| `4429` | Too many open streams for this credential or sandbox (new; normally refused before the upgrade) |

### Activity and streams

Data moving on a stream counts as sandbox activity for idle timeouts, checked at most once a minute (`LEWBOX_ACTIVITY_TOUCH_INTERVAL`), touching the toolbox so AerolVM sees it too; keepalives and pings don't, so an abandoned terminal doesn't keep a sandbox running ([sandboxes](../sandboxes.md)). A turn following doesn't need this: agentd keeps the sandbox awake while a turn runs, whether or not anyone follows it.

### One upstream per replica

Each replica keeps a **hub** per watched thing:

- **Turn events**: one `agentd events --follow` per conversation per replica, started for the first subscriber and stopped 30 s after the last leaves. Each subscriber has its own position; the hub replays from agentd's log for a subscriber that starts behind (agentd keeps the numbered log in the sandbox), then shares the live tail.
- **Session attach** stays one upstream per client: the toolbox's attach replays its buffer per connection, and input is per client.
- **Commands** are one upstream per request by nature.

Subscribers with slow connections get a bounded buffer (1,000 events or 4 MiB); a subscriber that falls further behind is closed with `error` `code: slow_consumer` and resumes by id. A slow client can never stall the upstream or other subscribers.

### Across replicas

Most streams don't need other replicas: their source is the sandbox, which any replica can reach. What does cross replicas goes through Postgres `LISTEN/NOTIFY`, which every replica already holds a connection for ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)):

| Channel | Payload | Used for |
|---|---|---|
| `outbox` | the new sequence number | Live changes and webhook delivery wake up and read new rows |
| `operation_done` | `op_…` | A request waiting on an operation (a create's 60 s wait, a replayed create) finishes when the worker on another replica does |
| `turn_uploaded` | `msg_…` | The hub releases a held-back `finished` once agentd's final upload has been stored, whichever replica received it |
| `auth_changed` | `kind:id` | Revocation: drop cached principals and close streams they opened |
| `stream_cancel` | `sandbox:sbx_…` | A sandbox stopping or being deleted closes its streams everywhere |

Payloads are ids only (NOTIFY payloads are capped at 8,000 bytes, and the data is in rows anyway). A replica that loses its listening connection reconnects, then treats it as "anything may have changed": it re-reads the outbox from its last sequence, refreshes held-back turns, and flushes its auth cache.

### Live changes

`GET /v0/events` (M4): one SSE stream of changes to everything the principal may read, so clients can drop polling.

- **Events are the webhook events** ([activity-and-webhooks](../activity-and-webhooks.md)): `sandbox.running`, `sandbox.stopped` (with `reason`), `operation.succeeded`, `operation.failed`, `conversation.status_changed`, `turn.finished`, `build.ready`, `build.failed`, `session.ended`, `port.exposed`, … The same payloads and the same names, from the same **outbox** table, so a client can use webhooks, the stream, or both, and see the same thing.
- **Filtered per principal**: a profile key sees its profile's; a sandbox key its sandbox's; an admin key or session its project's (`project=` for sessions). Filters: `types=`, `sandbox=`, `profile=` (admin, session).
- **Resumable** by the outbox sequence for 24 hours.
- **Not a replacement for webhooks** for server-side integrations (no delivery guarantees when nobody's connected), but the right thing for dashboards and SDK waits: the SDKs' `waitUntilRunning()` can follow it instead of polling the operation.

### Turn events specifics

Kept from legacy and the docs ([agents](../agents.md) owns the event types):

- Events are numbered across the whole conversation; a turn's stream sends only that turn's.
- Leaving doesn't stop the turn.
- `finished` is held back until what Lewbox stores of the turn is stored (up to 5 s), then ends the stream.
- A steered turn's stream ends with `finished` (`stop_reason: steered`, `next_message`), and the client follows the next message's events after that event's id.
- A queued turn's stream waits with keepalives until it starts.
- Stream errors: `stream_interrupted` (agentd's stream ended without `finished`), `server_restarting`. Both resumable by id.

### When a credential is revoked

Streams are authenticated when they open. Afterwards:

- **Revocation, suspension, a parent key's revocation, a member's removal**: the `auth_changed` notification closes every stream opened with that principal, on every replica, within about a second: SSE with an `error` event `code: key_revoked`, WebSockets with close `4403`.
- **A sandbox key's own 15 minutes running out** doesn't close its open streams. A terminal outlives its key; the browser mints a new key for its next connection ([authentication](authentication.md#revocation-and-expiry)).
- **Streams have no maximum duration** otherwise; a turn waiting on an approval may stream for an hour.

### When the server shuts down

On `SIGTERM` ([foundation/binary-and-commands](../foundation/binary-and-commands.md)):

1. **Stop accepting** new connections; `/ready` answers `503` so the load balancer drains this replica.
2. **Let short work finish**: wait up to `LEWBOX_SHUTDOWN_DRAIN` (20 s) for command streams to end on their own.
3. **Close the rest**: SSE streams get `error` with `code: server_restarting`; WebSockets get `{"type": "error", "code": "server_restarting"}` and close `1001`. Turn streams and live changes resume on another replica by event id; commands still running were killed with their stream (as documented: work that must survive belongs in a session); attached sessions carry on in the sandbox and can be attached again.
4. Exit within `LEWBOX_SHUTDOWN_TIMEOUT` (30 s).

`server_restarting` replaces legacy's `gateway_restarting` ([errors](errors.md#stream-errors)).

### Through proxies and load balancers

Documented for self-hosters ([foundation](../foundation.md)):

- Idle timeouts above 60 s (SSE keepalives every 15 s, WebSocket pings every 30 s).
- No response buffering for `text/event-stream`.
- WebSocket upgrades passed through.
- HTTP/2 is fine for SSE (many streams on one connection, avoiding the browser's six-connections-per-host limit for HTTP/1.1, which a dashboard with several chats would hit).

---

## Observability

- Metrics: `lewbox_streams_open{kind}`, `lewbox_stream_duration_seconds{kind,end}`, `lewbox_stream_events_total{kind}`, `lewbox_stream_hub_subscribers{kind}`, `lewbox_notify_received_total{channel}`, `lewbox_stream_slow_consumers_total`.
- A stream's end is logged once with its reason (`exit`, `client_left`, `server_restarting`, `revoked`, `upstream_ended`, `slow_consumer`) and duration.
- Interactive commands and attaches are activity events (`terminal.opened`, `terminal.closed`), as in legacy.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_SSE_KEEPALIVE` | `15s` | |
| `LEWBOX_WEBSOCKET_PING_INTERVAL` | `30s` | |
| `LEWBOX_ACTIVITY_TOUCH_INTERVAL` | `60s` | How often moving data touches the sandbox |
| `LEWBOX_EVENTS_RETENTION` | `24h` | Live changes resume window |
| `LEWBOX_SHUTDOWN_DRAIN` | `20s` | Grace for command streams |
| `LEWBOX_SHUTDOWN_TIMEOUT` | `30s` | |

## Testing

- SSE framing golden tests; UTF-8 boundary fuzzing (kept from the gateway's `sse.go` tests).
- Resume by header and by query, across a replica restart, for turns and live changes.
- Two replicas: a create waits on one while the operation finishes on the other; a revocation on one closes a stream on the other.
- Hub: 100 subscribers on one turn make one upstream; a slow subscriber is cut without slowing others.
- Shutdown: commands within the drain finish normally; others get `server_restarting`.
- The fake toolbox and fake agentd (from the gateway's `aerolvmtest` and agentd's fake adapter) drive all of these ([foundation/testing](../foundation/testing.md)).

## Build plan

`internal/httpapi/stream`: SSE writer (framing, keepalive, UTF-8 assembly, flush); WebSocket helpers on `coder/websocket` (auth by subprotocol, first message, ping, close codes, relay with kill-on-drop); hub with per-subscriber buffers; `internal/db/notify` (one listener connection, channel dispatch, reconnect handling); shutdown coordinator. M2 for SSE and WebSocket helpers; M3 for hubs and `turn_uploaded`; M4 for `/v0/events`.

## Open questions

1. **Live changes for profile keys in the alpha**, or only sessions (the dashboard) at first? (Proposed: all principals in M4; it's one filter.)
2. **Exec-stream exit codes under gVisor**: fix in the AerolVM fork (preferred, [clusters/aerolvm-fork](../clusters/aerolvm-fork.md)) or apply the `Execute` trap to streams too? Interactive commands need the right code either way.
3. Should commands optionally survive the stream (`detach_on_disconnect`) by becoming a session, rather than being killed? (Proposed: no; sessions exist for that. Keep commands simple.)
4. Should the dashboard connect to node domains directly for terminals (lower latency, less control-plane load), with sandbox keys checked at the node? (Proposed: Later; it needs auth at the node.)
