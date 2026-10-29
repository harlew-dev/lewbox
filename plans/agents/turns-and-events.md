# Turns and events

> Status: Draft · Milestone: Alpha (M3) · Core (agent framework) · Updated: 2026-10-29

Everything an agent does in a turn arrives as events: its reasoning and text as it writes them, each tool call as it starts and changes, its plan, approvals and questions, usage, and how the turn ended. Clients follow a turn as server-sent events (SSE), reconnect after the last event they saw, and get each event once. This plan covers the event model, the stream, and where events live in the new design: in Postgres, so any replica can serve them and a finished turn can be replayed even when its sandbox is stopped.

## Today

- **Contract:** [lewbox.com/docs/agents/events](https://lewbox.com/docs/agents/events).
- **agentd** maps ACP updates to events, numbers them across the conversation, appends them to `/root/.lewbox/agents/<cnv>/events.jsonl` (`internal/agentd/events.go`, `conversation.go`), and serves them through `lewbox-agentd events --conversation --after (--turn | --message) --follow` (`--idle-exit` 5 min; `--open-idle-exit` 1 h while the turn is open, `f91b9f6`; `--wait-uploaded` holds `finished` up to 5 s until what's stored has landed, `56a4a76`).
- **The gateway** serves `GET /v1/conversations/{cnv}/messages/{msg}/events` by running that command over the toolbox's exec-stream WebSocket and relaying each stdout line as an SSE event (`internal/server/conversations.go`, `streamEvents`, line 1029), with `: keepalive` every 15 s, `?types=` filtering, and `Last-Event-ID` or `?after=` resume.
- **Lewagent** (`resources/js/lib/agents.ts`) follows with `fetch`, resumes with `?after=` because its CORS preflight didn't allow `Last-Event-ID` (the gateway's allowed headers are Authorization, Content-Type, Idempotency-Key, X-Lewbox-Label), reconnects after a quiet timeout, and reads the conversation's status as a fallback when it misses a `finished`.

**Problems:**

1. **A stream is a process in the sandbox.** Each follower runs `agentd events --follow` over exec-stream; a stopped sandbox can't replay even a finished turn ("while its sandbox is running").
2. **`finished` had to be held back** until the upload landed, because the app learned of a turn's end separately from the stream (`56a4a76`).
3. **Turns the agent starts itself** aren't announced by any stream; clients poll the messages list ([building-a-chat-page § Known gaps](https://lewbox.com/docs/agents/building-a-chat-page#known-gaps)).
4. **Sub-agents' work runs into the main reply** (Claude's sub-agent text and tool calls arrive as the main agent's).
5. **An interrupted turn ends only when the sandbox starts again**; until then a follower gets `stream_interrupted` and the conversation reads `running`.

## Goals

- Keep the event types, fields, numbering and SSE format.
- Serve every stream from the control plane, from Postgres, live via `LISTEN/NOTIFY`, on any replica.
- One stream for a whole conversation, so turns the agent starts itself are seen as they start.
- `finished` arrives once what's stored is complete, by construction, not by waiting.
- End interrupted turns as soon as Lewbox knows the sandbox stopped.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Every event type and field; ids numbered across the conversation; three-line SSE events; `: keepalive` every 15 s; `?types=`; `Last-Event-ID` and `?after=`; streams ending after `finished`; `stream_interrupted` |
| **Change** | Events are stored in Postgres and served from there ([Where events come from](#where-events-come-from)); `gateway_restarting` becomes `server_restarting`; `Last-Event-ID` added to the CORS allowed headers; interrupted turns ended by the control plane |
| **Add** | `GET /v0/conversations/{cnv}/events` (every turn); `parent_call_id` on sub-agents' events where the adapter reports it; `started_at` and `ended_at` on tool calls (`ae373a4`) in the docs; replay of finished turns while the sandbox is stopped |
| **Drop** | Holding `finished` back in agentd (`--wait-uploaded`), on the channel path |

## Design

### Event model

Every event has:

| Field | Type | |
|---|---|---|
| `id` | integer | Its number in the conversation, from 1, the SSE `id:` line |
| `type` | string | Below. New types may appear: ignore unknown ones |
| `at` | string | ISO 8601 UTC, to the millisecond |
| `turn` | integer | The turn's number in the conversation |
| `message` | string | The turn's `msg_…`. Left out of a turn the agent started itself until it's recorded |
| `parent_call_id` | string | **New.** On events from a sub-agent: the tool call (Claude's `Task`/`Agent` call) that started it. Left out otherwise |

Types, mapped from ACP by agentd (legacy `docs/acp.md` § Events):

| Type | Fields | From ACP |
|---|---|---|
| `started` | `origin` (`prompt`, `agent`), `conversation`, `agent`, `model`, `mode`, `prompt` (blocks; prompted turns only), `steered` (steered turns), `output_schema` | The turn began |
| `reasoning` | `text`, `message_id` | `agent_thought_chunk` |
| `text` | `text`, `message_id` | `agent_message_chunk` |
| `tool_call` | `call_id`, `title`, `kind`, `status`, `input`, `output`, `content`, `locations`, `started_at`, `ended_at` | `tool_call` and `tool_call_update`, merged into the call's **whole** current state; sent only when it changes |
| `tool_output` | `call_id`, `terminal_id`, `text` | Codex's `_meta.terminal_output_delta` (`b89e993`) |
| `plan` | `entries` (`content`, `priority`, `status`) | `plan`; each replaces the last |
| `approval_request` | `request`, `call_id`, `title`, `kind`, `options`, `expires_at` | `session/request_permission` ([approvals-and-questions](approvals-and-questions.md)) |
| `question` | `request`, `text`, `schema`, `call_id`, `expires_at` | `elicitation/create` (form) |
| `answered` | `request`, `by`, `option`, `answers`, `declined` | A reply, a timeout, a cancel or the turn's end |
| `title` | `title` | `session_info_update` |
| `mode` | `mode` | The adapter's mode settings, in Lewbox's words |
| `commands` | `commands` (`name`, `description`) | `available_commands_update`, only when they change, without `/login`, `/logout` |
| `usage` | `context_tokens`, `context_size`, `cost`, `currency`, `rate_limit` | `usage_update`; `cost` is the turn's so far (Claude's running total minus the last report, `4c370f2`) |
| `task` | `task` | [background-tasks](background-tasks.md) |
| `finished` | `stop_reason`, `usage`, `output`, `origin`, `sender`, `next_turn`, `next_message` | The turn ended |
| `error` | `text`, `url` | The adapter failed or exited (with the end of `adapter.log`), the prompt failed, the turn was cut short, or a URL elicitation Lewbox can't show (the agent is told no) |
| `raw` | `update`, or `method` and `params` | Anything not mapped yet, so nothing is hidden |

**Tool calls.** `kind` is `read`, `edit`, `delete`, `move`, `search`, `execute`, `think`, `fetch`, `switch_mode` or `other`; `status` is `pending`, `in_progress`, `completed`, `failed`, or `unreported` (sent once, just before `finished`, for a call the agent never said had ended, `84a9a50`). `content` items: `text` (`text`), `diff` (`path`, `old_text`, `new_text`; `old_text` null for a new file), `terminal` (`terminal_id`, `directory`, `exit_code`, `signal`), or `{type, content}` with the agent's own. `input` and `output` are the agent's own shapes; clients should draw from `title`, `kind` and `content`. A call waiting for permission stays `pending` until answered.

**Sub-agents** (new). Claude's sub-agent messages carry `parent_tool_use_id` in the Agent SDK's messages, which agentd already receives as `_claude/sdkMessage` notifications for background tasks. agentd tags `text`, `reasoning` and `tool_call` events from a sub-agent with `parent_call_id`, so a client can draw them inside the sub-agent's card rather than in the main reply. Where the adapter doesn't expose the link (Codex today), events are untagged as now. Needs a spike to confirm the ACP updates can be matched to the SDK messages.

### Stop reasons

| `stop_reason` | Turn's status | Means |
|---|---|---|
| `end_turn` | `completed` | The agent finished |
| `max_tokens` | `completed` | Hit the model's output limit |
| `max_turn_requests` | `completed` | Hit its request limit in one turn |
| `refusal` | `completed` | The model refused to carry on |
| `cancelled` | `cancelled` | Cancelled |
| `failed` | `failed` | Couldn't start, exited, or refused the prompt; an `error` says why |
| `interrupted` | `interrupted` | The sandbox stopped mid-turn; the next prompt resumes the session |
| `steered` | `completed` | A prompt was steered in; `next_turn`, `next_message` carry on |
| `null` | `completed` | A turn the agent started itself |

Unknown stop reasons are treated as `completed`. A `failed` turn sets the conversation `failed` until its next turn.

### Where events come from

agentd sends each event up the [agent channel](agentd.md#the-agent-channel) as it's written to its log; the control plane stores it and wakes followers. There's no second path ([agentd § No second path](agentd.md#no-second-path)).

```
agentd ──events frame──▶ replica A ── INSERT agent_events … ON CONFLICT DO NOTHING
                                     └ NOTIFY agent_events, '<cnv>:<id>'
follower on replica B ◀── SSE ── replica B: LISTEN agent_events → SELECT … WHERE id > last
```

- **Table:** `agent_events (conversation_id, id, turn, message_id, type, at, data jsonb)`, primary key `(conversation_id, id)`. Inserts are idempotent, so agentd's resend after a reconnect is harmless. A gap (id N+2 before N+1) is held back from followers until it's filled or agentd's channel drops, as events must reach clients in order.
- **Volume:** text deltas are the bulk (a 250-word story was 117 events, ~470 bytes each, in legacy's spike). agentd coalesces consecutive `text` and `reasoning` deltas of one `message_id` over 50 ms into one event before sending, which cuts rows ~5–10× without a visible delay. Claude's `commands` event (~12 KB, twice per session start) is sent only when it changes.
- **Retention:** raw events are pruned `agents.event_retention` (7 days) after their turn ends. Older turns are read from chunks ([storage-and-uploads](storage-and-uploads.md#chunks)); following a pruned turn's events answers what's stored as a short synthetic replay (`started`, one event per chunk, `finished`), marked `"replayed": true`.
- **Fan-out:** one `LISTEN` connection per replica, demultiplexed in memory to that replica's followers; `NOTIFY` payloads carry only `cnv:id` (Postgres caps payloads at 8,000 bytes), and followers read rows in batches.
- **Ordering with what's stored:** the upload that records a turn's end and its last chunks is applied in the same transaction as the `finished` event's insert ([storage-and-uploads § Uploads](storage-and-uploads.md#uploads)). So a client that sees `finished` can prompt again or read what's stored at once, with no wait. On the channel, agentd sends the turn's final upload before the `finished` event, and the control plane holds `finished` until it has the upload.


### Following

**One turn:** `GET /v0/conversations/{cnv}/messages/{msg}/events`, `sandboxes.agents`.

- Sends the turn's events from its start (or after `Last-Event-ID` / `?after=`), then each new one, and closes after `finished`.
- A **queued** turn's stream waits with keepalives until it starts; its first event is `started`.
- A turn **steered into** ends with `finished` (`stop_reason: steered`, `next_message`); follow `next_message` after that event's id to carry on. SDKs do it for you.
- `?types=text,tool_call` sends only those types, plus `finished` always; keepalives go on. An unknown type is `422`. `?types=error` waits for a schema'd turn's answer alone.
- The stream only ever sends the turn asked for, whatever id it resumes after.
- **Works while the sandbox is stopped** for a turn that has ended (new). A turn that hasn't ended in a stopped sandbox is ended `interrupted` ([below](#interrupted-turns)), so its stream finishes too.

**A whole conversation (new):** `GET /v0/conversations/{cnv}/events`, `sandboxes.agents`. Every event of every turn after `Last-Event-ID` / `?after=` (default: the latest event, so it starts live), never closing on `finished`. It's how a chat page learns of a turn the agent starts itself the moment it starts, and how Lewagent's backend can follow a conversation without knowing its turns. Takes `?types=` too. Closes after an hour idle with `stream_interrupted`, to be reconnected.

### The format

```
id: 42
event: text
data: {"id":42,"type":"text","at":"2026-10-29T18:00:01.123Z","turn":3,"message":"msg_…","text":"Fixed the "}

: keepalive

```

- Three lines then a blank one. `data:` is the whole event as one line of JSON.
- `: keepalive` comments every 15 s while nothing else is sent.
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`, `Lewbox-Request-Id`.
- CORS allows `Last-Event-ID` as a request header (new), so browsers can resume with it as well as `?after=`.

### Leaving doesn't stop the turn

The turn runs in the sandbox whether anyone follows it. Closing a stream, crashing or losing the network leaves the agent working; follow again from where you left off. To stop the agent, [cancel](conversations.md#cancel-a-turn).

### Turns the agent starts itself

Claude moves long commands into the background and ends its turn; when one finishes, it carries on in a new turn with no prompt (`_meta._claude/origin: {kind: "task-notification"}`). agentd keeps the adapter running for 2 hours while background work isn't ambient, so that turn can happen ([background-tasks](background-tasks.md)).

- agentd opens it when the agent sends anything outside a turn (text, a tool call, a plan, an approval or a question): `started` with `origin: agent` and no `prompt` or `message`.
- The control plane records it as a turn (`origin: agent`) when the first event arrives, so the next events carry its `message`, and emits `activity.agent.turn_started`.
- It ends at Claude's `usage_update` with `cost`, or after `LEWBOX_AGENTD_AGENT_TURN_QUIET` (2 min) without an update, or when a prompt arrives (which ends it with `stop_reason: null` and starts the prompt's turn).
- **Peer turns:** when that `usage_update`'s `_meta._claude/origin` is `{kind: "peer", from, name}`, the turn replied to another Claude session in the sandbox; its `finished` has `origin: peer` and `sender`. The message it answered isn't passed on by Claude Code.
- Followers see it on the [conversation stream](#following) at once, or find it in the messages list and follow it by id.

### Interrupted turns

- **A stop that's asked for** (by a client, a timer, credits, an admin): the stop operation asks agentd to `shutdown` first. agentd cancels open turns, ends them `interrupted` (`error` "The sandbox is stopping." then `finished`), answers open requests `by: turn_end`, uploads, and exits. Bounded by 10 s; then the stop carries on.
- **A stop Lewbox learns of afterwards** (a lost node, an AerolVM timer the reconciler reports): the control plane ends each open turn itself, appending `error` and `finished` (`interrupted`) after the last stored event, and records the turn and conversation.
- **On agentd's next start,** `welcome` tells it, per conversation, the last event the control plane holds and that the turn is over. agentd truncates its log after the last acknowledged event, appends the control plane's closing events, and drops any events it wrote but never sent (seconds of a turn that's already over). The control plane is authoritative for a turn it has ended.
- The next prompt resumes the agent's session where it left off.

### Stream errors

Lewbox's own problems with a stream come as an `error` event **with no `id:` line**, then the stream closes; the turn carries on, so reconnect:

| Code | Means |
|---|---|
| `stream_interrupted` | Events stopped before the turn finished: the follower's replica lost the stream, or the conversation stream sat idle an hour |
| `server_restarting` | The replica carrying the stream is shutting down. Reconnect (any replica will do) |

An `error` with an `id:` is the agent's, part of the turn. SDKs reconnect after both codes and give up after three reconnects in a row that bring no new event, raising `stream_interrupted`.

**Errors before the stream** are ordinary JSON errors: `403 permission_denied`, `404 not_found` (no such conversation or message, or the message is a prompt), `422 validation_failed` (`after` or `Last-Event-ID` not a whole number from 0, an unknown type), `503 agents_unavailable`. `409 sandbox_not_running` and `502 agent_unavailable` no longer apply, as streams come from the control plane.

### Reading a turn without the stream

For clients that only need the outcome (Lewagent's workflow engine, a backend settling a task): `GET /v0/conversations/{cnv}/messages/{msg}?wait=60` holds until the turn ends or 60 s pass, and answers the turn with `status`, `stop_reason`, `usage`, `output` and `text` ([conversations § Messages](conversations.md#messages)). Webhooks `activity.agent.turn_ended` cover the rest ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)).

## Security

- Following needs `sandboxes.agents`; sandbox keys reach only their sandbox's conversations. A key is checked when a stream opens; a stream outlives its key, and only reconnecting needs a fresh one.
- Events are data from an untrusted sandbox, capped per event (1 MB; a larger one is cut like a chunk and marked `truncated`) and per conversation rate (a runaway adapter is throttled, its events coalesced, and an `error` logged).

## Observability

- Metrics: events stored per second, by type; channel-to-stored lag; open followers per replica; `NOTIFY` fan-out lag; synthetic replays served; interrupted turns by cause.
- Logs: stream opened and closed with conversation, turn, key, duration and events sent.

## Configuration

| Setting | Default | |
|---|---|---|
| `agents.sse_keepalive` | `15s` | |
| `agents.event_retention` | `7d` | After the turn ends |
| `agents.event_max_bytes` | `1MiB` | Per event |
| `agents.coalesce` | `50ms` | Sent to agentd |
| `agents.stream_idle` | `1h` | Conversation streams close after this idle |
| `agents.shutdown_wait` | `10s` | agentd's time to end turns before a stop |

## Testing

- A conversation of scripted events through the channel into Postgres, followed from two replicas (two servers on one database in a test), with resumes at every id, `?types=`, a gap held back, a reconnecting agentd resending, and `finished` arriving only after the final upload.
- Interrupted turns: asked stop, unannounced stop, then agentd restarting with unsent events on disk.
- agentd that never dials in: the prompt fails `agent_unavailable` within `agents.start_wait`, naming the channel URL.
- Retention: a pruned turn replayed from chunks.
- Browser: `Last-Event-ID` through CORS.

## Build plan

1. `agent_events`, insert path from uploads and the channel, the per-replica `LISTEN` hub (3 days).
2. Both stream endpoints, keepalive, filters, gap holding (2 days).
3. Interrupted turns: shutdown before stop, control-plane endings, `welcome` reconciliation (2 days).
4. Coalescing in agentd; `parent_call_id` spike and tagging (2 days).

## Open questions

1. **Postgres for raw events** vs only chunks plus a live relay. Events in Postgres give replay, cross-replica and stopped-sandbox reads; the cost is write volume, bounded by coalescing and 7-day pruning.
2. **Synthetic replay** of pruned turns: worth it, or `410 Gone` and let clients read chunks?
3. **Conversation stream default start:** live (proposed) or from event 0?
4. **Sub-agents:** can ACP updates be matched to `parent_tool_use_id` reliably? If not, ask `claude-agent-acp` to put it in `_meta` on the updates, and propose it upstream.
