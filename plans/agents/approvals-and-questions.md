# Approvals and questions

> Status: Draft · Milestone: Alpha (M3) · Core (agent framework) · Updated: 2026-10-29

An agent can stop mid-turn and wait for the client: an **approval** before a tool call its mode doesn't let it make alone (write a file, run a command, carry out a plan), or a **question** when it needs a choice (which database to use). Each arrives as an event with a `request` id; the turn waits until a reply, a decline, a cancel, or a timeout. Any process with `sandboxes.agents` can reply, so answers can come from a button in a UI, a rule in a backend, or a person on a terminal.

## Today

- **Contract:** [lewbox.com/docs/agents/approvals-and-questions](https://lewbox.com/docs/agents/approvals-and-questions).
- **agentd** (`internal/agentd/conversation.go`) turns ACP's `session/request_permission` into `approval_request` and `elicitation/create` (form) into `question`, waits for `reply` until `LEWBOX_AGENTD_REPLY_TIMEOUT` (10 min, from the app's `lewbox.agents.reply_timeout_seconds`), then applies the conversation's `on_unanswered` (`deny` by default, or `allow`).
- **The gateway:** `POST /v1/conversations/{cnv}/replies` → `lewbox-agentd call reply` (`createReply`, `internal/server/conversations.go:847`).
- **Stored** as chunks of kind `approval_request` and `question`, `external_id` the request id, status `pending`, `answered` or `expired`; the dashboard's sidebar and the PHP SDK's `pending()` read them.
- **Checked live** with both adapters (legacy `docs/acp.md` § Spike findings): Claude's options, `AskUserQuestion` forms, `ExitPlanMode`; Codex's `requestUserInput`, plan approval and (outside gVisor) command approvals.

**Lewagent's asks** (`lewagent-legacy/docs/todo.md`; `docs/workflows.md` § Approvals):

- **Hold an approval until it's answered** (`on_unanswered: wait`, or an expiry the prompt sets), for a workflow run waiting on a person for hours.
- **A computer-wide signal** that something is pending (`small-todo.md`), today a dot on a tab from reading each chat.

## Goals

- Keep the request, option and answer model, which works for both agents.
- Let a prompt choose how long requests wait, up to a project-set maximum, and choose `wait`.
- Find what's pending across a conversation or a sandbox in one request, and hear about it by webhook.
- Record who answered.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Events and fields; replying by option `id` or `kind`; `decline`; `answered` with `by`; one `answered` per request; plan mode's options; `on_unanswered` `deny` and `allow`; 10-minute default; questions cancelled on timeout; `404` for a request already answered |
| **Change** | Requests are recorded by the control plane as they arrive, not a second later from an upload, so `status=waiting` lists are exact |
| **Add** | `on_unanswered: wait`; `reply_timeout` per prompt; `GET /v0/conversations/{cnv}/requests` and `GET /v0/sandboxes/{sbx}/requests`; `answered.actor`; activity and webhook `agent.waiting`; `Idempotency-Key` on replies |

## Design

### Approvals

Which tool calls ask depends on the conversation's [mode](models-and-modes.md#modes): in Claude's `default`, writing a file asks and a read-only command such as `cat` doesn't; in `accept_edits`, edits go ahead and commands ask; in `bypass`, nothing asks. A tool in the conversation's `allowed_tools` never asks ([conversations](conversations.md#environment-skills-mcp-servers-and-tools)). Codex runs in `bypass` only for now, so its only approval is a plan's ([Plan mode](#plan-mode)).

An `approval_request` comes just after the `tool_call` it's for (same `call_id`), which carries what the agent wants to do.

| Field | | |
|---|---|---|
| `request` | string | `req_…`, the id to reply to |
| `call_id` | string | The tool call |
| `title` | string | For people: `Write tests/BillingTest.php`, `Run npm test` |
| `kind` | string | The tool call's kind |
| `options` | array | `{id, name, kind}`; `kind` is `allow_once`, `allow_always`, `reject_once` or `reject_always` |
| `expires_at` | string or null | When `on_unanswered` applies; `null` with `wait` and no timeout |

Option ids and names are the agent's own: Claude's are usually `allow-once` ("Yes"), `allow-with-updates` ("Yes, allow all edits in tests/ during this session", `allow_always`) and `reject` ("No"); Codex's include `implement_plan` and `revise_plan`, and under gVisor-free sandboxes command approvals with `allow_once`, `accept_execpolicy_amendment` (`allow_always`) and `cancel`. An approval needn't offer every kind.

### Questions

A question is a small form described by JSON Schema (a flat object of strings, numbers, booleans and choices). Questions work in every mode, `bypass` included.

| Field | | |
|---|---|---|
| `request` | string | `req_…` |
| `text` | string | The question |
| `schema` | object | Fields under `properties`, each with `type` and `title`; `oneOf` for a choice (`const`, `title`, `description`) |
| `call_id` | string or null | The tool call that asked |
| `expires_at` | string or null | |

**Each agent's forms** (show them as they come):

- **Claude** (`AskUserQuestion`): per question, `question_<n>`, a string whose `oneOf` lists the choices, and `question_<n>_custom`, free text. Answer one of the two.
- **Codex** (`requestUserInput`): a field whose `oneOf` includes "None of the above", and `<field>_note` for free text; a field can carry `_meta.codex.isOther` or `isSecret` (hide as typed).

URL elicitations (an agent asking the user to visit a page) aren't shown: the agent is told no, and an `error` event with `url` says so.

### Replying

`POST /v0/conversations/{cnv}/replies`, `sandboxes.agents`, `204`:

```json
{ "request": "req_…", "option": "allow_once" }
{ "request": "req_…", "answers": { "question_1": "postgres" } }
{ "request": "req_…", "decline": true }
```

- Exactly one of `option`, `answers`, `decline` (`422 validation_failed` otherwise).
- **`option`** by its `id`, or by its `kind`: `allow_once` picks the option of that kind, so the same code works for every agent. An option the approval doesn't have is `422 agent_refused`, listing those it has.
- **`answers`** keyed by the schema's fields; a choice by its `const`. Passed to the agent as sent, unchecked against the schema. `answers` to an approval or `option` to a question is `422 agent_refused`.
- **`decline`:** an approval gets its `reject_once` option, else `reject_always`; with neither, the call is cancelled. A question is declined: the agent carries on without an answer and usually says what it assumed.
- **Idempotent:** send an `Idempotency-Key` (SDKs do), and a retry of a reply that landed answers `204` with `Idempotent-Replayed: true`, not `404`.
- **Path:** the control plane checks the request is `pending` in its own record (under a row lock on the chunk), sends `reply` to agentd ([agentd § Commands](agentd.md#commands)), and on success marks it answered with `actor` (the key's `key_…` or the user's `usr_…`). agentd's `answered` event follows on the stream.

Replying doesn't interrupt a stream being read; the agent's work after the approval follows on it.

### The answered event

| Field | | |
|---|---|---|
| `request` | string | |
| `by` | string | `client`, `timeout`, `cancel` (the turn was cancelled) or `turn_end` (the turn ended first, as when the sandbox stopped) |
| `actor` | object or null | **New.** For `client`: `{type, id, name}` as in the activity log |
| `option` | string or null | An approval's option id; `null` when there was none and the call was cancelled |
| `answers` | object or null | A question's answers |
| `declined` | boolean | |

Every request gets exactly one `answered`, however it ends. Once answered it can't be replied to (`404`).

### When nobody answers

A request waits `reply_timeout` seconds (default 600, set per prompt from 60 to `agents.reply_timeout_max`, 24 h), until `expires_at`. Meanwhile the turn and conversation are `waiting`, and the sandbox counts as active, so its idle timeout doesn't stop it ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md)). Then `on_unanswered` answers, `by: timeout`:

| `on_unanswered` | An approval gets | A question gets |
|---|---|---|
| `deny` (default) | `reject_once`, else `reject_always` | Cancelled |
| `allow` | `allow_once`, else `allow_always` | Cancelled |
| `wait` (**new**) | Nothing: it waits up to `agents.reply_timeout_max`, then is denied | Waits likewise, then is cancelled |

- With `allow`, an agent left alone carries on as if in `bypass`, ten minutes slower each time: only for work you'd let run unattended.
- With `wait`, `expires_at` is the maximum; the docs say plainly that a waiting sandbox keeps running, and costs what running costs.
- The timer runs in agentd, which has the request; the control plane mirrors `expires_at` and, if agentd is gone (the sandbox stopped), the turn is ended `interrupted` and requests answered `turn_end` ([turns-and-events § Interrupted turns](turns-and-events.md#interrupted-turns)).

### Plan mode

In `plan` mode the agent explores and writes a plan without changing anything, then asks to carry it out. That's an approval like any other.

**Claude** asks "Approve Plan" on a tool call of kind `switch_mode` that carries the plan (written to `~/.claude/plans/<slug>.md`). Its options pick the mode to carry on in:

| Option id | Kind | Name | Carries on in |
|---|---|---|---|
| `exit-plan-clear-auto` | `allow_always` | Yes, clear context (12% used) and use auto mode | `auto`, planning cleared from context |
| `exit-plan-auto` | `allow_always` | Yes, and use auto mode | `auto` |
| `exit-plan-bypass` | `allow_always` | Yes, and bypass permissions | `bypass` |
| `exit-plan-default` | `allow_once` | Yes, manually approve edits | `default` |
| `reject` | `reject_once` | No, keep planning | `plan` |

A conversation that was in `bypass` before planning gets the bypass options first, and `exit-plan-clear-bypass` in place of `exit-plan-clear-auto`. Replying `allow_once` picks `exit-plan-default`, the safe choice by kind. Once Claude leaves plan mode a `mode` event follows and the conversation's `mode` changes, so the next turn starts there.

**Codex** asks "Implement this plan?" with `implement_plan` (`allow_once`) and `revise_plan` (`reject_once`).

### What's pending

- **From the stream:** follow the turn from its start or before the request; requests without an `answered` after them are pending.
- **From the control plane (new):** `GET /v0/conversations/{cnv}/requests?status=pending` and `GET /v0/sandboxes/{sbx}/requests?status=pending` list requests (`request`, `conversation`, `message`, `type` `approval` or `question`, the event's fields, `status`, `expires_at`, `created_at`, and once answered `answer`), oldest first. They're recorded the moment the event arrives, so there's no second's lag as with legacy's chunks. `sandboxes.agents`.
- **By webhook (new):** `activity.agent.waiting` when a request opens (conversation, turn, request id, type, title, `expires_at`; not the tool input), so an app can notify someone without polling. Paired with `activity.agent.turn_ended`.
- **Lists:** `GET /v0/conversations?status=waiting`.

An agent can ask several at once (tool calls made together): answer each, in any order.

### Data

Requests are stored as chunks (`kind` `approval_request` or `question`, `external_id` the request id, `status` `pending`, `answered` or `expired`, `content` the event's fields plus `answer`) ([storage-and-uploads § Chunks](storage-and-uploads.md#chunks)), with a partial index on `(conversation_id) WHERE status = 'pending' AND kind IN ('approval_request','question')` for the lists. On the channel path the control plane writes the chunk as it stores the event, rather than waiting for agentd's merged upload; the upload's version replaces it, keeping `actor`.

## Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.agents` |
| `404` | `not_found` | No such conversation, or nothing waiting under that `request` (answered, expired, or its turn ended) |
| `409` | `sandbox_not_running` | The sandbox isn't running |
| `422` | `validation_failed` | No `request`, or not exactly one of `option`, `answers`, `decline`; a `reply_timeout` out of range |
| `422` | `agent_refused` | An option the approval doesn't have; `answers` to an approval; `option` to a question |
| `502` | `agent_unavailable` | agentd couldn't be reached. Retry the same reply (with its `Idempotency-Key`) |

A `404` for a request seen a moment ago usually means another process, a timeout or the turn's end answered it; read its `answered` event or its chunk.

## Security

- Replies are commands to an agent that runs as root in the sandbox: an `allow` is a decision to let it act. The activity log records each reply's actor (`agent.replied`, with request id, option kind, never answers' text).
- `allow` and `wait` are per conversation; a project may cap `reply_timeout_max` lower, or forbid `allow`, per profile (open question).

## Observability

- Activity: `agent.waiting` (a request opened), `agent.replied` (by whom, which kind), and timeouts as `agent.replied` with `by: timeout`.
- Metrics: requests opened by agent and kind, time to answer, answered by (`client`, `timeout`, `cancel`, `turn_end`), `allow` by timeout.

## Configuration

| Setting | Default | |
|---|---|---|
| `agents.reply_timeout` | `10m` | Default per request |
| `agents.reply_timeout_max` | `24h` | Longest a prompt may ask for; what `wait` waits |

## Testing

- With the fake adapter: approvals with each option kind, by id and by kind; questions in both agents' forms; decline each; cancel with requests open; timeout with `deny`, `allow` and `wait`; a sandbox stop while waiting; two requests at once answered out of order; a retried reply with an `Idempotency-Key`.
- Plan mode: Claude's options by kind and id, and the `mode` event after; Codex's two options.
- Live: both agents, as in legacy's spike.

## Build plan

1. Replies, recording requests on arrival, `answered.actor` (2 days).
2. `reply_timeout`, `wait` (1 day; agentd already has the timer).
3. Request lists and `agent.waiting` (1 day).

## Open questions

1. **`wait` and cost.** A sandbox waiting a day on a person runs a day. Could Lewbox park it: answer the approval with a reject that says "paused for a person", stop the sandbox, and on the reply start it and prompt "Approved: carry on with …"? It changes the agent's view of what happened, so it's a Later idea, not the alpha.
2. **Per-profile policy:** should a project be able to forbid `on_unanswered: allow`, or cap `reply_timeout` per profile?
3. **Answers' privacy:** `answers` can hold secrets (Codex's `isSecret` fields). Store them in the chunk as now, or redact fields marked secret?
