# Agents

> Status: Draft · Milestone: Alpha (M3), parts Beta and Later · Core framework; agents are plugins (`agents/claude-code`, `agents/codex`, `agents/opencode` planned) · Updated: 2026-10-29

Lewbox runs coding agents (Claude Code and Codex) inside a profile's sandboxes, signed in with the profile's own logins. A project's app sends a prompt; the agent works in the sandbox with its own tools; the app follows what it does as a stream of events, answers its approvals and questions, and reads the whole conversation back later, even after the sandbox is gone. This plan is the overview; the parts below go into each piece.

This is the area of Lewbox that changed fastest in legacy (from first commit to queue, steer, instructions, output schemas, background tasks and worktrees in eight days, 2026-10-21 to 2026-10-29), and the one Lewagent leans on hardest: its chats, workflows, pull request drafts and "commit with AI" are all conversations.

## Contents

- [Today](#today)
- [Goals](#goals)
- [Keep, change, drop](#keep-change-drop)
- [How it fits together](#how-it-fits-together)
- [Core and agent plugins](#core-and-agent-plugins)
- [Concepts](#concepts)
- [API at a glance](#api-at-a-glance)
- [Security](#security)
- [Observability](#observability)
- [Configuration](#configuration)
- [Build plan](#build-plan)
- [Index](#index)
- [Later](#later)
- [Open questions](#open-questions)

---

## Today

Agents in legacy are split three ways:

| Piece | Where | What it does |
|---|---|---|
| `lewbox-agentd` | `lewbox-gateway-legacy/cmd/lewbox-agentd`, `internal/agentd/*` (~6k lines, ~3k of tests) | The ACP client inside the sandbox. Starts `claude-agent-acp` and `codex-acp`, numbers events into `events.jsonl`, merges them into chunks and uploads them, holds the queue, waits for replies, stops background tasks, makes worktrees, probes models |
| The gateway | `lewbox-gateway-legacy/internal/agents/agents.go`, `internal/server/conversations.go` (1,501 lines), `internal/server/tasks.go` | Uploads `lewbox-agentd` into the sandbox and starts it as a toolbox session; sends it commands by running `lewbox-agentd call --file` through the toolbox; serves a turn's events as SSE by running `lewbox-agentd events --follow` over the toolbox's exec-stream WebSocket; receives uploads at `POST /v1/agent-uploads` and forwards them to the app |
| The app | `lewbox-legacy` (`agent_conversations`, `agent_conversation_messages`, `agent_conversation_chunks`; `app/Http/Controllers/Api/Gateway/v1/*`) | Stores conversations, turns and chunks; decides queueing and sequence numbers under a lock; serves the Admin API's read-only views; records `agent.prompted` and `agent.turn_ended` activity |

The design record is `lewbox-legacy/docs/acp.md` (the investigation, spike findings, the decisions and the encryption design); the public contract is [lewbox.com/docs/agents](https://lewbox.com/docs/agents/overview) (six pages). Both are sources for these plans and are quoted where they matter.

**What's wrong with it**, beyond the split itself ([README](README.md#goals)):

- **Every command is a process.** A reply, a cancel or a status check writes a JSON file into the sandbox and runs `lewbox-agentd call --file` through the toolbox's Execute (`internal/agents/agents.go`, up to 120 s): 100–300 ms of overhead per call, and under gVisor every one of them is exposed to the toolbox's lost-exit-status bug ([lessons](lessons.md)).
- **Every follower is a stream into the sandbox.** Two browser tabs following one turn are two `events --follow` processes in the sandbox, two exec-stream WebSockets, and the stream only works while the sandbox runs.
- **Uploads hop three times** (agentd → gateway → app's internal API → database), with token checks, batching (`chunkBatchSize`) and "ignorable" refusals at each hop (`receiveUpload`, `conversations.go:1224`).
- **The app learns a turn ended only from the next upload**, so `finished` had to be held back until it landed (`56a4a76`), and a turn the agent starts itself has no stream that announces it ([building-a-chat-page § Known gaps](https://lewbox.com/docs/agents/building-a-chat-page#known-gaps)).
- **Gaps Lewagent is waiting on** (`lewagent-legacy/docs/todo.md` § *Waiting on Lewbox*): per-conversation `env`, skills, MCP servers and allowed tools; approvals held until answered (`on_unanswered: wait`); idle chats unloaded (17 chats held ~2.5 GB of a 12 GB computer); Codex's `~/.codex` SQLite race when two chats start at once; slash commands kept on the conversation; standalone worktrees; a message's source metadata; a turn's status and final text without the stream; sub-agents' output kept apart.

## Goals

1. **Keep the contract that works.** The conversation, message, chunk and event shapes in [lewbox.com/docs/agents](https://lewbox.com/docs/agents/overview) were refined against two real consumers (the dashboard and Lewagent). We keep their semantics, field names and error codes, and change only what [D4](README.md#decisions) lets us fix.
2. **Port `lewbox-agentd` rather than rewrite it.** It's Go, it's tested against a scripted fake adapter, and it embodies a dozen hard-won findings about both adapters. It becomes `lewbox agentd` in the [sandbox binary](agents/agentd.md#the-binary).
3. **One write path for what's stored.** agentd's uploads go straight into Postgres in one transaction, so the control plane knows a turn ended the moment agentd says so.
4. **Close Lewagent's gaps** listed above, each in the plan it belongs to.
5. **Streams that survive a replica.** A follower can be on any replica, reconnect to another, and replay a finished turn even when its sandbox is stopped ([turns-and-events](agents/turns-and-events.md#where-events-come-from)).
6. **Room for more agents.** ACP already covers OpenCode, Gemini and pi; adding one is an **agent provider plugin** ([plugins/agent-providers](plugins/agent-providers.md)), often little more than a manifest on the generic ACP driver, never a change to core.

### Non-goals (for now)

- Moving a conversation to another sandbox (it needs the agent's own transcript copied; [Later](#later)).
- Encrypting stored conversations ([storage-and-uploads § Encryption](agents/storage-and-uploads.md#encryption-later), Later).
- The OpenAI-compatible endpoint ([Later](#later)).
- Driving the agents' CLIs directly rather than through ACP adapters. It stays the fallback if Anthropic's answer on subscriptions calls for it ([profiles/agent-sign-ins](profiles/agent-sign-ins.md)).

## Keep, change, drop

| | What |
|---|---|
| **Keep** | ACP and the two adapters, pinned; agentd's event log, numbering across the conversation, chunk merging and the 64 KB cap; queue and steer; instructions; output schemas and the Codex stand-in; background tasks; worktrees on a conversation; models probed from the CLIs; plan mode; `on_unanswered`; held-back `finished`; idle unload of adapters; every API field and error code in the public docs |
| **Change** | Commands to agentd: over a channel agentd dials home, not a process per call ([agentd § Talking to agentd](agents/agentd.md#talking-to-agentd)). Events: stored in Postgres as they arrive, so followers read from the control plane ([turns-and-events](agents/turns-and-events.md#where-events-come-from)). Uploads: one transaction, no app hop. Sandbox ids stay on a conversation after the sandbox is deleted (deleted sandboxes remain readable). Admin keys and the dashboard read conversations through the Admin API. Agent tokens get their own signing key, not the port-link key |
| **Add** | Per-conversation `env`, `skills`, `mcp_servers`, `allowed_tools`; `on_unanswered: wait` and a per-prompt `reply_timeout`; a prompt's `metadata`; a conversation-wide event stream (finds turns the agent starts itself); a turn's `text` and long-poll `wait`; standalone worktrees (`wtr_`); slash commands kept; memory-aware unloading; `parent_call_id` on sub-agents' events where the adapter reports it; webhooks for turns and pending approvals |
| **Drop** | The app's internal conversation endpoints and the gateway's app client for them; `GATEWAY_PORT_LINK_KEY` doubling as the agent token key; `lewbox-agentd call --base64` (the 128 KiB argument limit, `4776361`); `?keep_worktree` as something callers must work out (worktrees are removed with their last conversation) |

## How it fits together

```
 Project's app / dashboard / Lewagent                          Control plane (lewbox serve)
 ─────────────────────────────────────                         ─────────────────────────────────────────────
 POST /v0/sandboxes/{sbx}/conversations ────────────────────▶  agents service
 POST /v0/conversations/{cnv}/messages                          • checks key, sandbox running, limits
 PATCH …/messages/{msg}  POST …/replies                         • records conversation, prompt, turn (one tx)
 GET  …/messages/{msg}/events (SSE)  ◀──────────────────────    • sends the command to agentd ─────────┐
                                                                • serves events from Postgres + NOTIFY  │
                                                                • stores uploads, ends turns, webhooks  │
                                                                                 ▲                      │
                                       agent channel (WebSocket, agent token)    │ events, uploads      │ commands
 Sandbox ────────────────────────────────────────────────────────────────────────┼──────────────────────┼─────
   lewbox agentd serve  (one per sandbox, started through the toolbox)           │                      ▼
     /root/.lewbox/agents/<cnv>/conversation.json, events.jsonl, adapter.log ────┘
     ├── claude-agent-acp ─▶ Claude Agent SDK ─▶ claude (the template's)      ACP over stdio
     └── codex-acp        ─▶ lewbox agentd (codex stand-in) ─▶ codex app-server
```

1. **Starting agentd.** The first prompt in a sandbox finds no agentd on the channel, so the control plane uploads the sandbox binary if needed and starts `lewbox agentd serve` as a toolbox session in a login shell, then waits for it to dial in ([agentd](agents/agentd.md#starting-and-upgrading)).
2. **A prompt** is recorded in Postgres (conversation, prompt message, turn message) under a lock on the conversation, then sent to agentd as a `prompt` command with an upload token, and answered `201` once agentd says the turn has started or been queued ([conversations](agents/conversations.md#start-a-conversation)).
3. **agentd** starts or reuses the conversation's adapter, applies settings, sends `session/prompt`, maps ACP updates to Lewbox events, numbers them, appends them to `events.jsonl`, and sends them up the channel as they happen ([turns-and-events](agents/turns-and-events.md)).
4. **The control plane** appends events to `agent_events`, `NOTIFY`s followers on any replica, merges chunks as agentd sends them, and when a turn's `finished` arrives, records it, updates the conversation, writes activity and enqueues webhooks, all in one transaction ([storage-and-uploads](agents/storage-and-uploads.md)).
5. **Replies, cancels, steers, task stops** are commands on the same channel ([approvals-and-questions](agents/approvals-and-questions.md), [background-tasks](agents/background-tasks.md)).

The agent channel is decided, in the alpha (2026-10-29). Legacy's path (commands as `lewbox agentd call --file` through the toolbox; events from `lewbox agentd events --follow` over exec-stream) isn't kept: the toolbox only installs and starts agentd, and a sandbox that can't reach the control plane fails clearly with `agent_unavailable` ([agentd § No second path](agents/agentd.md#no-second-path)).

## Core and agent plugins

The agent **framework** is core; each **agent** is a plugin ([plugins](plugins.md), [plugins/agent-providers](plugins/agent-providers.md)). Turning an agent's plugin off for a project hides it from `GET …/agents`, refuses new conversations with it (`422 plugin_disabled`), and stops writing its credentials into sandboxes; its conversations stay readable.

| Core (the framework) | Each agent provider plugin (`plugins/agents/<name>/`) |
|---|---|
| Conversations, messages, chunks, events and their API; queue and steer; approvals and questions; worktrees; background tasks | Its id and name (`claude-code`, `codex`, `opencode`) |
| agentd: the ACP client, the event log, chunking, uploads, the agent channel, idle unloading | Its **adapter**: the ACP package to install, its pinned version and command line (`claude-agent-acp@…`, `codex-acp@…`, `opencode acp`) |
| The sign-in machinery and the `Flow` interface ([profiles/agent-sign-ins](profiles/agent-sign-ins.md)) | Its credential methods, verifiers and sign-in flows ([profiles/agent-credentials](profiles/agent-credentials.md)) |
| The credentials store and the setup step runner ([sandboxes/profile-setup](sandboxes/profile-setup.md)) | Its setup contributor: files and env its credentials become, paths to scrub and harvest before a snapshot |
| Models-and-modes probing and caching | Its mode mapping (`default`, `accept_edits`, `plan`, `auto`, `bypass` → the agent's own), effort and fast-mode support, and quirks ([models-and-modes](agents/models-and-modes.md)) |
| Telemetry plumbing (`withTelemetry`) | How its CLI reads OpenTelemetry settings (env for Claude Code, `CODEX_CONFIG` for Codex) |
| | Its in-sandbox **driver**, compiled into the sandbox build (`plugins/agents/<name>/agentd`, build tag `sandbox`), only when the agent needs code: Codex's output-schema proxy and `CODEX_CONFIG` instructions; Claude Code's `systemPrompt` append and background-task stop |
| | What it adds to the provided `agents` and `claude-code` templates ([templates/provided-templates](templates/provided-templates.md)) |

**A generic ACP driver** in core runs any agent whose adapter speaks plain ACP, driven entirely by the plugin's manifest. OpenCode (`opencode acp`) is the first expected to need no driver code. Claude Code and Codex keep the drivers ported from legacy agentd (`internal/agentd/agents.go`, `codexproxy.go`, `tasks.go`), moved into their plugins.

## Concepts

| Concept | Id | What it is | Plan |
|---|---|---|---|
| **Agent** | `claude-code`, `codex` | A coding agent Lewbox can run, through its ACP adapter | [agentd](agents/agentd.md#agents) |
| **Conversation** | `cnv_` | One session with one agent, in one sandbox and one directory, possibly in a worktree. Keeps its agent, directory and instructions for good | [conversations](agents/conversations.md) |
| **Message** | `msg_` | A prompt (`role: user`) or a turn (`role: agent`). A turn is the agent's work in answer to a prompt, or one it started itself | [conversations](agents/conversations.md#messages) |
| **Event** | number | One thing the agent did during a turn, numbered across the conversation | [turns-and-events](agents/turns-and-events.md) |
| **Chunk** | (message, sequence) | What's stored of a turn: events merged (a run of text, a tool call's last state) | [storage-and-uploads](agents/storage-and-uploads.md#chunks) |
| **Request** | `req_` | An approval or a question the agent is waiting on | [approvals-and-questions](agents/approvals-and-questions.md) |
| **Mode** | `default`, `accept_edits`, `plan`, `auto`, `bypass` | How much the agent may do without asking, in Lewbox's words, mapped to each agent's own | [models-and-modes](agents/models-and-modes.md) |
| **Task** | the agent's id | Work the agent runs in the background (commands, monitors, subagents, workflows) | [background-tasks](agents/background-tasks.md) |
| **Worktree** | `wtr_` | A git worktree under `/root/workspace/.worktrees/`, made for a conversation or on its own | [worktrees](agents/worktrees.md) |

### States

```
conversation:  idle ⇄ running ⇄ waiting          failed (last turn failed; a prompt clears it)
turn:          queued → running ⇄ waiting → completed | cancelled | failed | interrupted
request:       pending → answered | expired
```

## API at a glance

Consumer API (profile keys and sandbox keys; [api](api.md) for conventions). All but listing and reading conversations need `sandboxes.agents`.

| Route | Plan |
|---|---|
| `GET /v0/sandboxes/{sbx}/agents` | [models-and-modes](agents/models-and-modes.md#the-options-endpoint) |
| `POST /v0/sandboxes/{sbx}/conversations` | [conversations](agents/conversations.md#start-a-conversation) |
| `GET /v0/conversations`, `GET /v0/conversations/{cnv}`, `DELETE /v0/conversations/{cnv}` | [conversations](agents/conversations.md) |
| `POST /v0/conversations/{cnv}/messages`, `GET …/messages`, `GET …/messages/{msg}`, `PATCH …/messages/{msg}` | [conversations](agents/conversations.md#prompt-again) |
| `GET /v0/conversations/{cnv}/messages/{msg}/events`, `GET /v0/conversations/{cnv}/events` | [turns-and-events](agents/turns-and-events.md#following) |
| `POST /v0/conversations/{cnv}/replies`, `GET /v0/conversations/{cnv}/requests` | [approvals-and-questions](agents/approvals-and-questions.md#replying) |
| `GET /v0/conversations/{cnv}/tasks`, `PATCH …/tasks/{task}` | [background-tasks](agents/background-tasks.md#api) |
| `POST /v0/sandboxes/{sbx}/worktrees`, `GET /v0/sandboxes/{sbx}/worktrees`, `GET`/`DELETE /v0/worktrees/{wtr}` | [worktrees](agents/worktrees.md#api) |
| `GET /agentd/v0/channel` (WebSocket) | agentd only, with its channel token: [agentd](agents/agentd.md#talking-to-agentd), [storage-and-uploads](agents/storage-and-uploads.md#uploads) |

The Admin API reads every profile's conversations in the project, with the same shapes, and can do what a profile key with `sandboxes.agents` can ([api](api.md) settles the prefix).

### Errors

Codes kept from legacy, all in [api/errors](api/errors.md):

| Status | Codes |
|---|---|
| `409` | `sandbox_not_running`, `turn_running`, `queue_full`, `not_steerable`, `not_queued`, `worktree_failed`, `worktree_dirty`, `worktree_in_use`, `worktree_removed`, `name_taken`, `task_not_stoppable` |
| `422` | `validation_failed`, `not_a_repository`, `agent_refused` |
| `502` | `agent_unavailable` |
| `503` | `agents_unavailable` |
| stream | `stream_interrupted`, `server_restarting` (was `gateway_restarting`) |

## Security

- **The sandbox is untrusted** ([README § Trust boundaries](README.md#trust-boundaries)). Anything agentd sends is a claim about one conversation, checked against its token: an upload for another conversation is refused, a turn number that goes backwards is refused, sizes are capped.
- **Agent tokens** are Ed25519-signed, audience `agentd`, claims `{sbx, cnv, exp}` (legacy `internal/agents`), now signed with their own key in the keyring, rotatable by `kid` ([foundation/secrets-and-encryption](foundation/secrets-and-encryption.md)). A token reaches one conversation's uploads and its share of the channel, nothing else.
- **Per-conversation secrets** (`env` values, MCP servers' env and headers) are stored encrypted, sent to agentd with each prompt, held in agentd's memory and the adapter's environment only, never written to `conversation.json` ([conversations § Environment](agents/conversations.md#environment-skills-mcp-servers-and-tools)).
- **Instructions are the control plane's copy.** agentd takes the copy in each prompt over the one on disk, which anything in the sandbox could edit (legacy `docs/acp.md` § Instructions).
- **Never `authenticate` or `logout`** on an adapter, and `/login` and `/logout` are hidden from slash commands: Lewbox owns logins ([profiles/agent-credentials](profiles/agent-credentials.md)).
- **No `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` or `apiKeyHelper` when a profile uses a Claude subscription**, as each wins over `CLAUDE_CODE_OAUTH_TOKEN` and bills the API instead; agentd removes them from the adapter's environment (legacy `docs/acp.md` § What the runner has to get right).
- **Conversations are stored unencrypted**, and a project can read its profiles' conversations. The docs say so plainly; encryption is designed and Later.
- **Claude subscriptions:** whether storing `setup-token`s is allowed is an open legal question ([profiles/agent-sign-ins](profiles/agent-sign-ins.md)). Nothing in this plan depends on the answer; API-key logins are unaffected.

## Observability

- **Activity** ([activity-and-webhooks/activity-log](activity-and-webhooks/activity-log.md)): `agent.prompted` (by the key, with `label`), `agent.turn_ended` (with `stop_reason`, `usage`, `label`), new `agent.waiting` (an approval or question opened), `worktree.created`, `worktree.removed`. Never a prompt's text.
- **Webhooks** ([activity-and-webhooks/webhooks](activity-and-webhooks/webhooks.md)): `activity.agent.turn_ended` (Lewagent settles checkout tasks on it today), new `activity.agent.turn_started` (including turns the agent started itself), `activity.agent.waiting`, `activity.worktree.created`, `activity.worktree.removed`.
- **Metrics:** turns started and ended by agent and stop reason; turn duration; time to first event; agentd starts, upgrades and channel reconnects; adapter starts and unloads; upload lag (event time to stored); open followers; events stored per second; chunks truncated.
- **Logs:** each command to agentd with its conversation, turn and duration; agentd's own log (`/root/.lewbox/agents/agentd.log`) readable through the dashboard's sandbox view for support.
- **Telemetry** of the agents themselves (tokens, prompts, tool calls, traces to Langfuse, Honeycomb, Grafana, LangSmith, OTLP) is the profile's: agentd adds the settings Lewbox's setup writes to each adapter's environment, with the conversation and agent as resource attributes (`5b56a01`; [profiles/telemetry](profiles/telemetry.md)).

## Configuration

Control plane (names follow [foundation/configuration](foundation/configuration.md)):

| Setting | Default | Meaning |
|---|---|---|
| `agents.enabled` | `true` | `503 agents_unavailable` when off |
| `agents.reply_timeout` | `10m` | Default wait for an approval or question |
| `agents.reply_timeout_max` | `24h` | The longest a prompt may ask for, and what `wait` means |
| `agents.queue_max` | `20` | Queued prompts per conversation |
| `agents.chunk_max_bytes` | `64KiB` | Cap on a stored chunk, sent to agentd with each prompt |
| `agents.output_schema_max_bytes` | `64KiB` | |
| `agents.instructions_max_chars` | `20000` | |
| `agents.event_retention` | `7d` | How long raw events are kept after their turn ends (chunks stay) |
| `agents.retention_days` | none | Conversations with no new message for this long are deleted; a project can set its own |
| `agents.token_ttl` | `24h` | Agent token lifetime; refreshed on the channel |
| `agents.adapter_idle` | `10m` | Unload an idle adapter after this; sent to agentd |
| `agents.background_idle` | `2h` | Keep an adapter with background work this long |
| `agents.adapters.claude-code` | `@agentclientprotocol/claude-agent-acp@0.94.0` | Pinned adapter package |
| `agents.adapters.codex` | `@agentclientprotocol/codex-acp@2.2.2` | Pinned adapter package |

agentd's own settings keep the `LEWBOX_AGENTD_*` names from legacy (`internal/agentd/config.go`) and are listed in [agentd § Configuration](agents/agentd.md#configuration).

## Build plan

Packages ([README § Repository layout](README.md#repository-layout)):

| Package | What |
|---|---|
| `internal/acp` | ACP v1 types and a JSON-RPC 2.0 stdio peer, split out of legacy `internal/agentd/rpc.go` so tests and agentd share it |
| `internal/agentd` | The in-sandbox daemon, ported from legacy, plus the channel client and the generic ACP driver |
| `plugins/agents/claude-code`, `plugins/agents/codex` | The two agent provider plugins: adapters, credential methods, sign-in flows, setup contributors, mode mappings, and their agentd drivers (Claude's tasks and `systemPrompt`; Codex's proxy and `CODEX_CONFIG`) |
| `plugins/agents/opencode` | Planned: a manifest on the generic ACP driver |
| `internal/agents` | Control plane: conversations, messages, chunks, events, worktrees, options cache, the channel hub, uploads, starting agentd |
| `internal/httpapi` (agents handlers) | The routes above |
| `internal/testing/fakeacp` | The scripted fake adapter (legacy `fake_adapter_test.go`, re-exec with `AGENTD_FAKE_ADAPTER=1`), exported for every package's tests |

Order, with rough sizes:

1. **Port agentd** into the sandbox binary, its tests green, splitting each agent's specifics into its plugin's driver behind the provider interface (4 days). [agentd](agents/agentd.md)
2. **Tables, the agents service, the Consumer API routes**, with uploads straight into Postgres (5 days). [conversations](agents/conversations.md), [storage-and-uploads](agents/storage-and-uploads.md)
3. **Events in Postgres and followers served from it**, with `NOTIFY` (3 days). [turns-and-events](agents/turns-and-events.md)
4. **The agent channel**: commands, events and uploads over it; built before the routes go live, as it's the only path (4 days). [agentd](agents/agentd.md#talking-to-agentd)
5. **Lewagent's asks:** env, skills, MCP servers, allowed tools, `wait`, metadata, turn `text`, conversation-wide stream, unloading, Codex race (5 days).
6. **Standalone worktrees** (3 days). [worktrees](agents/worktrees.md)
7. **Dashboard chat** ([dashboard](dashboard.md)) and **SDK** methods ([sdks-and-cli](sdks-and-cli.md)), in their own plans.

Steps 1–3 are the alpha's floor; 4–6 are planned for the alpha and can slip to Beta one by one without breaking the API, as none changes a shape the alpha ships.

## Index

| Plan | What it covers | Milestone |
|---|---|---|
| [agentd](agents/agentd.md) | The in-sandbox runner: binary, install, start and upgrade, the channel, commands, adapters, environment, idle unload, Codex stand-in | Alpha (M3) |
| [conversations](agents/conversations.md) | Starting, prompting, queue and steer, cancel, delete, instructions, structured output, content blocks, env/skills/MCP/tools, metadata, the objects | Alpha (M3) |
| [turns-and-events](agents/turns-and-events.md) | Every event type, SSE format, numbering, resume, held-back `finished`, turns the agent starts itself, where events come from, cross-replica streaming, stop reasons, reading a turn without the stream | Alpha (M3) |
| [approvals-and-questions](agents/approvals-and-questions.md) | Approvals, questions and their forms, replies, declining, timeouts and `on_unanswered` (with `wait`), plan mode, what's pending | Alpha (M3) |
| [models-and-modes](agents/models-and-modes.md) | Probing models and efforts, modes and their mapping, fast mode, slash commands, the options endpoint | Alpha (M3) |
| [worktrees](agents/worktrees.md) | Worktrees made with a conversation, and standalone worktrees (`wtr_`) | Alpha (M3), standalone Beta |
| [background-tasks](agents/background-tasks.md) | Claude's background tasks: listing, `task` events, stopping, keeping the sandbox awake | Alpha (M3) |
| [storage-and-uploads](agents/storage-and-uploads.md) | Tables, chunks and the 64 KB cap, the upload protocol and tokens, usage and cost, retention, deletion, encryption (Later) | Alpha (M3) |

## Later

- **The OpenAI-compatible endpoint** (`POST /v0/openai/responses`), so any OpenAI client (Laravel AI's `openai` driver) can drive an agent as if it were a model: the sandbox in `metadata`, `previous_response_id` continuing a conversation, the agent's own tool calls as display-only items, approvals and questions as calls to tools the client declares (`lewbox_approve`, `lewbox_ask`), `text.format: json_schema` as `output_schema`. Designed in legacy `docs/acp.md` § OpenAI-compatible endpoint; about 1.5 weeks.
- **More agents** over ACP: OpenCode, Gemini, pi. Each is an agent spec ([agentd § Agents](agents/agentd.md#agents)) and a provided template.
- **Moving a conversation to another sandbox**, by saving the agent's own transcript (Claude's `~/.claude/projects/…/<id>.jsonl`, Codex's thread under `$CODEX_HOME`) to the cluster's storage as each turn ends.
- **Switching agents within a conversation** (Lewagent's "not yet"), likely as a new conversation seeded with a summary.
- **Conversation search** across a profile's conversations (`small-todo.md`): Postgres full-text over `text` chunks.
- **Encrypted conversations** ([storage-and-uploads § Encryption](agents/storage-and-uploads.md#encryption-later)).

## Open questions

1. ~~**The agent channel in the alpha, or Beta?**~~ Decided (2026-10-29): the channel, in the alpha (M3), with no toolbox fallback ([agentd § No second path](agents/agentd.md#no-second-path)).
2. **Raw events in Postgres:** acceptable write volume? A cancelled 250-word story was 117 text events (55 KB) in legacy's spike. Proposed: agentd coalesces text deltas over ~50 ms before sending; events are pruned 7 days after their turn ([turns-and-events § Where events come from](agents/turns-and-events.md#where-events-come-from)).
3. **Does a sandbox always reach the control plane?** Uploads already need it. If projects get egress rules for sandboxes ([inside-a-sandbox](inside-a-sandbox.md)), the control plane's URL has to stay allowed.
4. **Starter templates with adapters preinstalled** (legacy `docs/acp.md` open decision 6), or installed by agentd on first use (57 MB with `--omit=optional`, ~26 s)? Proposed: both; provided `claude-code` and `codex` templates preinstall ([templates/provided-templates](templates/provided-templates.md)).
5. **Conversations counting against limits:** a stored-bytes limit per profile, or only retention (legacy open decision 4)?
6. **Codex approvals under gVisor** need a "no sandbox, ask first" preset in `codex-acp` (`dangerFullAccess` with `on-request`). Propose it upstream, as with AerolVM ([models-and-modes § Codex runs with full access](agents/models-and-modes.md#codex-runs-with-full-access)).
