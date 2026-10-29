# Storage and uploads

> Status: Draft · Milestone: Alpha (M3), encryption Later · Core (agent framework) · Updated: 2026-10-29

Lewbox keeps every conversation outside the sandbox: its prompts, what the agent did in each turn merged into **chunks**, approvals and questions with their answers, its title, settings, status, and each turn's usage and how it ended. It can be read back after the sandbox is gone. agentd sends what's stored up to the control plane (**uploads**), authenticated by **agent tokens**. This plan covers the tables, chunks, the upload protocol, tokens, usage, retention, deletion, and the design for encrypting conversations later.

## Today

- **Tables** in the app (legacy `docs/acp.md` § Tables, after `laravel-ai-sessions`): `agent_conversations` (`cnv_`), `agent_conversation_messages` (`msg_`), `agent_conversation_chunks`; MySQL in production.
- **Uploads:** agentd's uploader (`internal/agentd/upload.go`, `chunks.go`) merges events into chunks numbered from each turn's start (so a replay makes the same ones), and posts them with turns' states and conversation changes to the gateway's `POST /v1/agent-uploads` every second while there's something new, with a heartbeat at least every minute while the conversation isn't idle, backing off to 30 s on errors. `uploaded_through` in `conversation.json` is the last event the gateway has had; after a restart agentd replays its log and sends the rest.
- **The gateway** (`receiveUpload`, `internal/server/conversations.go:1224`) checks the token (Ed25519, audience `agentd`, claims `{aud, sbx, cnv, exp}`, signed with `GATEWAY_PORT_LINK_KEY`, `GATEWAY_AGENT_TOKEN_TTL` 24 h), touches the sandbox, then calls the app: `UpdateConversation` (`agent_session_id`, `title`, `mode`, `model`, `context`, `rate_limit`), `StartTurn` for a turn the agent started itself, `UpdateTurn` per turn, `RecordChunks` in batches; some refusals are "ignorable" (a turn already ended, a mode the app doesn't name). It answers `{through}`.
- **Caps** (`68ca185`, `CapPrompt`): chunk content capped at `lewbox.agents.chunk_max_bytes` (64 KB), except `text` and `reasoning`; a long string keeps its start and end around a note of the bytes cut, and `"truncated": true`. Prompt chunks cut only base64 `data` and `blob`.
- **A MySQL failure** (2026-10-23): eager-loading chunks ordered by `(message, sequence)` sorted whole rows in memory, and a chunk over `sort_buffer_size` (256 KB) failed reads with `1038 Out of sort memory`; chunks were then sorted in PHP. Postgres doesn't have this problem with an index on `(message_id, sequence)`.
- **Retention:** `lewbox.agents.retention_days` (null keeps forever), pruned daily.
- **Encryption:** designed and deferred (2026-10-21).

**Problems:** three hops per upload (agentd → gateway → app → database), a token a refused upload must wait a prompt to replace, cost and usage only learned a second late, and PHP's empty-object-as-array trap (outputs had to travel as JSON text).

## Goals

- One write path: an upload is applied in one transaction, straight into Postgres.
- Idempotent and order-safe: replays, retries and reconnects never duplicate or regress anything.
- Tokens that refresh themselves.
- Keep conversations after their sandbox is deleted; delete them completely when asked.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Chunk kinds, merging, numbering from each turn's start, the 64 KB cap and how it cuts, prompt chunks cutting only base64, `uploaded_through` replay, heartbeat while not idle, retention by days, deleting everything Lewbox kept on delete |
| **Change** | Uploads come over the [agent channel](agentd.md#the-agent-channel), the only path (no HTTP upload endpoint), and are applied directly. One channel token per sandbox replaces the per-conversation upload token. Agent tokens have their own signing key with key ids. `output_schema` and `output` are stored as Postgres `json` (exact text) |
| **Add** | Token refresh without a prompt; `text` computed for each ended turn; per-project retention; stored bytes counted per profile; raw events beside chunks ([turns-and-events](turns-and-events.md#where-events-come-from)) |
| **Drop** | The app's internal conversation endpoints; batching chunks between services; "ignorable" refusal handling across a service boundary (it becomes a rule in one function) |

## Design

### Tables

Ids are `bigint` internally with a `public_id` for the API ([foundation/ids-and-keys](../foundation/ids-and-keys.md)); every row carries `project_id` for scoping ([foundation/database](../foundation/database.md)).

**`agent_conversations`**

| Column | Type | Notes |
|---|---|---|
| `id`, `public_id` | | `cnv_` |
| `project_id`, `profile_id` | | Cascade on profile delete |
| `sandbox_id` | | Kept after the sandbox is deleted (sandboxes are never hard-deleted while referenced) |
| `agent` | text | |
| `agent_session_id` | text | ACP's `sessionId`: Claude's session or Codex's thread |
| `directory` | text | |
| `worktree_id` | | → `agent_worktrees` |
| `instructions` | text | |
| `env` | bytea | Encrypted ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)); names also in `env_names text[]` |
| `skills` | jsonb | `[{name, files}]` |
| `mcp_servers` | bytea | Encrypted whole; `mcp_server_names jsonb` (`name`, `type`) beside it |
| `allowed_tools`, `disallowed_tools` | text[] | |
| `model`, `effort`, `mode`, `fast`, `on_unanswered`, `reply_timeout` | | As last set |
| `title`, `status` | text | |
| `usage`, `context`, `rate_limit`, `commands` | jsonb | |
| `started_by_type`, `started_by_id` | | Actor, as in the activity log |
| `message_sequence` | int | Last sequence given |
| `events_through` | bigint | Last event id stored |
| `idempotency_key`, `request_fingerprint` | | Unique `(profile_id, idempotency_key)`, kept 24 h ([api/idempotency](../api/idempotency.md)) |
| `created_at`, `updated_at`, `last_message_at` | timestamptz | |

**`agent_messages`**

| Column | Type | Notes |
|---|---|---|
| `id`, `public_id` | | `msg_` |
| `conversation_id`, `sequence` | | Unique together; `sequence` null while queued |
| `role`, `origin`, `sender`, `status`, `stop_reason` | text | |
| `turn` | int | agentd's number; unique `(conversation_id, turn)` |
| `prompt_message_id` | | A turn's prompt |
| `model`, `mode` | text | What a turn ran with |
| `usage` | jsonb | |
| `output_schema`, `output` | json | `json`, not `jsonb`, so they're kept exactly as sent |
| `text` | text | A turn's answer, computed as it ends ([conversations § Messages](conversations.md#messages)) |
| `label` | text | |
| `metadata` | jsonb | A prompt's |
| `idempotency_key` | text | Unique `(conversation_id, idempotency_key)` |
| `actor_type`, `actor_id` | | Who prompted |
| `created_at`, `started_at`, `finished_at` | timestamptz | |

**`agent_chunks`**

| Column | Type | Notes |
|---|---|---|
| `message_id`, `sequence` | | Primary key |
| `kind` | text | Below |
| `external_id` | text | A tool call's id, or a request's |
| `status` | text | A tool call's; a request's `pending`, `answered`, `expired` |
| `content` | jsonb | |
| `size` | int | Bytes of `content`, for accounting |
| `truncated` | bool | |
| `updated_at` | timestamptz | |

Partial index `(conversation_id) WHERE status = 'pending' AND kind IN ('approval_request','question')` (with `conversation_id` denormalized onto the row for it).

Also: `agent_events` ([turns-and-events](turns-and-events.md#where-events-come-from)), `agent_tasks` ([background-tasks](background-tasks.md)), `agent_worktrees` ([worktrees](worktrees.md#data)), `agent_options` ([models-and-modes](models-and-modes.md#data)), `agentd_connections` and `agentd_commands` ([agentd](agentd.md#the-agent-channel)).

### Chunks

What's stored of a turn: its events merged, never a row per token.

| Kind | `content` |
|---|---|
| `prompt` | `blocks`: the prompt's content blocks (stored by the control plane as it records the prompt) |
| `text` | `text`: a run of the agent's reply, consecutive deltas of one `message_id` merged |
| `reasoning` | `text`: a run of its thinking |
| `tool_call` | The call's last state, as its `tool_call` event has it, with a Codex command's output joined into its `terminal` item's `text` (start and latest, within the cap) and `exit_code`, `signal`, `directory` |
| `plan` | `entries`, as last updated |
| `approval_request` | The event's fields, and once answered `answer` (the `answered` event's fields, with `actor`) |
| `question` | Likewise |
| `error` | `text` |
| `raw` | `payload`, as it came |

**The cap.** `content` is kept to `agents.chunk_max_bytes` (64 KB) of JSON, except `text` and `reasoning` (the agent's own words, kept whole). Over it, each string over a limit (the largest first) keeps its start and end around a note of the bytes cut, until it fits, and the chunk gets `"truncated": true`. agentd cuts before uploading (the limit comes in `welcome` and each `prompt`); the control plane checks and cuts again, so a misbehaving sandbox can't store more. A `prompt` chunk keeps its text and other fields (an image's `uri`, `mimeType`) whole and cuts only images' and files' base64 `data` and `blob`. The live events are never cut (beyond the 1 MB per-event bound), so a client wanting a whole tool output takes it from the events.

**Sizes seen** (legacy spike): a one-word answer 2 KB (Codex) to 28 KB (Claude) of events; small tool-using turns 6–37 KB; Claude's transcript in the sandbox 150–240 KB for a short session.

### Uploads

An upload carries one conversation's changes since the last acknowledged one:

```json
{
  "conversation": "cnv_…",
  "through": 128,
  "updates": { "agent_session_id": "…", "title": "…", "mode": "plan", "model": "opus",
               "context": {"tokens": 51234, "size": 200000, "at": "…"}, "rate_limit": {…}, "commands": [...] },
  "turns": [ { "turn": 7, "message": "msg_…", "status": "completed", "stop_reason": "end_turn",
               "usage": {…}, "model": "opus", "output": {…}, "origin": "prompt", "sender": null } ],
  "chunks": [ { "turn": 7, "sequence": 3, "kind": "tool_call", "external_id": "toolu_…",
                "status": "completed", "content": {…}, "truncated": false } ]
}
```

**Applied in one transaction**, under a row lock on the conversation:

1. Check the conversation belongs to the channel token's sandbox.
2. Apply `updates`; unknown fields are ignored; `mode` is mapped or kept as the adapter's own name.
3. For each turn: a turn with no `message` is one the agent started itself, so create its message (`origin: agent`, next sequence) and remember it for the chunks. Apply status changes only forward (`queued` → `running` ⇄ `waiting` → ended); a stale or repeated one is skipped, not refused. On a turn's end: set `finished_at`, add its usage to the conversation's, compute `text`, set the conversation's status, write `agent.turn_ended` activity and enqueue its webhook.
4. Upsert chunks by `(message_id, sequence)`, replacing what's there, keeping `actor` on answered requests. Chunks for an unknown turn are skipped.
5. On the channel: insert the turn's `finished` event only now, so `finished` is visible only once everything it stands for is stored ([turns-and-events](turns-and-events.md#where-events-come-from)).
6. Record sandbox activity (an upload counts, keeping the sandbox awake while its agent works or waits) ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md)).
7. Answer `{through}` (`ack` on the channel), so agentd advances `uploaded_through`.

**Sizes:** an `upload` frame is capped at 8 MB (the channel's read limit); agentd splits larger ones.

**Cadence:** an upload follows within ~250 ms of a change, and at least every minute while the conversation isn't idle (the heartbeat). While the channel is down, agentd keeps everything in its log and sends it after the next `welcome`.

### Tokens

| Token | Claims | Given | Reaches |
|---|---|---|---|
| Channel token | `{aud: "agentd", sbx, iat, exp}` | When agentd starts; refreshed by a `token` frame an hour before expiry | The channel for this sandbox, and uploads for its conversations |

- Format: `base64url(header).base64url(claims).base64url(signature)`, Ed25519, header `{alg: "EdDSA", kid}`. Signed with the **agent-token key** in the keyring, not the port-link key legacy shared (`GATEWAY_PORT_LINK_KEY`). Verification accepts the current and previous keys, so rotation never cuts an agent off ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)).
- TTL `agents.token_ttl` (24 h), refreshed over the channel before it expires, so a long background task never waits for a prompt to upload again.
- A token for a deleted sandbox or conversation is refused `401 invalid_token`; agentd stops uploading for that conversation and removes its directory.

### Usage and cost

- **Per turn:** the prompt response's `usage` (`input_tokens`, `output_tokens`, `cached_read_tokens`, `cached_write_tokens`, `reasoning_tokens`, `total_tokens`) and, from Claude, `cost` with `currency`. Claude's `cost` is a running total for the adapter's session (`total_cost_usd`), so agentd gives each turn, and each `usage` event, the difference since the last report (`4c370f2`); it restarts from zero when the adapter does. Codex reports no cost.
- **Steered runs:** each part has the cost it ran up; the tokens for the whole run are on the last turn.
- **Per conversation:** `usage` adds up its ended turns; `context` (`tokens`, `size`, `at`) and `rate_limit` are the latest reported.
- **Meaning:** `cost` is what the same work would cost at the provider's API prices, reported on a subscription too, so it doesn't say who paid. Lewbox doesn't bill it; the provider bills the profile's login. `rate_limit` shows a Claude subscription's windows (five-hour, seven-day).
- **Usage endpoints** (Beta, [credits/budgets-and-usage](../credits/budgets-and-usage.md)) can sum turns' usage per profile per day for projects that want to show it.

### Retention

- **Raw events:** pruned `agents.event_retention` (7 days) after their turn ends.
- **Conversations:** `agents.retention_days` (instance default none); a project can set its own, and later a profile. A daily job deletes conversations with no new message for that long, with their messages, chunks, events, tasks and worktree records. The conversation stays in the sandbox (agentd's log and the agent's transcript); prompting a pruned one is `404`.
- Activity events about conversations follow the activity log's retention ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md)).

### Deletion

- **A conversation:** [conversations § Delete](conversations.md#delete-a-conversation). Everything Lewbox kept goes in one transaction; agentd's copy goes now or on its next start.
- **A sandbox:** conversations stay readable, with `sandbox_status: deleted`; they can't be prompted.
- **A profile:** its conversations are deleted with it.
- **Backups** keep deleted rows for their own retention; the docs say so.

### Accounting

`size` is summed per profile as stored bytes (chunks plus prompts), shown on the profile and in the dashboard. Whether it becomes a limit kind (`agent_storage_mb`) is an open question (legacy open decision 4).

### Encryption (later)

Designed in legacy `docs/acp.md` § Encryption (later), deferred on 2026-10-21, kept here so it isn't lost:

- **Settings per profile:** *Store agent conversations* (off keeps rows, statuses and usage but no chunks; agentd deletes a turn's JSONL an hour after it ends), *Encrypt agent conversations*, *Encrypt the live stream too* (the control plane relays ciphertext; the OpenAI-compatible endpoint is then unavailable).
- **Keys:** `agent_encryption_keys` (`enk_`) per profile, X25519. Bring your own public key (backends keep the private key), or a master password in the browser: PBKDF2-SHA256 600,000 rounds, HKDF into a wrap key and an auth hash (`lewbox/agents/wrap`, `lewbox/agents/auth`), the private key wrapped with AES-256-GCM, and again under a recovery key shown once (after `hidemy.info`'s vault).
- **Scheme:** a fresh 256-bit content key per turn, sealed to the active public key with HPKE (RFC 9180: DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, AES-256-GCM, info `lewbox/agents/message`), stored as the message's `sealed_key`. Each chunk's content sealed with AES-256-GCM under its turn's key, `iv || ciphertext || tag`, additional data `cnv_…/msg_…/<sequence>`. agentd encrypts in the sandbox, so plaintext for storage never leaves it. Go 1.27 has `crypto/hpke` in the standard library.
- **Readable to Lewbox still:** ids, times, agent, model, mode, statuses, stop reasons, chunk kinds, tool calls' `kind` and `status`, sizes, usage and cost.
- **Not protected:** the sandbox sees everything (and the agent's own transcript is plaintext there and in snapshots); the project has root on its nodes and its bucket; without stream encryption the live stream passes the control plane in plaintext over TLS; answers reach the sandbox in plaintext; a lost password and recovery key lose the conversations for good.
- About 1.5 weeks when it's picked up.

## Security

- Uploads are untrusted input: schema-checked, size-capped, scoped by token, applied only forward.
- Conversations are stored unencrypted, readable by the project through the Admin API and dashboard. The docs say so.
- Secrets in `env` and MCP servers are encrypted at rest and excluded from every API response and log.

## Observability

- Metrics: uploads applied, skipped (stale), refused (token, size); upload latency; bytes stored per profile; chunks truncated; retention deletions.
- Logs: refused uploads with sandbox, conversation and reason.

## Configuration

| Setting | Default | |
|---|---|---|
| `agents.chunk_max_bytes` | `64KiB` | |
| `agents.upload_max_bytes` | `8MiB` | |
| `agents.token_ttl` | `24h` | |
| `agents.retention_days` | none | Project-overridable |
| `agents.event_retention` | `7d` | |

## Testing

- Upload application: a scripted turn's uploads in order, out of order, repeated, partial; an agent-started turn; a steered pair; stale statuses skipped; chunks for unknown turns skipped; caps enforced server-side on oversize content.
- Tokens: expiry, refresh on the channel and in HTTP responses, rotation (old key still verifies), a token for another conversation or sandbox refused.
- `finished` never visible before its final upload is applied.
- Retention and deletion: everything gone, nothing orphaned (foreign keys and a check query).

## Build plan

1. Tables and migrations (1 day).
2. The upload applier behind the channel's `upload` frames; tokens with the keyring (2 days).
3. `text` computation, usage totals, accounting (1 day).
4. Retention jobs (½ day).

## Open questions

1. **A stored-bytes limit per profile,** or only retention?
2. **Retention per profile** as well as per project?
3. **Backups and deletion:** do we promise anything about deleted conversations in backups, or just document the backup retention?
4. **When encryption comes,** and whether the browser uses `@hpke/core` or our own HPKE over WebCrypto (a dependency either way).
