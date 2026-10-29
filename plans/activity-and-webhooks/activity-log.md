# Activity log

> Status: Draft · Milestone: Alpha (M4) · Core; plugins register their kinds · Updated: 2026-10-29

Each sandbox has an activity log: its operations, what was done inside it, requests about it that were refused, agents prompted and their turns ending, and events the project's own app adds. It answers "what happened to my sandbox, and why did that fail?" for a project's users and its support staff, without Lewbox ever keeping what a command said or printed.

The public contract is already documented at [lewbox.com/docs/sandboxes/activity](https://lewbox.com/docs/sandboxes/activity); this plan keeps it and describes how the single binary writes and serves it.

---

## Today

- Legacy app: `sandbox_events` (`evt_`), `app/Models/SandboxEvent.php`, `app/Enums/Sandbox/SandboxEventKind.php` (kinds and groups), `SandboxEventOutcome.php`, `StopReason.php`; the dashboard's Activity timeline on the sandbox page; `GET /api/v1/sandboxes/{sbx}/activity` and `GET /api/v1/profiles/{prf}/activity` on the Admin API; pruned daily with per-tenant retention (`tenants.activity_retention_days`, `succeeded_command_retention_days`, `record_command_stderr`; defaults 30 and 3, max 90, `config/lewbox.php` `activity`).
- Legacy gateway: records commands, terminals, sessions signalled, file writes, moves, removes, directories made, port links and refused requests (`internal/events/recorder.go`: queue of 10 000, batches of 500 every 2 s to `POST /api/gateway/v1/sandbox-events`, dropped when full); redacts stderr (`internal/server/activity.go`: `Authorization:` headers, `gh[pousr]_`/`github_pat_` tokens, `lb[pst]_` keys, bearer/basic credentials, URL userinfo; last 2 KB, redacted before cutting). Serves `GET /v1/sandboxes/{sbx}/activity`, `GET /v1/activity`, `POST /v1/sandboxes/{sbx}/activity` (app events, 60 a minute per key, `Idempotency-Key`).
- Labels: `^[a-z0-9][a-z0-9._-]{0,63}$`, from a body `label` or the `X-Lewbox-Label` header, on commands, prompts and app events.
- Lewagent records its own `app.*` events (`lewagent-legacy/app/Enums/Activity/LewagentEvent.php`, `RecordLewagentEvent`) and shows the log in its Activity panel.

What's wrong: two writers, lossy batching, no live stream, and reads during busy periods scanning one large table that a daily `DELETE` prunes.

## Goals

- Keep the public contract: the event object, filters, groups, outcomes, app events, limits.
- One in-process writer; nothing dropped silently.
- Cheap retention: dropping partitions, not deleting rows.
- Live following over SSE.
- Profile- and project-level events (connections, keys) in the same model.

## Design

### The event

| Field | Type | Notes |
|---|---|---|
| `id` | `evt_…` | |
| `kind` | string | Below; app events are `app.<name>` |
| `group` | string | Derived from `kind` |
| `source` | `lewbox` \| `client` | `client` for app events: the client's own claim |
| `scope` | `sandbox` \| `profile` \| `project` | New: where it belongs |
| `sandbox` | id or null | |
| `profile`, `project` | ids | Always set |
| `actor` | `{type, id, name}` | `type`: `user`, `admin_key`, `profile_key`, `sandbox_key`, `node`, `lewbox`. A sandbox key's events also carry `on_behalf_of`: the key or user that made it |
| `details` | object | By kind |
| `operation` | object or null | For `operation` events: the operation, with its progress |
| `outcome` | `succeeded`, `failed`, `timed_out`, `killed`, `refused` or null | |
| `label` | string or null | |
| `status`, `code`, `message` | | Refused requests: HTTP status and error code and message (≤ 1000 chars). Lewbox-initiated operations: the reason. App events: the summary |
| `request_id` | `req_…` | |
| `conversation` | `cnv_…` or null | Agent events |
| `stderr` | string or null | Commands that didn't succeed: last 2 KB, redacted. Off per project with `record_command_stderr: false` |
| `occurred_at` | timestamp | |

### Kinds

Kept from legacy, and new (marked ★):

| Group | Kinds |
|---|---|
| `operations` | `operation` (create, start, stop, restart, delete, apply_setup; with progress), `sandbox.stopped` (stopped on its own, with `reason`, and `backstop` when the node stopped it without Lewbox), ★ `sandbox.lost` (lost with its node), ★ `sandbox.idle_warning`, ★ `sandbox.idle_deferred` (an idle stop the re-check put off, with why) |
| `commands` | `command.ran` (`exit_code`, `duration_ms`, `timed_out`), `terminal.opened`, `terminal.closed` |
| `sessions` | `session.started`, `session.ended` (exit code, watched sessions within seconds), `session.killed`, `session.signalled` |
| `ports` | `port.exposed`, `port.closed`, `port.link_created` |
| `files` | `file.written`, `file.moved`, `file.removed`, `directory.made` (paths, sizes; never contents). Reads aren't logged |
| `keys` | `key.created` (sandbox keys, with their permissions and who made them) |
| `ssh_keys` | `ssh_key.added`, `ssh_key.removed` |
| `agents` | `agent.prompted` (no prompt text), `agent.turn_ended` (stop reason, usage, cost), ★ `agent.approval_requested`, ★ `agent.approval_answered` |
| ★ `worktrees` | `worktree.created`, `worktree.removed` ([agents/worktrees](../agents/worktrees.md)) |
| ★ `git` | `git.token_written`, `git.token_failed`, `git.repositories_changed`, `git.access_changed`, `github.token_issued` (when the issue names the sandbox) |
| `requests` | `request.refused` (method, route pattern, status, code) |
| ★ `setup` | `setup.applied` (each step's status), `repositories.cloned` |
| ★ `hooks` | `hook.ran` (hook, label, source, `exit_code`, `duration_ms`, stop reason; outcome `succeeded`, `failed`, `timed_out`, `killed`; redacted stderr tail on failure) ([sandboxes/hooks](../sandboxes/hooks.md#activity-and-webhooks)) |
| `app` | `app.*` |

Profile-scope (★): `github.connected`, `github.disconnected`, `github.installation_added`, `github.installation_removed`, `github.authorization_lost`, `github.token_issued` ([git/tokens](../git/tokens.md#the-token-api)), `agent.credential_set`, `agent.sign_in_completed`, `agent.sign_in_failed`, `telemetry.changed`, `ssh_key.added`/`removed` (profile keys), `key.created`/`revoked` (profile keys), `profile.suspended`, `profile.credits_exhausted`. Project-scope is the project audit log ([projects](../projects.md)), which uses the same table and writer.

Kinds are a Go enum with `Group()`, `Label()` (the dashboard's words, from legacy `SandboxEventKind::label()`), and `RaisesWebhook()`. Core owns the enum; **plugins register their kinds** with the same fields through the registry ([plugins/extension-points](../plugins/extension-points.md)): the `git` group's `github.*` kinds by `git/github`, and credits kinds by `credits`. Events of a disabled plugin stay in the log and are still listed.

### What's never kept

- A command's text, its stdout, its environment.
- A prompt's text, the agent's output.
- File contents.
- Any secret: stderr is redacted with the legacy gateway's patterns (`Authorization:` lines; `gh[pousr]_…`, `github_pat_…`; `lb[apstnil]_…` — all Lewbox prefixes; `bearer`/`basic` credentials of 8+ characters; URL userinfo → `***@`), **plus** every secret value Lewbox knows was handed to that sandbox (profile credentials, Git tokens, env values marked secret), matched literally. Redaction runs before the 2 KB cut, on twice that, so a cut can't leave half a secret unmatched.

So something like "Committed abc1234 Fix login redirect" comes from the project's app, as an app event.

### App events

`POST /v0/sandboxes/{sbx}/activity`:

| Field | |
|---|---|
| `kind` | Required. `app.` + 1–60 of `[a-z0-9._-]`, starting with a letter or digit |
| `label` | Optional, label rules |
| `outcome` | `succeeded` or `failed` |
| `message` | One line, ≤ 500 characters |
| `details` | ≤ 2 KB of JSON; values strings, numbers, booleans, null, or lists of those |
| `request_id` | The request it followed, to show them together |
| `occurred_at` | Within the last 24 hours; now by default |

`201` with the event. `Idempotency-Key`: same key and body → `200` with the first event and `Idempotent-Replayed: true`; same key, different body → `422 idempotency_key_reused`. 60 a minute per key (`429 rate_limited`, `Retry-After`). Kept the whole retention, whatever their kind.

### Reading

`GET /v0/sandboxes/{sbx}/activity`, `GET /v0/activity` (every sandbox the profile key sees), and for admin keys `GET /v0/projects/{prj}/activity` (the whole project) and `…/profiles/{prf}/activity`. Newest first, cursor pages (50, ≤ 100). Filters:

| Filter | |
|---|---|
| `kind` | Comma-separated or repeated, exact kinds or `app.*`-style prefixes |
| `group` | `operations`, `commands`, `sessions`, `ports`, `files`, `keys`, `ssh_keys`, `agents`, `worktrees`, `git`, `requests`, `setup`, `hooks`, `app` |
| `outcome` | `succeeded`, `failed` (failed, timed out, killed, failed operations), `refused`, `problems` (both) |
| `label` | Exact, or a prefix ending in `*` |
| `q` | Case-insensitive text in labels, codes, messages and stderr |
| `since`, `until` | `since` inclusive, `until` exclusive |
| `actor` | A `usr_` or `key_` id |
| `conversation` | `cnv_` |
| `sandbox` | On the profile- and project-wide lists |
| `scope` | `sandbox`, `profile`, `project` |

Permissions: `sandboxes.activity.view` or `sandboxes.activity` to read, `sandboxes.activity` to add. A sandbox key can read its own sandbox's (with either permission), not `GET /v0/activity`.

### Following live

`GET /v0/sandboxes/{sbx}/activity/stream` (and `/v0/activity/stream`): SSE, each event as `event: activity`, `id: <evt id>`, `data: <event JSON>`, with the same filters. Resumes with `Last-Event-ID` or `?after=<evt id>` (replaying from the table, then live). Live events arrive through `LISTEN activity_<project>` notifications carrying the event id; each replica's subscribers read the row by id and apply their filters ([api/streaming](../api/streaming.md)). Keepalive comments every 15 s.

### Storage

`activity_events`, **range-partitioned by day** on `occurred_at`:

- Columns as the event, with `kind` and `group` as small text, `details` as `jsonb`, `stderr` as `text`, `search` as a generated `tsvector` over label, code, message and stderr (for `q`, with a trigram index fallback for substrings).
- Indexes per partition: (`sandbox_id`, `occurred_at desc`), (`profile_id`, `occurred_at desc`), (`project_id`, `occurred_at desc`), (`conversation_id`) where not null, (`kind`), GIN on `search`.
- **Retention**: partitions older than the instance's `LEWBOX_ACTIVITY_MAX_RETENTION` (90 days) are dropped by a daily leader job. Shorter per-project retention (`projects.activity_retention_days`, default 30) and the shorter retention for successful commands (`succeeded_command_retention_days`, default 3) are applied by a batched `DELETE … WHERE project_id = $1 AND occurred_at < $2` per project per day partition, only on partitions past the shortest retention, so most rows leave by partition drop.
- App events are kept the project's full retention whatever their kind.

### The writer

Two paths, one table:

1. **In the change's transaction** (`activity.Record(ctx, tx, e)`): operations, status changes, keys, ports, sessions, agents, git, refused requests. These commit with the change, and their webhook events with them.
2. **Batched** (`activity.Enqueue(e)`): the high-volume kinds recorded where there's no transaction to join — `command.ran` and file events from the toolbox handlers. A per-replica writer goroutine collects up to 500 events or 200 ms, and writes them with `COPY` **and** raises their webhook events in one transaction.
   - The queue is bounded (`LEWBOX_ACTIVITY_QUEUE`, 50 000). When it's over 80%, the handlers that enqueue `command.ran` with outcome `succeeded` block for up to 50 ms, then shed **only successful commands** (counted in `lewbox_activity_shed_total{kind}`); failures, refusals and everything else wait. Shedding is logged once a minute with counts.
   - On shutdown the writer drains before the process exits (within the shutdown timeout).
   - A crash loses at most what's in one replica's queue (≤ 200 ms of commands). Accepted for successful commands; failed commands are recorded on path 1 (they're rare and matter).

### The idle-timeout interaction

Writing activity never counts as sandbox activity for the idle timeout; only work inside the sandbox does ([sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md)).

## API

| Method and path | Key | Permission |
|---|---|---|
| `GET /v0/sandboxes/{sbx}/activity` | profile, sandbox | `sandboxes.activity.view` or `sandboxes.activity` |
| `GET /v0/sandboxes/{sbx}/activity/stream` | profile, sandbox | same |
| `POST /v0/sandboxes/{sbx}/activity` | profile, sandbox | `sandboxes.activity` |
| `GET /v0/activity`, `/v0/activity/stream` | profile | `sandboxes.activity.view` or `sandboxes.activity` |
| `GET /v0/projects/{prj}/activity` | admin, dashboard | |
| `GET /v0/projects/{prj}/profiles/{prf}/activity` | admin, dashboard | |
| `GET /v0/projects/{prj}/sandboxes/{sbx}/activity` | admin, dashboard | |

Errors: `403 permission_denied`, `404 not_found`, `422 validation_failed`, `422 idempotency_key_reused`, `429 rate_limited`.

## Security

- Redaction as above; tested against a corpus of real stderr with tokens in every form legacy has seen.
- `q` searches only what the reader may see.
- App events are marked `source: client`; the dashboard shows them as the client's claim.
- A sandbox key's events are attributed to whoever made it (`on_behalf_of`), as legacy did.

## Observability

- Metrics: `lewbox_activity_events_total{kind,path}`, `lewbox_activity_queue_depth`, `lewbox_activity_shed_total{kind}`, `lewbox_activity_write_seconds`, `lewbox_activity_partition_drop_seconds`.
- Each event's `request_id` links it to the access log and traces.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_ACTIVITY_MAX_RETENTION` | `90d` | Partitions dropped after |
| `LEWBOX_ACTIVITY_DEFAULT_RETENTION` | `30d` | New projects |
| `LEWBOX_ACTIVITY_DEFAULT_SUCCEEDED_COMMAND_RETENTION` | `3d` | New projects |
| `LEWBOX_ACTIVITY_QUEUE` | `50000` | Per replica |
| `LEWBOX_ACTIVITY_CLIENT_EVENTS_PER_MINUTE` | `60` | Per key |

## Testing

- Redaction corpus; cut-after-redact.
- Writer: batching, backpressure, shedding only successful commands, drain on shutdown.
- Retention: partition creation ahead (a job makes the next 7 days' partitions), drops, per-project deletes.
- Streams: resume by id, filters, events from another replica (two servers on one database in tests).
- App events: idempotency, limits, `occurred_at` bounds.

## Build plan

1. Migration with partitioning helpers; partition maintenance job.
2. `internal/activity`: kinds, `Record`, `Enqueue` and the writer, redaction.
3. Read endpoints and filters; app events.
4. Streams.
5. Hook every producer (operations, toolbox handlers, middleware for refusals, agents, git).

## Open questions

1. **Profile and project scope in the same table**, or a separate audit log for project administration?
2. **Successful commands** at 3 days by default: still right now that writes are cheap?
3. **Read events** (file downloads, port link opens are logged; directory listings aren't): should file downloads be logged as `file.read`, for audit?
