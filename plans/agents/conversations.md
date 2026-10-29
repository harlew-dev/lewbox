# Conversations

> Status: Draft · Milestone: Alpha (M3) · Core (agent framework) · Updated: 2026-10-29

A conversation is one session with one coding agent, in one sandbox and one directory. A project's app starts it with a prompt, prompts it again as often as it likes, queues prompts or steers them into the running turn, cancels turns, and reads everything back, even after the sandbox is deleted. This plan covers the conversation and message resources and how prompts flow. Events are in [turns-and-events](turns-and-events.md), what's stored in [storage-and-uploads](storage-and-uploads.md).

## Today

- **Public contract:** [lewbox.com/docs/agents/conversations](https://lewbox.com/docs/agents/conversations), matching legacy behaviour except where noted.
- **Gateway:** `lewbox-gateway-legacy/internal/server/conversations.go` (`createConversation`, `createMessage`, `prompt`, `updateMessage`, `steerMessage`, `deleteConversation`, `listMessages`).
- **App:** `agent_conversations`, `agent_conversation_messages`, `agent_conversation_chunks`; the internal `POST sandboxes/{sbx}/conversations`, `POST conversations/{cnv}/messages`, `PATCH conversations/{cnv}/messages/{msg}` (legacy `docs/acp.md` § The app). The app decides whether a prompt may be queued (`queue_max` 20) under a lock on the conversation; agentd decides whether it runs, queues or is steered.
- **Built since 2026-10-21:** queue and steer (`053107e`), labels (`42f69bb`), instructions (`6d95391`), output schemas (`5bb00fe`), worktrees with names (`b84bbb0`, `92beca5`, `5b4392d`).

**Gaps Lewagent is waiting on** (`lewagent-legacy/docs/todo.md` § *Waiting on Lewbox*; `docs/agent-library.md` § *What Lewbox needs*; `docs/sessions.md` § *Which session is calling*; `docs/workflows.md` § *What Lewbox needs*):

- `env` on a conversation, so `lewagent-computer` knows which chat is calling (`LEWAGENT_SESSION`), and for an MCP relay's token.
- Skills, MCP servers and allowed tools per conversation.
- A prompt's source metadata (a workflow's name and run), returned with its history.
- A turn's status and final text by id, without following its stream.

## Goals

- Keep the public contract's fields, semantics and error codes.
- Add per-conversation environment, skills, MCP servers and tool permissions, without secrets touching the sandbox's disk.
- Make the control plane the one place that records a prompt and the turn after it, in one transaction, with the conversation locked.
- A cheap way to wait for a turn's answer.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Every field in the docs; `201` once the turn has started or been queued; idempotent prompts; queue (max 20) and steer; cancel; `instructions` fixed per conversation; `output_schema` per turn; content blocks; `label`; delete refusing a dirty worktree |
| **Change** | `sandbox` stays set after the sandbox is deleted (deleted sandboxes stay readable, [sandboxes/lifecycle](../sandboxes/lifecycle.md)); the conversation adds `sandbox_status` so a list can show which ones can still be prompted. Delete removes the worktree only when no other conversation works in it ([worktrees](worktrees.md)) |
| **Add** | `env`, `skills`, `mcp_servers`, `allowed_tools`, `disallowed_tools` on a conversation; `metadata` on a prompt; `reply_timeout` and `on_unanswered: wait` ([approvals-and-questions](approvals-and-questions.md)); `text` on a turn; `GET …/messages/{msg}?wait=`; `commands` on the conversation |

## Design

### Start a conversation

`POST /v0/sandboxes/{sbx}/conversations`, `sandboxes.agents`. The sandbox must be `running` (`409 sandbox_not_running`).

| Field | Type | Description |
|---|---|---|
| `agent` | string | Required. `claude-code` or `codex` (`422 agent_refused` otherwise) |
| `prompt` | string or array | Required. Text up to 100,000 characters, or up to 100 [content blocks](#content-blocks) |
| `directory` | string | Absolute path where the agent works. Default `/root/workspace`. Fixed for the conversation. The agent starts in it as it is: it must exist (`422 validation_failed` on `directory` when agentd finds it missing) |
| `worktree` | boolean or object | `true`, `{branch, from, copy, name}`, or `{id}` for a [standalone worktree](worktrees.md#standalone-worktrees). None by default |
| `instructions` | string | Up to 20,000 characters appended to the agent's system prompt. Fixed for the conversation ([Instructions](#instructions)) |
| `env` | object | **New.** Environment for the agent's process ([below](#environment-skills-mcp-servers-and-tools)). Write-only. Fixed |
| `skills` | array | **New.** Skills for this conversation only. Fixed |
| `mcp_servers` | array | **New.** MCP servers for this conversation only. Fixed |
| `allowed_tools`, `disallowed_tools` | array | **New.** Tools the agent may use without asking, or not at all. Fixed |
| `output_schema` | object | This turn's answer as JSON ([Structured output](#structured-output)) |
| `model`, `effort`, `mode`, `fast` | | [models-and-modes](models-and-modes.md). From this turn on |
| `on_unanswered` | string | `deny` (default), `allow` or `wait` (**new**). From this turn on ([approvals-and-questions](approvals-and-questions.md#when-nobody-answers)) |
| `reply_timeout` | integer | **New.** Seconds an approval or question waits, 60 to `agents.reply_timeout_max`. Default `agents.reply_timeout` (600). From this turn on |
| `label` | string | Names the prompt in the activity log (`agent.prompted`, `agent.turn_ended`), as a command's `label`; the `X-Lewbox-Label` header does the same, and the field wins |
| `metadata` | object | **New.** The prompt's own string pairs, up to 20, keys as sandbox metadata's (`letters, numbers, _ . : / -`), values up to 255 characters. Stored on the prompt and returned with it; Lewbox doesn't read it |

Send an `Idempotency-Key`. A retry with the same key and body answers `200` with the conversation the first request started and `Idempotent-Replayed: true`; with another body, `422 idempotency_key_reused` ([api/idempotency](../api/idempotency.md)).

It answers `201` with the [conversation](#the-conversation-object), and its first turn as `message`, once the turn has started. The first prompt in a sandbox can take up to ~30 s (agentd starting, an adapter installing); later ones a second or two. SDKs wait up to 2 minutes.

#### How it's recorded

```
BEGIN
  SELECT sandbox FOR SHARE                         -- running, the key reaches it
  idempotency check (profile, key) → existing? answer it
  INSERT agent_conversations (status idle, settings, instructions, env ciphertext …)
  INSERT agent_messages  prompt (role user, status queued, sequence null, metadata, label)
  INSERT agent_messages  turn   (role agent, status queued, sequence null, prompt_message_id)
  INSERT agent_chunks    prompt's content blocks (kind prompt, capped)
  INSERT activity        agent.prompted
COMMIT
command agentd: prompt { conversation, message: turn id, … }      ── agentd may take ~30 s first time
on result:
  started  → UPDATE turn status running, turn number; assign sequences (prompt n, turn n+1);
             conversation status running
  queued   → leave both queued (only on a later prompt)
  error    → UPDATE turn failed (stop_reason failed), conversation failed; answer the error
```

- The command goes to agentd **after** the commit, so a slow agentd never holds a lock. A crash between commit and command leaves a `queued` turn no agentd knows of; a sweeper re-sends `prompt` for turns `queued` for over a minute whose conversation has no running turn (agentd answers a known message as it is, so a re-send is harmless).
- A retried request (same `Idempotency-Key`) whose turn is still `queued` re-sends the command; one whose turn has started answers it.
- `agent_refused` and `agent_unavailable` from agentd fail the turn and are returned as the request's error.

### Prompt again

`POST /v0/conversations/{cnv}/messages`, `sandboxes.agents`. The conversation's next turn, in its own sandbox, which must be running.

Takes `prompt`, `output_schema`, `model`, `effort`, `mode`, `fast`, `on_unanswered`, `reply_timeout`, `label`, `metadata`, `queue` and `steer`. `agent`, `directory`, `worktree`, `instructions`, `env`, `skills`, `mcp_servers`, `allowed_tools` and `disallowed_tools` can't change: giving any is `422 validation_failed` naming it. Settings apply from this turn on; leave them out to keep the conversation's.

It answers `201` with the new turn as a [message](#messages): `running`, `queued`, or `running` as a steered turn.

### Queue and steer

While a turn runs or waits, a new prompt doesn't have to wait for the client:

| Field | Default | |
|---|---|---|
| `queue` | `true` | Hold the prompt until the turns before it end. With `false`, refused `409 turn_running` |
| `steer` | `false` | Send the prompt into the running turn now. If the agent can't take it, it's queued, or with `queue: false` refused `409 not_steerable` |

- **Queued:** the turn is `queued` with `sequence` `null`; neither it nor its prompt gets a place in the conversation until it starts, so the conversation reads in the order things happened. Its events can be followed at once; the stream waits ([turns-and-events](turns-and-events.md#following)). Up to `agents.queue_max` (20) wait; another is `409 queue_full`. The queue lives in agentd (`conversation.json`, each prompt with its settings), so it drains with no one following; the control plane mirrors it in `agent_messages`.
- **Steered:** sent through the adapters' `_session/steering` extension. The running turn ends `completed` with stop reason `steered`, `next_turn` and `next_message`; the steered prompt's turn carries on from there. Claude is asked for `idleBehavior: "promptRequired"` so a turn that has just ended starts the prompt as a turn of its own; Codex answers `injected`, `startedNewTurn`, or `activeTurnNotSteerable` (legacy `docs/acp.md` § Queue and steer).
- **Not steerable:** a turn waiting on an approval or question (the agent would only read the prompt after the answer, and the request would belong to the turn before the split); a prompt with an `output_schema` (it needs a turn of its own). A model or mode on a steered prompt applies from the next turn.
- **Queued prompts:** `GET /v0/conversations/{cnv}/messages?status=queued` lists each queued prompt with its turn, in the order they'll run. `PATCH …/messages/{turn}` with `steer: true` steers one into the running turn (`200`; `409 not_queued`, `409 not_steerable`); with `status: cancelled` takes it out of the queue (`200`): neither it nor its prompt ever gets a place.

### Cancel a turn

`PATCH /v0/conversations/{cnv}/messages/{msg}` with `status: cancelled`. For a running or waiting turn it answers `202` with the turn as it is; agentd sends `session/cancel`, open approvals and questions are answered `by: cancel`, and the turn ends with `finished`, `stop_reason` `cancelled`. What the agent did before is kept, and it sees it on the next prompt. A turn that has ended is `409 turn_running`, whose message says how it ended (legacy's code, kept for compatibility of clients' handling). Cancelling leaves the queue alone; take queued prompts out first to stop everything.

Leaving a stream never cancels a turn ([turns-and-events](turns-and-events.md#leaving-doesnt-stop-the-turn)).

### Instructions

Kept as built on 2026-10-26 (legacy `docs/acp.md` § Instructions, checked live with both adapters):

- Appended to the agent's own system prompt, never replacing it; `CLAUDE.md` and `AGENTS.md` files still load. Claude takes them as ACP's `systemPrompt` (`{mode: "append"}`, which `claude-agent-acp` 0.94.0 turns into the Agent SDK's `{type: "preset", preset: "claude_code", append}`), on `session/new`, `load` and `resume` alike, as Claude keeps the system prompt it started with. Codex takes them as `developer_instructions` in `CODEX_CONFIG`, merged into each thread's config, new or resumed, without touching `~/.codex/config.toml` (one caveat: a `developer_instructions` in `config.toml` is replaced for that conversation, not added to).
- Fixed for the conversation; a later prompt giving them is `422`. The control plane keeps them and sends them with every prompt, and agentd prefers that copy to `conversation.json`.
- An adapter that can't take them fails the turn rather than running without them.
- Lewagent's rules (workspace, repositories, projects, presets) come to more than 20,000 characters for some chats; the limit stays, and is a setting (`agents.instructions_max_chars`).

### Structured output

Kept as built on 2026-10-26 (legacy `docs/acp.md` § Output schemas, measured live):

- `output_schema` is a JSON Schema with `type: object`, up to 64 KB, for this turn only. The agent works as usual (every event streams), then answers in JSON matching it; `finished` carries it as `output`, and the turn's message keeps `output_schema` and `output`.
- **Claude:** the Agent SDK's `outputFormat` via `_meta.claudeCode.options` on the session. It ends the turn with a `StructuredOutput` tool call it checks and retries; any JSON Schema works. It's session-wide, so a turn with a different schema (or none after one) reopens Claude's process (~1 s), which stops its background tasks and rewrites its whole prompt cache: measured on Opus, $0.363 for a turn that switched against $0.013 for one that didn't.
- **Codex:** `outputSchema` on `turn/start`, per turn, through the [Codex stand-in](agentd.md#the-codex-stand-in). Strict schemas only (`additionalProperties: false` in every object, every property `required`); others are refused by Codex as the turn's text, and the turn ends with `output: null` and an `error` event.
- A prompt with a schema can be queued but not steered (`not_steerable`); one steered into a turn with a schema carries on with it.
- **Docs:** say plainly that alternating schemas on Claude costs a first turn's price each time, and suggest a separate conversation for schema'd steps (as Lewagent's workflows plan does: "ask aside").
- **Upstream:** propose to `codex-acp` that `session/prompt` take an output schema, and drop the stand-in once it does.

### Content blocks

A prompt is text, or up to 100 [ACP content blocks](https://agentclientprotocol.com/protocol/content), each with a `type`:

| Type | Fields | Notes |
|---|---|---|
| `text` | `text` | A text prompt is sent as one |
| `image` | `data` (base64), `mimeType`, `uri`? | PNG, JPEG, GIF, WebP; up to 5 MB each |
| `resource_link` | `uri`, `name`, `mimeType`? | Points the agent at a file, e.g. `file:///root/workspace/app/Invoice.php` |
| `resource` | `resource: {uri, text \| blob, mimeType}` | Embedded content |

What an agent makes of a block other than `text` is its own. The request body is capped at 20 MB. The stored `prompt` chunk keeps text and fields whole and cuts only images' and files' base64 ([storage-and-uploads § Chunks](storage-and-uploads.md#chunks)).

### Environment, skills, MCP servers and tools

All new, all given on the first prompt and fixed for the conversation, as Lewagent's composer treats them ("read-only once the chat starts", `docs/agent-library.md` § Interface). Changing one would need the adapter reopened, which costs what a schema change does on Claude; a new conversation is the honest way.

**`env`:** up to 50 variables, names `^[A-Za-z_][A-Za-z0-9_]{0,127}$`, string values up to 32 KB, 256 KB in total. Names Lewbox sets are refused (`IS_SANDBOX`, `CLAUDE_CODE_EXECUTABLE`, `CODEX_PATH`, `CODEX_HOME`, `CODEX_CONFIG`, `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_*`, `OPENAI_API_KEY`, `OTEL_*`, `LEWBOX_*`): logins and telemetry come from the profile. Added last to the adapter's environment ([agentd § The adapter's environment](agentd.md#the-adapters-environment)), so the agent and its tools see them. Write-only: stored encrypted, never returned (the conversation shows `env_names`), sent to agentd with each prompt, held only in memory there.

**`skills`:** up to 20, each `{name, files}`: `name` as a template id (lowercase words joined by hyphens), `files` an object of relative paths to text, which must include `SKILL.md`; 1 MB in total. agentd writes them to `/root/.lewbox/agents/<cnv>/plugin/` as a plugin (`.claude-plugin/plugin.json` and `skills/<name>/…`):

- **Claude:** `plugins: [{type: "local", path}]` in `_meta.claudeCode.options` on `session/new`, `load` and `resume`, as `outputFormat` is.
- **Codex:** to be found in a spike. Candidates (Lewagent's `docs/agent-library.md`): `[[skills.config]] path, enabled` in `CODEX_CONFIG`, if it reaches skills outside `~/.codex/skills` and `.codex/skills`; or a `CODEX_HOME` per conversation, which moves `auth.json` and threads and is likely too costly. Until then, skills on a Codex conversation are `422 validation_failed`, "Codex doesn't take skills through Lewbox yet."

**`mcp_servers`:** up to 20, each `{name, type: "stdio", command, args, env}` or `{name, type: "http", url, headers}`. Passed as ACP's `mcpServers` on `session/new`, `load` and `resume`; `http` ones only to an adapter that advertises `mcpCapabilities.http` (`422` otherwise). `env` values and `headers` are secrets: stored encrypted and handled as `env` is, never in `conversation.json`. A stdio server runs in the adapter's process group, so it stops when the adapter unloads.

**`allowed_tools`, `disallowed_tools`:** up to 200 patterns each, in Claude's syntax (`Bash(git status:*)`, `mcp__playwright__browser_snapshot`, `mcp__linear__*`), passed as `allowedTools` and `disallowedTools` in `_meta.claudeCode.options`. A tool in `allowed_tools` runs without an approval in any mode; one in `disallowed_tools` is never offered. Codex: if the spike finds an equivalent in its config, mapped; otherwise `422`.

### List and get

- `GET /v0/conversations`: newest first, cursor pages of up to 100; filters `sandbox`, `agent`, `status`, and **new** `worktree` (a `wtr_` id). A sandbox key sees only its sandbox's. No permission needed (as with sandboxes).
- `GET /v0/conversations/{cnv}`: no permission needed.
- The Admin API lists every profile's in the project, filtered by `profile` too, with `started_by`.

### Messages

- `GET /v0/conversations/{cnv}/messages`: oldest first, each prompt then its turn, with their [chunks](storage-and-uploads.md#chunks); `after` (a sequence) and `per_page` (up to 100); `status=queued` for the queue. Needs `sandboxes.agents`.
- `GET /v0/conversations/{cnv}/messages/{msg}` **new**: one message, with chunks unless `?chunks=false`. Takes `?wait=<seconds>` (up to 60): for a turn that's `queued`, `running` or `waiting`, the request holds until it ends or the time passes, then answers with the turn as it is. Served from Postgres with `LISTEN` on the conversation, so it's cheap. This is Lewagent's "turn status without the stream" for its workflow engine.

### Delete a conversation

`DELETE /v0/conversations/{cnv}`, `sandboxes.agents`. `204`.

1. Mid-turn (`running` or `waiting`): `409 turn_running`. Cancel first.
2. Its worktree, if it has one and no other conversation works in it, is removed through agentd, keeping its branch: `409 worktree_dirty` with uncommitted changes (other than paths it copied in) unless `?force=true`. `?keep_worktree=true` leaves it. In a stopped sandbox the worktree is left, its record too, for `DELETE /v0/worktrees/{wtr}` once the sandbox runs ([worktrees](worktrees.md#removing)).
3. agentd's copy (`/root/.lewbox/agents/<cnv>/`) is removed if the sandbox runs, else left for agentd to clean up on its next start (it removes directories of conversations the control plane says are gone in `welcome`).
4. Rows are deleted: the conversation, its messages, chunks and events. Activity events stay (they never held prompt text).

The agent's own transcript stays in the sandbox until the sandbox is deleted. A deleted conversation is `404`.

### The conversation object

| Field | Type | |
|---|---|---|
| `id` | string | `cnv_…` |
| `sandbox` | string | Its sandbox's id, kept after the sandbox is deleted |
| `sandbox_status` | string | **New.** The sandbox's status, so a list can tell which can be prompted |
| `agent` | string | |
| `directory` | string | In its worktree, when it has one |
| `worktree` | object or null | `id` (**new**), `path`, `branch`, `from`, `repository` |
| `instructions` | string or null | |
| `env_names` | array | **New.** Names only |
| `skills` | array | **New.** Names |
| `mcp_servers` | array | **New.** `name` and `type` only |
| `allowed_tools`, `disallowed_tools` | array | **New** |
| `model`, `effort`, `fast` | | As last set, `null` for the default |
| `mode` | string | Now; can change mid-turn |
| `on_unanswered` | string | `deny`, `allow` or `wait` |
| `reply_timeout` | integer | **New.** Seconds |
| `title` | string or null | From the agent, shortly after its first turn (Claude's can arrive after `finished`) |
| `status` | string | `idle`, `running`, `waiting`, `failed` |
| `usage` | object or null | Totals over ended turns |
| `context` | object or null | `tokens`, `size`, `at`, from the latest `usage` event |
| `rate_limit` | object or null | Claude subscription windows, as last reported |
| `commands` | array | **New.** The agent's slash commands, `name` and `description`, as last reported ([models-and-modes § Slash commands](models-and-modes.md#slash-commands)) |
| `created_at`, `last_message_at` | string | |
| `message` | object | Only on create: its first turn |

### Statuses

| Status | Means |
|---|---|
| `idle` | No turn running; ready for a prompt |
| `running` | A turn is running (including one the agent started itself) |
| `waiting` | A turn waits on an approval or question |
| `failed` | Its last turn failed, as when the agent couldn't start. A prompt tries again |

### Messages

A prompt (`role: user`) then the turn after it (`role: agent`). A turn the agent starts itself has no prompt before it.

| Field | | |
|---|---|---|
| `id` | string | `msg_…` |
| `sequence` | integer or null | From 1; `null` while queued, and for good once taken out of the queue |
| `role` | string | `user`, `agent` |
| `origin` | string or null | A turn's: `prompt`, `agent`, or `peer` (it replied to another agent session) |
| `sender` | string or null | For `peer` |
| `status` | string | A turn's: `queued`, `running`, `waiting`, `completed`, `cancelled`, `failed`, `interrupted`. A prompt's: `queued`, then `completed`, or `cancelled` |
| `stop_reason` | string or null | [turns-and-events § Stop reasons](turns-and-events.md#stop-reasons) |
| `model`, `mode` | string or null | What a turn ran with |
| `usage` | object or null | `input_tokens`, `output_tokens`, `cached_read_tokens`, `cached_write_tokens`, `reasoning_tokens`, `total_tokens`, `cost`, `currency`, as reported |
| `output_schema`, `output` | object or null | |
| `text` | string or null | **New.** A turn's answer: its last run of text after its last tool call, as a chat shows it. `null` until it ends, or for a turn with no text |
| `label` | string or null | A prompt's |
| `metadata` | object | **New.** A prompt's; `{}` otherwise |
| `started_at`, `finished_at` | string or null | |
| `chunks` | array | When listed, or on a single get |

## Errors

| Status | Code | When |
|---|---|---|
| `403` | `permission_denied` | No `sandboxes.agents` |
| `404` | `not_found` | No such sandbox, conversation or message the key sees, or a deleted one |
| `409` | `sandbox_not_running` | The sandbox isn't running, or is deleted |
| `409` | `turn_running` | A prompt with `queue: false` while a turn runs; a delete mid-turn; a cancel of an ended turn |
| `409` | `queue_full` | 20 prompts queued |
| `409` | `not_steerable` | Waiting on a request, or the prompt has a schema |
| `409` | `not_queued` | Steering a prompt that isn't queued |
| `409` | `worktree_failed`, `worktree_dirty`, `worktree_removed` | [worktrees](worktrees.md#errors) |
| `422` | `validation_failed` | A field: an unknown mode, `fast` on Claude, instructions over the limit, a bad schema, a fixed field on a later prompt, a reserved env name, skills on Codex |
| `422` | `not_a_repository` | `worktree` outside a git repository |
| `422` | `agent_refused` | agentd refused the request; the message is its |
| `422` | `idempotency_key_reused` | |
| `502` | `agent_unavailable` | agentd couldn't be started or reached. Retry |
| `503` | `agents_unavailable` | Agents are off on this Lewbox |

A prompt can also fail after it's accepted (a model the agent refuses): the turn ends `failed` with an `error` event, and the conversation is `failed` until the next prompt.

## Security

- `env`, MCP `env` and `headers` are encrypted with the keyring, decrypted only to build the `prompt` command, and never logged. Request logging redacts them.
- Prompt text, instructions and skills are stored as given, unencrypted ([storage-and-uploads § Encryption](storage-and-uploads.md#encryption-later)).
- A sandbox key with `sandboxes.agents` can do all of this for its own sandbox, which is how browsers chat ([api/authentication](../api/authentication.md)).

## Observability

- Activity `agent.prompted` (actor, `label`, conversation, turn; never the text) and `agent.turn_ended` (stop reason, usage, `label`).
- Metrics: prompts by agent and outcome (started, queued, steered, refused), time from request to turn started, sweeper re-sends.

## Testing

- Service tests against Postgres and a fake agentd on the channel: first prompt, retry with the same key at each stage (before commit, after commit before command, after started), queue to 20 and the 21st, steer each way, cancel running and queued, delete with clean, dirty, forced, kept and shared worktrees, fixed fields refused on later prompts, reserved env names.
- Encryption: `env` never in any response, log line or agentd file (scan the fake sandbox's files after a test).
- API tests from the OpenAPI document ([api/openapi-and-codegen](../api/openapi-and-codegen.md)).
- Live: Lewagent's chat flows on a staging Lewbox.

## Build plan

1. Tables and the agents service: create, prompt, queue, steer, cancel, delete, list, get (4 days).
2. `env`, `skills`, `mcp_servers`, tools, through to agentd (2 days); the Codex skills and tools spike (1 day).
3. `metadata`, `text`, `?wait=` (1 day).
4. The sweeper for un-sent prompts (½ day).

## Open questions

1. **Fixed or changeable `env`, skills and MCP servers?** Fixed is proposed. Lewagent's relay token per chat could need rotating; if so, a `PATCH /v0/conversations/{cnv}` that reopens the adapter at the next turn.
2. **What `text` means for Codex,** whose commentary comes as separate messages before its answer: the last message's text (proposed), or all text after the last tool call?
3. **Skills and tools on Codex:** the spike's outcome decides whether they ship for Codex in the alpha.
4. **Prompt `metadata` filters:** list messages by `metadata[key]`? Not proposed until someone needs it.
5. **`turn_running` for cancelling an ended turn** is odd; keep it for compatibility, or use `invalid_state`? We're free to change ([D4](../README.md#decisions)); proposed `invalid_state`, with the message saying how the turn ended.
