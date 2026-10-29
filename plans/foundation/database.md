# Database

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

Postgres is Lewbox's only state (D2). This plan covers how we talk to it, migrations, transactions and locking, the conventions every table follows, and the full initial list of tables, each owned by the plan that designs it.

## Today

- **SQLite in development and tests, MySQL in production** (`lewbox-legacy/.env.example`: `DB_CONNECTION=sqlite`). The 82 migrations in `database/migrations/` carry workarounds for both:
  - MySQL has no partial unique indexes, so `ssh_keys` uses a virtual column to make `(profile_id, name)` unique only among keys without a `sandbox_id`, and those indexes are recreated after SQLite table rebuilds.
  - MySQL's 64-character index name limit broke `profile_github_installations` on its first deploy (commit `ac18298`).
  - Sorting agent chunks hit MySQL's sort buffer, so chunks are ordered in PHP.
- **Eloquent models** with `HasPublicId` (`app/Concerns/HasPublicId.php`): an auto-increment `id` plus a unique `public_id`.
- **Profile row locks** serialise limit checks: `ReserveSandbox` locks the profile row (`lockForUpdate`) so two creates can't both pass a limit.
- **Soft deletes** on profiles, templates and cluster installations.
- **Database queue, cache and sessions** (`jobs`, `cache`, `sessions` tables), because there's no Redis.
- **The gateway has no database.** It holds operation leases through the app (`claimed_by`, `lease_expires_at`, preconditions `expected_status`, `expected_aerolvm_id`).

## Goals

- One dialect, and use what it offers: `SKIP LOCKED`, `LISTEN/NOTIFY`, partial and expression indexes, JSONB with GIN, `timestamptz`, advisory locks, declarative partitioning.
- SQL written as SQL, checked at build time, with typed Go results.
- Migrations that run safely while old and new versions of the binary are both serving.
- Every project-owned row carries `project_id`, so isolation is a `WHERE` clause that can't be forgotten.

## Design

### Driver and queries

- **`jackc/pgx/v5`** with `pgxpool`. No `database/sql` layer.
- **`sqlc`** generates typed Go from SQL in `internal/db/queries/*.sql`, into `internal/db/sqlc`. CI runs `sqlc diff` and `sqlc vet` so generated code is never stale and queries are checked against the schema.
- **Dynamic queries** (list filters: status, cluster, `metadata[k]=v`, activity filters) use a small, tested builder in `internal/db/filter`, producing parameterised SQL only. Never string-concatenate values.
- **One `Queries` interface** per transaction. Services take a `db.Querier` so the same code runs inside or outside a transaction.

### Transactions

```go
err := db.Tx(ctx, pool, func(tx db.Tx) error {
    profile, err := tx.LockProfile(ctx, profileID)       // SELECT … FOR UPDATE
    …checks against limits…
    sandbox, err := tx.InsertSandbox(ctx, …)
    op, err := tx.InsertOperation(ctx, …)
    err = jobs.Insert(ctx, tx, operations.RunArgs{Operation: op.ID}) // same transaction
    err = activity.Record(ctx, tx, …)                    // same transaction
    err = webhooks.Enqueue(ctx, tx, events…)             // same transaction
    return nil
})
```

- **Read committed** isolation (Postgres default), with explicit row locks where order matters:
  - `profiles` row: limit checks on create and start (as legacy).
  - `sandboxes` row: one operation at a time (`operation_in_progress`), status transitions.
  - `conversations` row: one running turn, queue order.
- **Serialisation and deadlock retries**: `db.Tx` retries the whole function up to 3 times on `40001` and `40P01`. Functions must be safe to re-run (they are: nothing leaves the transaction until commit).
- **Side effects after commit** (`NOTIFY`, closing a stream) go in `tx.AfterCommit(func())`, run only if the commit succeeds. Postgres sends `NOTIFY` only at commit anyway; `AfterCommit` is for in-process effects.
- **Timeouts**: `statement_timeout` per connection (`LEWBOX_DATABASE_STATEMENT_TIMEOUT`, 30 s); long jobs set their own.

### Migrations

- **Tool:** `pressly/goose/v3` with SQL migration files embedded via `embed.FS`, run by `lewbox migrate` and on `serve` start. (`golang-migrate` is the alternative; goose's Go API, embedding and `status` output are the deciding factors. `atlas` is heavier than we need.)
- **Files:** `internal/db/migrations/NNNNN_description.sql`, numbered sequentially (not timestamps, so merge order is explicit; CI fails on duplicates).
- **Locking:** migrations run under `pg_advisory_lock(<lewbox constant>)`, so concurrent replicas starting at once don't race.
- **Forward-only in production.** `down` exists for development. A bad migration is fixed by a new one.
- **Expand and contract.** A release's migrations must work with the *previous* release's code still running, because replicas roll one at a time ([build-and-release](build-and-release.md#upgrades)):
  - Add columns nullable or with defaults; backfill in a job; add `NOT NULL` in a later release.
  - Rename = add new, write both, read new, drop old over three releases.
  - Never drop a column the previous release reads.
- **Big backfills** are jobs, not migrations, so they don't hold locks.
- **`lewbox migrate verify`** compares the live schema to what the build expects (the embedded migrations' final schema, dumped with `pg_dump --schema-only` at build time). Used in CI and by `/ready`.
- **Schema dump:** `internal/db/schema.sql` is generated after each migration and committed, for sqlc and for review.

### Plugin migrations

Each plugin compiled in ([plugins](../plugins.md)) brings its own migrations ([plugins/extension-points](../plugins/extension-points.md#migrations-and-tables)):

- **Files:** `plugins/<name>/migrations/NNNNN_description.sql`, embedded by the plugin, numbered in their own sequence.
- **Version table per plugin:** `goose_plugin_<name>` (slashes and hyphens as underscores: `goose_plugin_git_github`), so plugins' numbers never collide with each other's or the core's.
- **Order:** the core's migrations first, then each plugin's in plugin order (dependencies first), all under the same advisory lock.
- **Always run,** whether the plugin is on or off for the instance, so turning a plugin on never runs DDL and turning it off never loses data.
- **Ownership:** a plugin declares the tables it owns; a lint test fails a plugin migration touching any other table (beyond foreign keys *to* core tables), and a core migration touching a plugin's table. The core never has a foreign key to a plugin's table, so the core-only build migrates. New plugins prefix their tables with their name (`gitlab_…`); first-party plugins keep the names below and declare them.
- **Expand and contract, forward-only, `verify`** as for the core: `lewbox migrate status` and `verify` cover every plugin's sequence, and `migrate verify` checks each plugin's schema too.
- **A plugin removed from a build** leaves its tables and its version table; start-up warns, naming them. `lewbox migrate drop-plugin NAME` removes them, refusing while any row references a live resource.
- **sqlc:** each plugin has its own `sqlc.yaml` and queries, generated against the full schema dump (core plus its own tables).

### Conventions

**Keys and ids**

- Primary key `id bigint generated always as identity`. Internal only, used for joins and foreign keys; never in the API.
- `public_id text not null unique` where the resource has a public id, e.g. `sbx_Qm4pL0sX2bN8cV` ([ids-and-keys](ids-and-keys.md)). Generated in Go, not in SQL.
- Resources named by a slug (templates, clusters' names) have `name text` unique within their scope, and still have an internal `id`.
- Foreign keys always declared, `ON DELETE` chosen explicitly per relation (`CASCADE` for owned children, `RESTRICT` where deletion must be refused, `SET NULL` where history outlives the parent, e.g. a conversation's `sandbox_id`).

**Scoping**

- Every project-owned table has `project_id bigint not null references projects(id)`, even where it's derivable through a parent (e.g. `sandbox_ports` has `project_id` as well as `sandbox_id`). It costs 8 bytes a row and makes every query's isolation explicit.
- Every sqlc query on a project-owned table takes `project_id` (or a profile id, which implies one). A lint test parses the queries and fails on any `SELECT`/`UPDATE`/`DELETE` of a project-owned table without `project_id` or `profile_id` in its `WHERE`, except an allow-list of system queries (the reconciler, pruning).
- **Row-level security** is not used for the alpha; the lint test and API tests cover isolation. It's a candidate later ([open questions](#open-questions)).

**Types**

- Times: `timestamptz`, stored in UTC, always `created_at` and `updated_at` (set by Go, not triggers). API times are RFC 3339 with milliseconds.
- Enumerations: `text` with a `CHECK (status IN (…))` constraint, not Postgres `ENUM` types, which are awkward to change.
- Money and units: integers. vCPUs in millicores (`cpu_millis int`), memory in MB, disk in GB, credits in micro-units (`bigint`).
- Flexible structured data: `jsonb`: metadata, setup reports, repositories, operation steps, template ports, settings. Anything queried by key gets an index (metadata: `GIN (metadata jsonb_path_ops)` for `metadata @> '{"k":"v"}'`).
- Encrypted values: `bytea` columns named `*_encrypted` ([secrets-and-encryption](secrets-and-encryption.md)).
- Hashed tokens: `token_hash bytea not null unique` (32 bytes SHA-256) and `token_last_four text`.

**Deleting**

- **Default: hard delete.** Most rows are gone when they're deleted.
- **Status, not soft delete, where the API promises history.** Sandboxes stay as `status = 'deleted'` with `deleted_at`, because "a deleted sandbox can still be read". Operations, activity and conversations of a deleted sandbox stay (until retention).
- **Tombstones where names must be reusable.** A deleted template's slug can be reused, so `templates` has `deleted_at` and a partial unique index `UNIQUE (project_id, name) WHERE deleted_at IS NULL`. Same for profiles' `external_id`.
- No global soft-delete scope. Each query says what it wants.

**Concurrency fields**

- `sandboxes.status_sequence bigint`: incremented on every status change, sent in webhooks so consumers order them (kept from legacy).
- `version int` optimistic locking is not used by default; row locks are.

### Partitioning

The largest tables grow without bound until pruned:

| Table | Growth | Pruned by |
|---|---|---|
| `activity_events` | Every command, file write, request refused… legacy seeded 300,000 per project for performance tests | Retention, per project (default 30 days, 3 for successful commands) |
| `node_metrics` | One row per node per minute | 14 days |
| `conversation_chunks` | Every text run and tool call | Conversation retention (default none) |
| `webhook_delivery_attempts` | Every attempt | 30 days |

Pruning by `DELETE` on large tables bloats them and needs vacuum. Postgres declarative partitioning by month (`PARTITION BY RANGE (occurred_at)`) lets pruning drop whole partitions. But per-project retention means a partition can only be dropped once its newest row is past the *longest* retention (90 days max), with per-project `DELETE`s inside it before then.

Recommendation: **partition `node_metrics` and `activity_events` by month from the start** (cheap to do now, painful to add later), with a periodic job that creates partitions ahead and drops old ones. Leave `conversation_chunks` unpartitioned (retention is per conversation, not time). Decide in M0.

### Connection use

- `pgxpool` sized by `LEWBOX_DATABASE_MAX_CONNS` (25).
- One dedicated connection per process for `LISTEN` ([jobs-and-scheduling](jobs-and-scheduling.md#wake-ups)).
- Long-held streams (SSE, WebSockets) must not hold a connection: they read, release, and wait on in-process channels fed by `NOTIFY`.
- Behind PgBouncer in transaction mode, `LISTEN` and advisory session locks break. Document: use a direct connection for the listener (`LEWBOX_DATABASE_LISTEN_URL`, defaulting to `LEWBOX_DATABASE_URL`), or session mode.

## Tables

The initial schema, by area. Each owner plan defines the columns; this list says what exists and who owns it. **Alpha** tables are created in M0–M4 migrations; **Beta** and **Later** ones when their plans are built.

### Platform

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `goose_db_version` | Migration state | [database](database.md) | Alpha |
| `river_job`, `river_leader`, `river_queue`, `river_migration` (if River) | Job queue | [jobs-and-scheduling](jobs-and-scheduling.md) | Alpha |
| `encryption_keys` | Data keys wrapped by the keyring | [secrets-and-encryption](secrets-and-encryption.md) | Alpha |
| `signing_keys` | Ed25519 keys for port links and agent tokens, private part encrypted | [secrets-and-encryption](secrets-and-encryption.md#signing-keys) | Alpha |
| `idempotency_keys` | One table for every idempotent request: `(scope, key)`, request fingerprint, response or resource reference, `expires_at` | [api/idempotency](../api/idempotency.md) | Alpha |
| `rate_limit_counters` | If rate limits are counted in Postgres | [api/rate-limits](../api/rate-limits.md) | Alpha |
| `audit_log` | Instance and project admin actions | [observability](observability.md#audit) | Alpha |
| `goose_plugin_<name>` | Each plugin's migration state | [Plugin migrations](#plugin-migrations) | Alpha |
| `plugin_settings` | Plugins' project and profile settings (schema-validated JSON, secrets encrypted) | [plugins/extension-points](../plugins/extension-points.md#settings) | Alpha |

### Users and projects

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `users` | `usr_`, email, name, `instance_admin` | [projects/users-and-login](../projects/users-and-login.md) | Alpha |
| `login_codes` | Emailed codes, hashed, with attempts | [projects/users-and-login](../projects/users-and-login.md) | Alpha |
| `user_sessions` | Dashboard sessions, token hashed | [projects/users-and-login](../projects/users-and-login.md) | Alpha |
| `projects` | `prj_`, name, slug, `settings` jsonb | [projects](../projects.md) | Alpha |
| `project_members` | user × project, role | [projects/members-and-roles](../projects/members-and-roles.md) | Alpha |
| `project_plugins` | Which plugins a project has turned on or off, where it has chosen | [projects/settings](../projects/settings.md#plugins), [plugins/building-and-enabling](../plugins/building-and-enabling.md#project-enablement) | Alpha |
| `project_invitations` | Pending invitations by email | [projects/members-and-roles](../projects/members-and-roles.md) | Alpha |
| `admin_keys` | `key_`, token `lba_` hashed, permissions, expiry | [projects/admin-keys](../projects/admin-keys.md) | Alpha |
| `notifications` | Project notifications (certificate expiring, webhook endpoint disabled…) | [projects](../projects.md) | Alpha |

### Clusters and nodes

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `clusters` | `clu_`, project, name, runtime, architecture, status, AerolVM fork version, domain | [clusters](../clusters.md) | Alpha |
| `cluster_secrets` | PAT, CA key and certificate, gossip key, credential key (encrypted) | [clusters/pki-and-dns](../clusters/pki-and-dns.md) | Alpha |
| `cluster_dns` | A cluster's DNS: `provider` (a DNS provider's id, e.g. `cloudflare` from `dns/libdns`), domain, credentials shaped by that provider's schema (encrypted) | [clusters/pki-and-dns](../clusters/pki-and-dns.md), [plugins/dns-providers](../plugins/dns-providers.md) | Alpha |
| `cluster_storage` | A cluster's bucket: `provider` (a `storage/*` plugin's id), settings and credentials shaped by its schema (encrypted) | [clusters/storage-and-registry](../clusters/storage-and-registry.md), [plugins/storage-providers](../plugins/storage-providers.md) | Alpha |
| `cluster_installations` | `cin_`, install token hashed and encrypted, status machine | [clusters/installation](../clusters/installation.md) | Alpha |
| `nodes` | `node_`, cluster, status, host facts, node token hashed, versions, `last_seen_at`, certificate expiry | [clusters/node-agent](../clusters/node-agent.md) | Alpha |
| `node_metrics` | One row per node per heartbeat interval (partitioned) | [clusters/health-and-metrics](../clusters/health-and-metrics.md) | Alpha |

### Profiles

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `profiles` | `prf_`, project, name, `external_id`, status, default cluster, Git name and email | [profiles](../profiles.md) | Alpha |
| `profile_keys` | `key_`, token `lbp_` hashed, permissions, cluster access, expiry | [profiles/profile-keys](../profiles/profile-keys.md) | Alpha |
| `profile_key_clusters` | Clusters a restricted key reaches | [profiles/profile-keys](../profiles/profile-keys.md) | Alpha |
| `profile_limits` | One row per limit kind and period. Owned by the `limits` plugin | [limits](../limits.md) | Alpha |
| `profile_credentials` | Agent credentials (encrypted), agent and provider (from the agent plugins' registries), `verified_at`. Core: the agent framework's | [profiles/agent-credentials](../profiles/agent-credentials.md) | Alpha |
| `github_people`, `github_person_installations`, `profile_github_people`, `profile_github_accounts` | GitHub people (identity, user token) and what they reach, the project's; the people and accounts each profile links, up to `LEWBOX_GIT_MAX_CONNECTIONS_PER_PROFILE` | [git/github-app](../git/github-app.md#tables) | Alpha |
| `profile_telemetry` | OTLP export settings (encrypted). Owned by the `telemetry` plugin | [profiles/telemetry](../profiles/telemetry.md) | Alpha |
| `profile_setup_links` | `psl_`, token hashed, sections, return URL, expiry, use | [profiles/setup-links](../profiles/setup-links.md) | Alpha |
| `ssh_keys` | `ssh_`, profile or sandbox, name, fingerprint. Partial unique indexes per owner | [profiles/ssh-keys](../profiles/ssh-keys.md) | Alpha |
| `agent_sign_ins` | `asi_`, status machine, helper sandbox | [profiles/agent-sign-ins](../profiles/agent-sign-ins.md) | Alpha |

### Templates

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `templates` | Slug, project (null for provided), profile (null unless a profile's own), Dockerfile, ports, default size, env (encrypted), hooks, stop grace and scrub paths, `deleted_at` | [templates](../templates.md) | Alpha |
| `template_clusters` | Which clusters a template builds on | [templates](../templates.md) | Alpha |
| `template_builds` | `bld_`, template × cluster, status, image, size, error | [templates/builds](../templates/builds.md) | Alpha |

### Sandboxes

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `sandboxes` | `sbx_`, project, profile, cluster, node, AerolVM id, template build or image, size, timers (and `warn_before`, `idle_stops_at`, `timers_armed`), `persist`, env (encrypted), metadata (GIN), status and `status_sequence`, snapshot, container command, setup report, repositories, hooks (the template's copied at create, and its own; hook env encrypted) and their last runs, stop grace, scrub paths, `deleted_at` | [sandboxes/lifecycle](../sandboxes/lifecycle.md) | Alpha |
| `node_events` | AerolVM lifecycle events received from node agents, `(node, seq)` unique, kept 7 days for deduplication and debugging ([clusters/node-agent](../clusters/node-agent.md#lifecycle-events)) | [sandboxes/reconciler](../sandboxes/reconciler.md) | Alpha |
| `sandbox_snapshots` | One row per snapshot: sandbox, operation, name, node, size, status, `scrub` (the fork's scrub report) and `scrub_method` (`node` or `restart`), superseded and deleted times | [sandboxes/persistence](../sandboxes/persistence.md) | Alpha |
| `operations` | `op_`, subject (sandbox or build), kind, status, step, steps, attempts, lease (`claimed_by`, `lease_expires_at`), error, error code | [sandboxes/operations](../sandboxes/operations.md) | Alpha |
| `sandbox_keys` | `sbk_`, token `lbs_` hashed, sandbox, permissions, made by | [sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md) | Alpha |
| `sandbox_sessions` | `ses_`, AerolVM session id, command, PTY, `watch`, status, exit | [inside-a-sandbox/sessions](../inside-a-sandbox/sessions.md) | Alpha |
| `sandbox_ports` | Port, protocol, access, proxy port, persist, URL, host, host port | [inside-a-sandbox/ports](../inside-a-sandbox/ports.md) | Alpha |
| `sandbox_git_repositories`, `sandbox_git_tokens` | Repositories a sandbox's Git tokens reach; what's written, per account, with expiry (never the token). Access: `sandboxes.git_access` and `sandbox_git_repositories.access`, default `full` | [git/tokens](../git/tokens.md#tables) | Alpha |

### Agents

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `conversations` | `cnv_`, project, profile, sandbox (`SET NULL`), agent, settings, worktree, status, usage | [agents/conversations](../agents/conversations.md) | Alpha |
| `conversation_messages` | `msg_`, prompt and turn, origin, status, sequence, output schema and output | [agents/turns-and-events](../agents/turns-and-events.md) | Alpha |
| `conversation_chunks` | Stored chunks, capped at 64 KB | [agents/storage-and-uploads](../agents/storage-and-uploads.md) | Alpha |
| `conversation_tasks` | Background tasks | [agents/background-tasks](../agents/background-tasks.md) | Alpha |
| `worktrees` | `wtr_`, standalone worktrees | [agents/worktrees](../agents/worktrees.md) | Later |

### Activity and webhooks

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `activity_events` | `evt_`, the activity log (partitioned) | [activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md) | Alpha |
| `webhook_endpoints` | `whk_`, URL, events, secret and previous secret (encrypted), status | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) | Alpha |
| `webhook_events` | `whe_`, the event payload, once | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) | Alpha |
| `webhook_deliveries` | `whd_`, event × endpoint, status | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) | Alpha |
| `webhook_delivery_attempts` | One per attempt, response summary | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) | Alpha |
| `github_deliveries` | Inbound GitHub webhook deliveries, deduplicated by `X-GitHub-Delivery`, with payloads for forwarding (7 days) | [activity-and-webhooks/github-webhooks](../activity-and-webhooks/github-webhooks.md) | Alpha |

### GitHub (the `git/github` plugin)

Owned by the `git/github` plugin, in its own migrations ([plugins/git-providers](../plugins/git-providers.md)); the GitHub rows in the Profiles section above are its too.

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `github_apps` | A project's GitHub App: id, slug, client id, private key, client secret, webhook secret (encrypted) | [git/github-app](../git/github-app.md) | Alpha |
| `github_installations` | The App's installations the project knows, one per GitHub account | [git/github-app](../git/github-app.md#tables) | Alpha |
| `github_issued_tokens` | Tokens issued by the token API (`ght_`): who, for what, until when; the token encrypted only while reusable | [git/tokens](../git/tokens.md#tables) | Alpha |

Legacy's `pushes` table isn't carried over: Lewbox no longer pushes ([git/integrating](../git/integrating.md)).

### Credits (the `credits` plugin)

`sandbox_runs` is core, written by the sandbox lifecycle whether or not credits are on, so usage history exists from the alpha. The rest are owned by the `credits` plugin, in its own migrations, created whenever it's compiled in ([credits](../credits.md)).

| Table | Purpose | Owner | Milestone |
|---|---|---|---|
| `sandbox_runs` | When each sandbox ran, at what size: usage history (core) | [credits/metering](../credits/metering.md) | Alpha |
| `credit_buckets` | `cbk_` | [credits/balances](../credits/balances.md) | Beta |
| `credit_transactions` | `txn_` | [credits/balances](../credits/balances.md) | Beta |
| `credit_transaction_lines` | Per bucket | [credits/balances](../credits/balances.md) | Beta |
| `usage_hourly` | Roll-ups per profile per hour | [credits/budgets-and-usage](../credits/budgets-and-usage.md) | Beta |

### Later

`project_domains` (custom domains), `conversation_keys` (encryption), `sandbox_stats`.

### Gone from legacy

`cache`, `jobs`, `failed_jobs`, `sessions` (Laravel's), `tenant_users` (now `project_members`), `tenant_api_keys` (now `admin_keys`), `credentials` (polymorphic, split into typed tables: `cluster_secrets`, `cluster_dns`, `cluster_storage`, `github_apps`), `tenant_notifications` (now `notifications`).

## Security

- The application connects as a role that owns its schema. No superuser needed (advisory locks, `LISTEN`, partitions all work as owner).
- `sslmode=verify-full` recommended in production; documented.
- Secrets in the database are encrypted ([secrets-and-encryption](secrets-and-encryption.md)); tokens are hashed. A database dump alone yields no usable credential without the keyring.
- Backups are the operator's job; document `pg_dump` and that the keyring must be backed up separately.

## Observability

- pgx tracer: spans per query (name from sqlc's query name), duration histogram `lewbox_db_query_duration_seconds{query}`.
- Pool stats: `lewbox_db_pool_{acquired,idle,total,max}_conns`, `lewbox_db_pool_acquire_duration_seconds`.
- Slow queries (> 500 ms) logged with the query name, not its arguments.
- `lewbox_db_tx_retries_total{reason}`.

## Testing

See [testing](testing.md#postgres). In short: every test package that touches the database gets a fresh database cloned from a migrated template, in milliseconds; tests run in parallel.

- Migrations: up from empty; `verify` passes; each migration applies against a database at the previous version with seed data (catches `NOT NULL` without defaults).
- The scoping lint over `internal/db/queries`.
- Row-lock behaviour: two concurrent creates against a `running_sandboxes = 1` limit; one wins.

## Build plan

1. `internal/db`: pool, `Tx` with retries and `AfterCommit`, error mapping (unique violation → domain error).
2. goose with embedded migrations, `lewbox migrate`, advisory lock, `verify`.
3. sqlc configuration, generation in CI, the scoping lint.
4. The filter builder for list endpoints.
5. First migrations: platform, users and projects.
6. Partition management job, if we partition.

## Open questions

1. Partition `activity_events` and `node_metrics` from the start? (Recommended.)
2. Row-level security as defence in depth for project isolation: later, or never?
3. goose or golang-migrate?
4. Should `user_sessions` and `login_codes` be in Postgres, or signed cookies with no table? (Postgres recommended, so sessions can be listed and revoked.)
