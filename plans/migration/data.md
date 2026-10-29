# Migration: data

> Status: Draft · Milestone: Beta · Updated: 2026-10-29

How legacy Lewbox's MySQL data becomes the new Postgres data: every table mapped, ids kept, secrets re-encrypted from Laravel's `APP_KEY` to the new keyring, tokens kept working, and the state inside running sandboxes made usable by the new control plane. The tool is `lewbox migrate legacy`.

The new schema belongs to the feature plans. Table names below are this plan's working names; where a feature plan names a table differently, it wins and this mapping follows.

## Today

The legacy schema is in `lewbox-legacy/database/migrations/` (78 migrations, 44 tables). Things that matter for moving it:

- **MySQL in production, SQLite in development and tests,** with workarounds for both: a virtual column `sandbox_id_or_zero` for a partial unique index on `ssh_keys`, `profile_github_installations`' index names shortened to fit MySQL's 64 characters (`ac18298`), chunk sorting done in PHP.
- **Ids:** an auto-increment `id` for joins and a `public_id` (prefix plus 14 mixed-case alphanumerics, from `HasPublicId`) for the APIs. Templates use a user-chosen `public_id` slug instead.
- **Polymorphic tables:** `credentials` (`credentialable`: tenants and clusters), `operations` (`subject`: sandboxes and template builds), `tenant_notifications` (`notifiable`).
- **Encrypted columns,** with Laravel's encrypter under `APP_KEY` (`APP_PREVIOUS_KEYS` if it was ever rotated): `sandboxes.env`, `templates.env`, `credentials.value` (`encrypted:json`), `profile_credentials.secret`, `profile_telemetry.settings` and `headers`, `webhook_endpoints.secret` and `previous_secret`, `cluster_installations.token`, and the cached sandbox keys the app keeps for itself.
- **Hashed tokens:** `token_hash` is `sha256(token)` over the whole token, prefix included (`app/Concerns/IssuesApiTokens.php`, `Node::hashToken`, `ClusterInstallation`, `ProfileSetupLink`), with `last_four` beside it.
- **Money-like values** in millionths of a credit (`credit_transactions.amount`, rates on `tenants`); `sandbox_runs` times as Unix integers.
- **Laravel's own tables:** `jobs`, `job_batches`, `failed_jobs`, `cache`, `cache_locks`, `sessions`, `password_reset_tokens`, and the passwordless login token table.

## Goals

- Every row that means something to a user arrives, with the same public id.
- Every secret arrives encrypted under the new keyring, and is never written anywhere in plaintext.
- Every key, token and link that works before cutover works after it.
- Running sandboxes, their sessions, ports and conversations carry on.
- The tool can run repeatedly (rehearsals) and verify its own result.

## Design

### The tool

```
lewbox migrate legacy \
  --mysql 'user:pass@tcp(host:3306)/lewbox?parseTime=true' \
  --app-key "$LEGACY_APP_KEY" [--app-previous-keys "$LEGACY_APP_PREVIOUS_KEYS"] \
  --gateway-link-key "$LEGACY_GATEWAY_PORT_LINK_KEY" \
  [--only tables…] [--since 2026-10-01] [--dry-run] [--verify]
```

- Reads MySQL with `go-sql-driver/mysql`, writes the new Postgres through the normal `internal/db` layer, in one transaction per table group, in dependency order ([Order](#order)).
- Requires an **empty** new database (fresh migrations applied), or `--resume` with its own progress table (`legacy_migration_progress`), so a failed run can continue.
- Keeps a mapping of legacy integer ids to new rows in `legacy_id_map (table, legacy_id, new_id)`, which later steps and the [verification](#verification) use. It's dropped 90 days after cutover.
- `--dry-run` reads and transforms everything and reports counts and problems without writing.
- Logs counts per table, and every row it skips with why.

### Ids

- **Public ids are kept as they are.** Legacy's `prefix + 14 base62` is the new format ([foundation/ids-and-keys](../foundation/ids-and-keys.md)), so `sbx_…`, `prf_…`, `clu_…`, `op_…`, `ses_…`, `cnv_…`, `msg_…`, `key_…`, `evt_…`, `whk_…`, `whe_…`, `whd_…`, `bld_…`, `asi_…`, `psl_…`, `ssh_…`, `cbk_…`, `ctx_…`, `ntf_…`, `node_…` move unchanged.
- **Renamed prefixes keep their random part:** a tenant `tnt_AbC…` becomes the project `prj_AbC…`, so the dashboard can redirect `/t/tnt_AbC…` to `/p/prj_AbC…` without a lookup.
- **Rows without public ids** (`profile_limits`, `credit_transaction_lines`, `node_metrics`, `sandbox_runs`, pivots) get new internal ids.
- Whatever primary key the new schema uses ([foundation/database](../foundation/database.md)), foreign keys are rewritten through `legacy_id_map`.

### Table mapping

| Legacy table | New table | Notes |
|---|---|---|
| `users` | `users` | `email`, `name`, `created_at`. `password` dropped (passwordless). `email_verified_at` kept |
| `tenants` | `projects` | `tnt_` → `prj_` with the same random part; `name`; activity settings to project settings; credit rates to the `credits` plugin's project settings (kept for Beta; the plugin is enabled for migrated projects that had credits); `git_mode` dropped (GitHub App only; [git](../git.md)); `sign_in_cluster_id` and sign-in limits to the project's agent settings |
| `tenant_users` | `project_members` | `role` (`member`, `admin`) → the new roles ([projects/members-and-roles](../projects/members-and-roles.md)): every legacy member becomes an **owner**, since legacy enforced nothing and "every account is an administrator"; owners can tidy roles afterwards |
| `tenant_api_keys` | `admin_keys` | `name`, `token_hash`, `last_four`, `last_used_at`, `expires_at`; marked `legacy_prefix = 'lbt_'` ([Tokens](#tokens)) |
| `tenant_notifications` | `notifications` | `kind`, `description`, `acknowledged_at`; `notifiable` resolved to the project and the resource |
| `credentials` (`kind` = `cluster_secrets`) | `clusters` secret columns | PAT, gossip key, CA certificate and key, credential key, re-encrypted ([Secrets](#secrets)) |
| `credentials` (`dns`) | `cluster_dns` (or `clusters` columns) | provider, domain, token |
| `credentials` (`object_storage`) | `cluster_storage` | provider, endpoint, bucket, keys |
| `credentials` (`github_app`) | `github_apps` | app id, slug, client id and secret, private key, webhook secret, bot user id; `source: manifest`; permissions and events read from `GET /app` on first start |
| `clusters` | `clusters` | `external_id` → `slug`; `name`, `status`, `architecture`, `runtime`, `gpu_vendor`, `aerolvm_version` (v0.6.4 stays until the [fork rollout](../clusters/aerolvm-fork.md)); connectivity `public` (legacy had no tunnels) |
| `cluster_installations` | `cluster_installations` | Only `pending` and `started` ones within their expiry are worth keeping; finished ones are kept for history; `token` re-encrypted; prefix `lbi_` doesn't apply to legacy tokens, which have none |
| `nodes` | `nodes` | Every column; `token_hash` kept so nodes' existing tokens keep working ([cutover](cutover.md#nodes)) |
| `node_metrics` | `node_metrics` | Last 14 days only (legacy's retention); optional with `--skip-metrics` |
| `profiles` | `profiles` | `external_id`, `name`, `status`, `default_cluster_id`, `git_name`, `git_email`, `deleted_at` (soft-deleted ones kept as deleted) |
| `profile_api_keys` | `profile_keys` | `name`, `token_hash`, `last_four`, `cluster_access`, `permissions` (null = full access), `expires_at`, `last_used_at` |
| `profile_api_key_cluster` | `profile_key_clusters` | |
| `profile_limits` | `profile_limits` | `kind`, `period`, `value`; units unchanged (`vcpus` in millicores) unless [limits](../limits.md) changes them, in which case converted here |
| `profile_credentials` (`agent` set) | `profile_agent_credentials` | `provider`, `secret` re-encrypted, `metadata`, `verified_at` |
| `profile_credentials` (`agent` null, `github_app`) | `github_people` + `profile_github_people` | One person per legacy connection (merged by GitHub user id across the project's profiles), linked to its profile as the default. The identity from `metadata`; the user token if legacy kept one, re-encrypted, else `user_authorized: false` (the person authorizes again to get user tokens; installation tokens work without) |
| `profile_credentials` (`agent` null, `github`: pasted tokens) | — | **Not migrated**: pasted tokens are gone ([git](../git.md)). The migration report lists these profiles; their sandboxes clone only public repositories until the person connects the project's App. Legacy tenants should be told before cutover |
| `profile_github_installations` | `github_installations` + `github_person_installations` + `profile_github_accounts` | One `github_installations` row per (project, `installation_id`), reached by the connection's person and linked to each profile that had it (`via_person_id` that person). Legacy profiles over the new limit keep every account they had (the limit only stops new links); `account`, `account_type`, `repository_selection`, `error`; `account_id` read from GitHub on first start |
| `profile_telemetry` | `profile_telemetry` | `provider`, `settings` and `headers` re-encrypted, `endpoint`, `protocol`, `signals`, `content` |
| `profile_setup_links` | `profile_setup_links` | `token_hash`, `sections`, `return_url`, `expires_at`, `used_at`; unused, unexpired links keep working |
| `ssh_keys` | `ssh_keys` | Profile keys and sandbox keys (`sandbox_id`); the `sandbox_id_or_zero` virtual column is dropped, Postgres uses partial unique indexes |
| `agent_sign_ins` | `agent_sign_ins` | Finished ones for history; in-flight ones are cancelled at freeze ([cutover](cutover.md#the-day)) |
| `templates` | `templates` | `public_id` (slug), `name`, `dockerfile`, `cluster_access`, `ports`, defaults, `env` re-encrypted; `tenant_id` null (global, defined in code) → provided templates, re-synced from code by the new server rather than copied |
| `template_cluster` | `template_clusters` | |
| `template_builds` | `template_builds` | `dockerfile`, `status`, `image` (the registry path stays valid; same buckets), `size_mb`, `error`, `finished_at` |
| `sandboxes` | `sandboxes` | Every column: `aerolvm_id`, `node_id`, `template_build_id`, `image`, size, timers, `env` re-encrypted, `metadata`, `status`, `status_sequence`, `snapshot`, `command` (the resume workaround), `setup`, `repositories`, `git_repositories` (→ `sandbox_git_repositories`, source `setting`; `sandboxes.git_access` set to `"read"`, as legacy's sandboxes were read-only: migrated sandboxes must not gain write access by the new default, `full`), `github_token_expires_at` (dropped), `persist`, `purpose`, idempotency key and fingerprint (only if within 24 h) |
| `operations` | `operations` | Finished ones for history (`kind`, `status`, `step`, `steps`, `error`, `error_code`, times); `claimed_by` and `lease_expires_at` dropped. None may be pending or running at freeze ([cutover](cutover.md#the-day)). Template-build operations re-linked to builds |
| `sandbox_sessions` | `sandbox_sessions` | `aerolvm_id`, `command`, `directory`, `pty`, `watch`, `status`, `exit_code`, `exit_signal`, times; running ones stay running and are reconciled after cutover |
| `sandbox_ports` | `sandbox_ports` | `port`, `protocol`, `access`, `proxy_port`, `persist`, `public_url`, `host`, `host_port` |
| `sandbox_keys` | `sandbox_keys` | Only unexpired ones (they last 15 minutes); in practice usually none |
| `sandbox_events` | `activity_events` | Within the project's retention; `kind`, `app_kind`, actor (user, profile key or tenant key → admin key), `details`, `outcome`, `label`, `request_id`, `status`, `code`, `message`, `stderr`, idempotency, `occurred_at` |
| `sandbox_runs` | `sandbox_runs` | All, for usage history and the Beta `credits` plugin ([credits/metering](../credits/metering.md)); open runs closed at freeze and reopened by the new metering for sandboxes still running |
| `credit_buckets` | `credit_buckets` | `kind`, `allowance`, `interval`, `renews_at`, `settled_balance` |
| `credit_transactions` | `credit_transactions` | `amount` (millionths), `description`, `reference`, `sandbox_run_id`, `settled_at`; live (unsettled) transactions settled at freeze first |
| `credit_transaction_lines` | `credit_transaction_lines` | |
| `pushes` | — | **Not migrated**: Lewbox no longer pushes ([git/integrating](../git/integrating.md)). Exported as JSON lines (`pushes.jsonl`) for Lewagent to import into its own `pushes` table if it wants the history; none may be in progress at freeze |
| `agent_conversations` | `conversations` | `agent`, `agent_session_id`, `directory`, `worktree`, `instructions`, `model`, `mode`, `effort`, `fast`, `on_unanswered`, `title`, `status`, `usage`, `context`, `rate_limit`, `message_sequence`, `last_message_at`, `started_by` |
| `agent_conversation_messages` | `conversation_messages` | Every column, including queued prompts (`sequence` null) and `output_schema`/`output` |
| `agent_conversation_chunks` | `conversation_chunks` | `sequence`, `kind`, `external_id`, `status`, `content` (JSON, up to 64 KB), `size`. The largest table: copied in batches of 10,000 with `COPY` |
| `webhook_endpoints` | `webhook_endpoints` | `url`, `description`, `events`, `enabled`, `disabled_reason`, `secret` and `previous_secret` re-encrypted, `previous_secret_expires_at`, `failing_since` |
| `webhook_events` | `webhook_events` | Last 7 days (for the dashboard's stats and redelivery) |
| `webhook_deliveries` | `webhook_deliveries` | Last 7 days; pending ones are drained before freeze, or migrated as pending and sent by the new server |
| `webhook_delivery_attempts` | `webhook_delivery_attempts` | Last 7 days |
| `jobs`, `job_batches`, `failed_jobs`, `cache`, `cache_locks`, `sessions`, `password_reset_tokens`, passwordless tokens | — | Not migrated. Users sign in again |
| — | `project_plugins` | New. Each migrated project gets the shipped defaults, plus `credits` on when the tenant had credit buckets, so its behaviour doesn't change ([projects/settings](../projects/settings.md#plugins)). Plugin-owned tables (`credit_*`, `profile_limits`, GitHub, telemetry) are written by the importer whether or not their plugin is on |

### Order

1. `users`; `tenants` → `projects`; `tenant_users`; `tenant_api_keys`; project-level `credentials`.
2. `clusters` with their `credentials`; `cluster_installations`; `nodes`; `node_metrics`.
3. `profiles`; `profile_api_keys` and their clusters; `profile_limits`; `profile_credentials`; `profile_github_installations`; `profile_telemetry`; `profile_setup_links`; profile `ssh_keys`; `agent_sign_ins`.
4. `templates`; `template_cluster`; `template_builds`.
5. `sandboxes`; sandbox `ssh_keys`; `sandbox_ports`; `sandbox_sessions`; `sandbox_keys`; `operations`.
6. `sandbox_runs`; `credit_buckets`; `credit_transactions`; `credit_transaction_lines`; `pushes` exported to `pushes.jsonl`.
7. `agent_conversations`; messages; chunks.
8. `sandbox_events`; `tenant_notifications`; webhooks.

Sequences and counters (`status_sequence`, `message_sequence`, `chunk_sequence`) are copied, so the next values continue.

### Secrets

Laravel's encrypter produces `base64(json({iv, value, mac, tag}))`: AES-256-CBC with the key from `APP_KEY` (`base64:` prefix stripped), and `mac = hex(HMAC-SHA256(key, base64(iv) . value))`. The tool:

1. Decodes and checks the MAC in constant time, trying `APP_KEY` then each of `APP_PREVIOUS_KEYS`.
2. Decrypts with AES-256-CBC and removes PKCS#7 padding.
3. Unserialises when the value is PHP-serialised (`s:<n>:"…";`, from `encrypt($v, true)`); `encryptString` and the `encrypted`/`encrypted:json` casts don't serialise, so most values are raw.
4. Re-encrypts immediately with the new keyring's current key ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)), binding the ciphertext to its table, column and row id as associated data.
5. Zeroes the plaintext buffer.

A secret that fails to decrypt stops the table's transaction and is reported with its table, column and public id; nothing is half-migrated.

The legacy gateway's `GATEWAY_PORT_LINK_KEY` (an Ed25519 seed) is imported into the keyring too, as the signing key for port links and agent upload tokens, so `lewbox-portd` proxies and `lewbox-agentd` daemons already running in sandboxes keep accepting what the new server signs ([Running sandboxes](#running-sandboxes)). It's rotated after every legacy proxy and daemon has been replaced.

### Tokens

Tokens are hashed as `sha256(token)`, prefix included, in both legacy and new, so a presented token finds its row whatever its prefix.

| Legacy token | After migration |
|---|---|
| `lbp_` profile keys | Unchanged; work as before |
| `lbs_` sandbox keys | Unchanged; expire within 15 minutes anyway |
| `lbt_` tenant keys | Work as **admin keys** for 90 days: the new auth accepts the `lbt_` prefix as the admin key kind for rows marked `legacy_prefix = 'lbt_'`, logs a deprecation warning, and adds a `Lewbox-Deprecation` response header. The dashboard lists them with a "Replace" prompt. After 90 days the prefix is refused |
| Node tokens (no prefix) | Unchanged; nodes keep using them until `lewbox node` re-enrolls ([cutover](cutover.md#nodes)) |
| Installation tokens | Unchanged for unused, unexpired installations |
| Setup links | Unchanged; unused, unexpired links work |
| Login codes and sessions | Not migrated; users sign in again |

### Running sandboxes

What lives *outside* the database, in sandboxes and on nodes, and how the new control plane takes it over:

- **AerolVM sandboxes** are found by their `aerolvm_id` and node, as legacy recorded them, and by AerolVM tags `lewbox_profile` and `lewbox_sandbox`, which the new reconciler reads too ([sandboxes/reconciler](../sandboxes/reconciler.md)). The new reconciler's first pass after cutover compares every running sandbox with AerolVM, as legacy's did every 60 s.
- **The cluster PATs** are migrated, so the new server reaches AerolVM exactly as the gateway did.
- **Snapshots** of stopped sandboxes stay in the registries and buckets; `sandboxes.snapshot` names them, and `command` keeps the nested-toolbox fix working on resume.
- **Sessions:** toolboxd sessions keep running; the new server reads them by name (`ses_…`), and reads the gVisor exit-status files under `/root/.lewbox/sessions/<ses_id>` the same way the gateway did.
- **Private ports:** legacy `lewbox-portd` binaries run in sandboxes as sessions `lewbox-portd-<port>`, verifying links with the legacy Ed25519 public key. With the key imported, new links verify. On a sandbox's next start, the new `lewbox portd` replaces them.
- **Agents:** legacy `lewbox-agentd` daemons in sandboxes hold conversation state in `/root/.lewbox/agents/<cnv>/` (`conversation.json`, `events.jsonl`). The new agentd must read that layout ([agents/agentd](../agents/agentd.md)); the build-version handshake makes an idle legacy daemon answer `upgrade` and stop, and the new one starts. A busy legacy daemon finishes its turn first.
  - **Uploads:** legacy daemons post to `<GATEWAY_PUBLIC_URL>/v1/agent-uploads` with tokens signed by the imported key. The new server serves that exact path (as a compatibility route) for 30 days, and `api.lewbox.com` (or whatever host `GATEWAY_PUBLIC_URL` was) points at it after cutover ([cutover](cutover.md#dns)).
- **Git tokens** in running sandboxes (GitHub App installation tokens, refreshed every 5 minutes by legacy) are refreshed by the new server's job from its first run; a gap of a few minutes is within their hour-long lifetime.
- **Profile setup** files in sandboxes (`/etc/profile.d` env file, agent credentials) stay; the new server re-applies setup only when credentials change, as legacy did.

### Verification

`lewbox migrate legacy --verify` compares the two systems after a run:

- **Counts** per table (within retention windows), and per project and profile.
- **Checksums** of each migrated column over public ids, with secrets compared after decrypting both sides in memory.
- **Through the APIs:** for a sample of profiles, list sandboxes, conversations, templates and activity through the legacy Admin and Consumer APIs and through the new API, and compare field by field after mapping names. Uses a legacy tenant key and the same key as an admin key on the new side, which also proves token migration.
- **Keys:** for each key kind, a known test key made before the run authenticates after it.
- **Secrets:** a canary secret per kind, made before the run, decrypts to the expected value.
- The report lists every difference; a rehearsal passes only with none, apart from documented ones (dropped tables, renamed fields).

## Security

- The tool runs on a trusted host with network access to both databases; `APP_KEY` and the gateway link key come from the environment, never flags in shell history (`--app-key-file` too).
- Plaintext secrets exist only in memory, per row.
- The rehearsal databases are production data: encrypted disks, deleted after each rehearsal, access limited to the people running it.
- `legacy_id_map` holds no secrets; it's dropped after 90 days.

## Testing

- **Fixtures:** a MySQL database built by running `lewbox-legacy`'s own migrations and seeders (`php artisan migrate --seed` in a CI job with PHP 8.4), plus extra seeders for every table and edge case: soft-deleted profiles, null permissions, encrypted values made with `encrypt` and `encryptString`, a rotated `APP_KEY`, chunks at 64 KB, queued messages, a sandbox with a gVisor exit-status file.
- **Per mapping:** unit tests from fixture rows to expected new rows.
- **End to end:** run the tool, then `--verify`, in CI.
- **Laravel encrypter compatibility:** test vectors made by PHP's own `Crypt` in CI, decrypted by the Go code.
- **Rehearsals** on production copies ([cutover](cutover.md#rehearsals)).

## Build plan

1. `internal/legacymigrate`: MySQL reader, id map, progress table, ordered runner, dry run. (Beta)
2. Laravel decrypter and re-encryption; test vectors. (Beta)
3. Mappings for every table, with tests. (Beta)
4. Token compatibility in `internal/auth` (`lbt_` as admin, deprecation header). (Beta)
5. Compatibility route `/v1/agent-uploads` and imported link key. (Beta)
6. `--verify`. (Beta)
7. CI job building legacy fixtures. (Beta)

## Open questions

1. **Keep all activity, or only problems,** beyond 7 days? Activity is the biggest table after chunks; within retention it's at most 30 days.
2. **Chunks:** copy all conversation history (it can be large), or only the last N months? (Proposed: all; conversations are kept until deleted, as the docs promise.)
3. **Global templates** (`tenant_id` null): re-sync from code rather than copy, so their Dockerfiles match the new release? (Proposed: yes; keep their builds' records so existing sandboxes still reference them.)
4. **Roles:** every legacy member as owner, or admins as owners and members as members? Legacy didn't enforce either.
