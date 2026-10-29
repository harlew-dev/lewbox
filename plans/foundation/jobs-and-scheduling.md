# Jobs and scheduling

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

Everything Lewbox does that outlives a request is a job in Postgres: running a sandbox's operation, delivering a webhook, emailing a code, reconciling a cluster, pruning old rows. This plan covers the queue, how work is claimed, leased, retried and woken, how periodic work runs exactly once, and how live updates reach other replicas.

## Today

Legacy has two job systems, and neither knows about the other.

**The app** (`lewbox-legacy`): Laravel's `database` queue and scheduler.

- `routes/console.php` schedules every periodic task (listed [below](#periodic-jobs)).
- `withoutOverlapping()` without an expiry holds its lock for 24 hours. When a run was killed, every following run was skipped for a day: that's how sign-ins stopped in production, fixed by `withoutOverlapping(1)`. `MarkOfflineNodes` still has the day-long lock.
- `onOneServer()` relies on the cache lock.
- Webhook deliveries are queued after commit, with `webhooks:send-due` every minute as a backstop for lost jobs.
- Metering every 5 s on one server; sign-ins advanced every 5 s.

**The gateway** (`lewbox-gateway-legacy`): goroutine loops, each asking the app over HTTP.

- **Operations runner** (`internal/operations/runner.go`): polls `GET operations` every 1 s per replica (`GATEWAY_OPERATION_POLL_INTERVAL`, a stopgap for a "wake-up call" that was never built). Claims with `PATCH operations/{op}` (`claimed_by`, 60 s lease), renews every 20 s, records each step (which also renews), retries transient failures with backoff 1 s → 30 s until the operation's 30 min timeout, capacity errors for 2 min. Concurrency 64. On shutdown, stops without failing; the lease lapses and another replica resumes from the recorded step. Every step is idempotent (a retried create adopts the AerolVM sandbox tagged with its id).
- **Reconciler** (`internal/reconcile/reconcile.go`): every 60 s, lists the app's running sandboxes and AerolVM's, reports stops AerolVM made on its own (idle timeout, lifetime), with preconditions so duplicate reports from every replica are harmless.
- **Session watcher** (`internal/reconcile/sessions.go`): every 5 s, checks sessions created with `watch`, so `session.ended` reaches webhooks within seconds. Each check counts as AerolVM activity.
- **Event recorder** (`internal/events/recorder.go`): queue of 10,000, flushed every 2 s in batches of 500 to the app; drops events when full.
- **Sweepers** for the key and cluster caches.

Every replica runs the reconciler and session watcher; the app's preconditions make the duplicates harmless but wasteful.

## Goals

- One queue for every kind of background work, in Postgres, with **transactional enqueue**: a job is inserted in the same transaction as the change that needs it.
- Work starts **within milliseconds** of being enqueued, on any replica, without polling every second.
- **No lock that outlives its holder.** Leases expire; leaders are re-elected.
- **Periodic work runs once per period** across all replicas.
- **Crash-safe operations**: a replica dying mid-operation loses at most the current step, which is re-run.
- **Visible**: what's queued, running, retrying and failed, in metrics, logs, the dashboard (for instance admins) and `lewbox admin jobs`.

## Design

### River or our own

Two ways to build the queue.

**[River](https://riverqueue.com)** (`riverqueue/river`, Go, Postgres-native, built on pgx):

- Transactional insert (`InsertTx`), `SKIP LOCKED` claiming, `LISTEN/NOTIFY` wake-ups, retries with backoff, snoozing, unique jobs (by args, by period), scheduled jobs, periodic jobs with leader election, job timeouts, cancellation, queues with their own concurrency, a test helper, and River UI.
- Owns its tables (`river_job`, `river_leader`, `river_queue`, `river_migration`) and migrations, which we run from ours.
- Licence: MPL-2.0. File-level copyleft: we can link it and ship our MIT code; changes to River's own files would be MPL. Acceptable, but confirm.
- **Gap: no heartbeat leases.** A job whose worker died is rescued after `RescueStuckJobsAfter`, a fixed time (default 1 hour), not when a heartbeat stops. An operation that runs 25 minutes (a template build) would need that set above 25 minutes, so a crashed create would wait that long to resume. Legacy resumed within 60 s.

**Our own** (~1,000 lines): a `jobs` table, `SELECT … FOR UPDATE SKIP LOCKED`, heartbeat leases, `NOTIFY`, backoff, a leader lock, periodic schedule. We'd own every line, and would rebuild what River already has and has tested.

**Decided (2026-10-29): River for the queue, plus our own leases on operations.** River's MPL-2.0 licence is fine alongside MIT: its copyleft covers only River's own files, and only if we modify them.

- Operations already have their own rows with domain state (`step`, `steps`, `claimed_by`, `lease_expires_at`, `attempts`), because the API reads them. Keep the heartbeat lease *there*.
- A River job `operation.run{operation_id}` runs an operation: it takes the operation's lease (`UPDATE operations SET claimed_by = $me, lease_expires_at = now() + 60s WHERE id = $1 AND (claimed_by IS NULL OR lease_expires_at < now())`), renews it every 20 s and with every recorded step, and finishes it.
- A periodic job, `operations.rescue`, every 10 s, finds operations that aren't finished and whose lease has lapsed (or that were never claimed for 30 s), and enqueues `operation.run` for each (unique by operation id, so no duplicates). That brings legacy's 60 s resume back, independent of River's rescuer.
- River's own `RescueStuckJobsAfter` is set to `LEWBOX_OPERATION_TIMEOUT` + 5 min (35 min), as a backstop for River's bookkeeping only.
- Every other job kind is short (seconds) and River's own semantics suit it.

If River's licence or behaviour turns out to be a problem, the interface below hides it, and our own queue can replace it.

### The interface

```go
package jobs

// Args is a job's arguments, stored as JSON. Kind() names it.
type Args interface{ Kind() string }

type Worker[A Args] interface {
    Work(ctx context.Context, job *Job[A]) error
}

func Insert(ctx context.Context, tx db.Tx, args Args, opts ...InsertOpt) error   // in a transaction
func InsertNow(ctx context.Context, args Args, opts ...InsertOpt) error           // outside one

// Options: Queue, Priority, ScheduledAt, MaxAttempts, UniqueBy(args|period), Timeout
```

Domain packages register workers in `internal/app`. Workers return:

- `nil`: done.
- `jobs.Snooze(d)`: not ready, try again in `d`, without counting an attempt (e.g. waiting for a node to come online).
- `jobs.Cancel(err)`: give up for good (a permanent error).
- any other error: retry with backoff, up to `MaxAttempts`.

### Queues

| Queue | Concurrency (per process) | Jobs |
|---|---|---|
| `operations` | 64 (`LEWBOX_JOBS_CONCURRENCY`) | `operation.run` |
| `webhooks` | 32 | `webhook.deliver` |
| `default` | 16 | Everything short: email, token refresh, sign-in steps, notifications |
| `maintenance` | 4 | Reconcile, prune, registry GC, partitions, backfills |

Separate queues keep a flood of webhooks from starving operations.

### Retries

- Default: 10 attempts, exponential backoff with jitter, base 1 s, cap 5 min.
- Per kind where it matters: `webhook.deliver` uses the webhook schedule (1 m, 5 m, 30 m, 2 h, 6 h, 12 h, 24 h) and `MaxAttempts = 8`; `email.send` 5 attempts.
- `operation.run` doesn't use River's retries for the operation's own failures: inside, it retries transient failures (unreachable node, toolbox warming up) with its own backoff (1 s → 30 s) until the operation's deadline, and capacity errors for `LEWBOX_CAPACITY_WAIT` (2 m), as legacy did. It returns an error to River only if the process is shutting down (snooze) or something unexpected panics.

### Timeouts and cancellation

- Every job runs with a context: cancelled at its timeout, at shutdown, or when the job is cancelled (River's cancel, or an operation's lease being lost).
- Default timeout 1 minute; `operation.run` has none of its own (the operation's deadline governs); maintenance jobs 10 minutes.
- Losing an operation's lease cancels its context at once. The worker must not write anything after that; every write includes `AND claimed_by = $me`, so a stale worker's writes affect nothing even if it races.

### Wake-ups

`LISTEN/NOTIFY` does two jobs:

1. **Wakes workers.** River notifies on insert; idle workers claim at once. `LEWBOX_JOBS_FALLBACK_POLL` (5 s) catches anything missed (a listener reconnecting).
2. **Fans out live updates** to whichever replica holds a stream. One hub per process, `internal/notify`, holds the single `LISTEN` connection and dispatches to in-process subscribers.

| Channel | Payload | Who listens |
|---|---|---|
| `lewbox_operation` | operation public id, status | Requests waiting on a create or a CLI waiting on an operation; the dashboard's live views |
| `lewbox_sandbox` | sandbox public id, status, sequence | Dashboard streams; caches |
| `lewbox_turn` | message public id, last event number | SSE followers of a turn not on the replica receiving uploads ([agents/turns-and-events](../agents/turns-and-events.md)) |
| `lewbox_session` | session public id | Waiters on a session's end |
| `lewbox_cache` | cache name, key | Invalidates in-memory caches (cluster connections, key lookups) |

Payloads are ids, never data: `NOTIFY` payloads are capped at 8,000 bytes, and subscribers re-read what they need. Notifications are best-effort: every subscriber also has a timeout and re-reads, so a missed notification costs a delay, not correctness.

### Leadership

Periodic work must run once per period, not once per replica.

- River elects a leader (`river_leader`, a row with a TTL, renewed by the leader) and only the leader inserts periodic jobs. The jobs themselves run on any replica's workers.
- Periodic jobs are inserted **unique by period** (e.g. `UniqueOpts{ByPeriod: interval}`), so a leader change mid-period can't insert one twice.
- Long periodic work **fans out**: `sandboxes.reconcile` inserts one `sandboxes.reconcile_cluster{cluster}` per cluster, so clusters are reconciled in parallel across replicas, and one slow cluster doesn't hold up the rest.
- On graceful shutdown the leader resigns, so another takes over immediately.
- If we build our own queue: a leader lock is `pg_try_advisory_lock(<const>)` held on a dedicated connection; it's released automatically when the connection dies.

### Periodic jobs

Every scheduled task and loop from legacy, and what it becomes.

| Job | Interval | Does | Was | Owner |
|---|---|---|---|---|
| `operations.rescue` | 10 s | Enqueue `operation.run` for unclaimed or lapsed operations | Gateway poll, 1 s per replica | [sandboxes/operations](../sandboxes/operations.md) |
| `sandboxes.reconcile` → `reconcile_cluster` | 60 s | Compare running sandboxes with AerolVM; record stops it made (idle timeout, lifetime), lost nodes, wrong nodes | Gateway reconciler, 60 s per replica | [sandboxes/reconciler](../sandboxes/reconciler.md) |
| `sessions.watch` → `watch_sandbox` | 5 s | Check watched sessions; record ends | Gateway session watcher, 5 s per replica | [inside-a-sandbox/sessions](../inside-a-sandbox/sessions.md) |
| `nodes.mark_offline` | 30 s | Nodes without a heartbeat for 3 min → offline; their sandboxes' runs end | `MarkOfflineNodes`, 1 m (day-long lock) | [clusters/health-and-metrics](../clusters/health-and-metrics.md) |
| `nodes.certificate_warnings` | daily | Notify projects of certificates near expiry | `NotifyExpiringCertificates`, daily | [clusters/pki-and-dns](../clusters/pki-and-dns.md) |
| `registry.gc` → `gc_cluster` | hourly | Delete unused snapshots, builds and layers from each cluster's bucket | `CleanUpRegistries`, hourly | [clusters/storage-and-registry](../clusters/storage-and-registry.md) |
| `templates.sync_provided` | at start, hourly | Provided templates defined in code → database; rebuild changed ones | `SyncGlobalTemplates`, hourly | [templates/provided-templates](../templates/provided-templates.md) |
| `sign_ins.sweep` | 30 s | Expire stale sign-ins; start queued ones when there's room. Each sign-in also advances itself with its own snoozing job | `AdvanceSignIns`, 5 s | [profiles/agent-sign-ins](../profiles/agent-sign-ins.md) |
| `sign_ins.refresh_chatgpt` | 5 m | Refresh ChatGPT logins older than 3 days; write them to running sandboxes | `RefreshSubscriptionLogins`, 5 m | [profiles/agent-sign-ins](../profiles/agent-sign-ins.md) |
| `git.refresh_token` | Scheduled per sandbox and account at expiry − 15 m (not periodic) | Mint and write a GitHub installation token into a running sandbox | `RefreshGitHubAppTokens`, 5 m sweep | [git/tokens](../git/tokens.md#refreshing) |
| `github.refresh_user_tokens` | Weekly | Refresh user tokens whose refresh token would lapse unused | none | [git/github-app](../git/github-app.md#the-user-token) |
| `github.prune` | Daily | Prune `github_deliveries` (7 d) and `github_issued_tokens` (30 d) | none | [git/tokens](../git/tokens.md), [github-webhooks](../activity-and-webhooks/github-webhooks.md) |
| `keys.prune_sandbox_keys` | hourly | Delete expired sandbox keys | `model:prune SandboxKey`, hourly | [sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md) |
| `users.prune` | hourly | Expired login codes and sessions | `model:prune Token`, daily | [projects/users-and-login](../projects/users-and-login.md) |
| `idempotency.prune` | hourly | Keys past 24 h | (rows on sandboxes; never pruned) | [api/idempotency](../api/idempotency.md) |
| `activity.prune` | daily | Per-project retention; drop old partitions | `model:prune SandboxEvent`, daily | [activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md) |
| `conversations.prune` | daily | Conversations past retention, if set | `model:prune AgentConversation`, daily | [agents/storage-and-uploads](../agents/storage-and-uploads.md) |
| `webhooks.prune` | daily | Events, deliveries and attempts past 30 days | `model:prune WebhookEvent`, daily | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) |
| `webhooks.disable_failing` | 10 m | Disable endpoints failing for 72 h; notify | inside delivery | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) |
| `nodes.prune_metrics` | daily | Metrics past 14 days; drop partitions | `model:prune NodeMetric`, daily | [clusters/health-and-metrics](../clusters/health-and-metrics.md) |
| `db.partitions` | daily | Create next months' partitions; drop expired ones | | [database](database.md#partitioning) |
| `setup_links.prune` | daily | Expired links | | [profiles/setup-links](../profiles/setup-links.md) |
| `credits.checkpoint_hourly` | 1 h | Charge open runs to the ledger, per project with the `credits` plugin on; replaces the 5-second meter, as the moment credits run out is scheduled (`credits.exhaust`, per profile) rather than polled | `MeterSandboxes`, 5 s, one server | [credits/balances](../credits/balances.md) (Beta, `credits` plugin) |
| `credits.sweep` | 1 m | Backstop: runs whose stop was observed late; exhaustion jobs that should exist | | [credits/balances](../credits/balances.md) (Beta, `credits` plugin) |
| `credits.renew_buckets` | 5 m | Renew subscription buckets that are due | `ResetSubscriptionBuckets`, 5 m | [credits/balances](../credits/balances.md) (Beta, `credits` plugin) |

**Gone:** `webhooks:send-due` (deliveries are jobs, retried by the queue), the gateway's event flush (activity is written in the request's transaction), the key and cluster cache sweepers (caches are bounded and invalidated by `NOTIFY`), `SendSeededHeartbeats` (the development seeder's job).

**Event-driven instead of periodic where possible:**

- **Session ends.** The AerolVM fork could notify on a session's exit, so `sessions.watch` only backstops. Proposed in [clusters/aerolvm-fork](../clusters/aerolvm-fork.md).
- **AerolVM's own stops** (idle, lifetime). The fork could call back too, so the reconciler only backstops. Same.

### Plugins' jobs

Plugins register their own job kinds and periodic jobs ([plugins/extension-points](../plugins/extension-points.md#jobs-and-periodic-jobs)) on this same queue:

- **Kinds are namespaced** by the plugin: `credits.checkpoint_hourly`, `git.github.refresh_installations`. A kind outside its plugin's namespace fails start-up.
- **Jobs carry their plugin and project** in their metadata, so turning a plugin off for a project cancels its pending jobs there, and turning it on runs its `Reconcile` (`plugin.reconcile`, unique per plugin and project) to re-create schedules ([plugins/registry-and-lifecycle](../plugins/registry-and-lifecycle.md#turning-a-plugin-on-or-off-for-a-project)).
- **Workers are always registered**, on or off, so a job never fails as an unknown kind; one whose plugin is off for its scope is discarded when it runs.
- **Periodic jobs** of a plugin off for the instance aren't scheduled; `PerProject` ones run only for projects where the plugin is on.
- **Lifecycle handlers** are delivered as `lifecycle.deliver` jobs, inserted in the transaction of the change ([plugins/extension-points](../plugins/extension-points.md#lifecycle-handlers)).

### Other job kinds

Not periodic; enqueued by what needs them. Non-exhaustive, each owned by its plan:

| Kind | Enqueued when |
|---|---|
| `operation.run` | An operation is created or rescued |
| `webhook.deliver` | An event matches an endpoint |
| `email.send` | A sign-in code, invitation or notification |
| `sign_in.advance` | A sign-in is created; then it snoozes itself between steps |
| `sandbox.apply_setup` | A profile's credentials or Git identity change: one per running sandbox, which inserts an `apply_setup` operation |
| `git.refresh_sandbox_token` | Scheduled per sandbox at token expiry − 15 m |
| `template.build` | Handled as an `operation.run` on the build |
| `conversation.finalize_turn` | agentd's last upload, or a turn interrupted by a stop |
| `backfill.*` | A migration needs data moved |

### Operations and jobs

An *operation* is a domain object the API shows; a *job* is how the work is scheduled. For sandbox and build operations:

```
POST /v0/sandboxes
  tx: insert sandbox (creating), insert operation (pending), insert job operation.run{op}
  commit → NOTIFY → a worker claims the job
worker:
  take the operation's lease (or exit: someone else holds it)
  for each step not yet recorded: do it (idempotently), record it (renews the lease)
  finish: status succeeded|failed, sandbox status, activity, webhooks — one transaction
  → NOTIFY lewbox_operation → the waiting request answers 201
```

Full design in [sandboxes/operations](../sandboxes/operations.md).

## Security

- Job arguments are stored in plaintext JSON in `river_job`. **No secrets in job arguments**: pass ids, and let the worker read and decrypt what it needs. A lint test checks job `Args` types have no `secret.String` fields.
- Errors recorded on jobs are logged and shown to instance admins; they must not contain secrets (errors from AerolVM and git are redacted before they're wrapped).

## Observability

- `lewbox_jobs_inserted_total{kind}`, `lewbox_jobs_completed_total{kind,result}` (succeeded, retried, snoozed, cancelled, discarded), `lewbox_job_duration_seconds{kind}`, `lewbox_jobs_queued{queue,state}` (gauge, sampled), `lewbox_job_queue_latency_seconds{queue}` (scheduled → started).
- `lewbox_leader{replica}` gauge (1 on the leader), `lewbox_leader_changes_total`.
- `lewbox_notify_reconnects_total`, `lewbox_notify_dispatch_total{channel}`.
- `lewbox_operation_lease_lost_total{kind}`, `lewbox_operations_rescued_total{kind}`.
- Every job log line carries `job_id`, `kind`, `attempt`, and the `request_id` of the request that enqueued it, stored in the job's metadata, so a sandbox create can be followed from request to finished operation.
- Trace context is stored in job metadata and continued in the worker.
- Instance admins see the queue in the dashboard (counts by state, failed jobs with errors, retry/cancel); River UI could be mounted for them at `/admin/jobs`.

## Configuration

`LEWBOX_JOBS_CONCURRENCY`, `LEWBOX_JOBS_FALLBACK_POLL`, `LEWBOX_OPERATION_LEASE`, `LEWBOX_OPERATION_RENEW_INTERVAL`, `LEWBOX_OPERATION_TIMEOUT`, `LEWBOX_CAPACITY_WAIT`, `LEWBOX_RECONCILE_INTERVAL`, `LEWBOX_SESSION_WATCH_INTERVAL`, `LEWBOX_CREDITS_CHECKPOINT_INTERVAL`: see [configuration](configuration.md#jobs-and-operations).

## Testing

- River's `rivertest` for asserting a job was inserted with given args, in a transaction.
- Workers are tested directly with a fake clock.
- **Crash test**: start an operation on replica A (a test `serve` with its own worker), kill A mid-step, assert replica B resumes it from the recorded step after the lease lapses, and that A's late writes (if it wakes) change nothing.
- **Leader test**: two replicas; exactly one inserts periodic jobs; kill the leader; the other takes over within its TTL; no period runs twice.
- **NOTIFY test**: a waiter on replica A sees an operation finished by replica B within 100 ms; with notifications dropped, within the fallback poll.
- Every periodic job: idempotent when run twice in one period.

## Build plan

1. `internal/jobs`: River client set-up, migrations, the `Insert`/`Worker` interface, queues, error mapping.
2. `internal/notify`: the `LISTEN` hub with reconnect, subscribers, channels.
3. Leader-only periodic schedule with unique-by-period inserts.
4. Operation leases and `operations.rescue`, with the crash test.
5. Each periodic job lands with its owner's plan.
6. Admin views: `lewbox admin jobs`, the instance admin dashboard page.

## Open questions

1. ~~**River or our own queue?**~~ Decided (2026-10-29): River, with leases on operations ([above](#river-or-our-own)).
2. ~~**Event-driven reconciler and session watcher?**~~ Decided (2026-10-29): yes. They follow the fork's event stream (F35, a P0 for the alpha, [clusters/aerolvm-fork](../clusters/aerolvm-fork.md)); the periodic jobs stay only as backstops.
3. ~~**River UI or our own view?**~~ Decided (2026-10-29): our own small view in the dashboard (counts by state, failed jobs, retry and cancel), behind the dashboard's own sign-in; River UI isn't mounted.
