# Migration

> Status: Draft · Milestone: Beta · Updated: 2026-10-29

How we move from legacy Lewbox (the Laravel app, its MySQL database and the Go gateway) to the new single binary: the data, the clusters and the sandboxes running on them, Lewagent (the biggest consumer, on a new API), and the switch of lewbox.com itself. The alpha is new installs only; moving existing data and Lewagent happens in Beta.

## Today

What exists to move:

- **lewbox.com's legacy deployment:** the Laravel app on Laravel Cloud (`box.harlew.com`, Admin API at `/api/v1`) with a **MySQL** database; the gateway (`lewbox-gateway.laravel.cloud`, Consumer API at `/v1`), stateless, behind Caddy; a database queue, cache and sessions.
- **Tenants' clusters:** nodes installed with `install.sh` (`lewbox-legacy/resources/scripts/install.sh`, 1,246 lines), running AerolVM **v0.6.4** (pinned; v0.6.5 has no release assets), systemd timers for certificate renewal, firewall sync, metrics and snapshot cleanup, each node's CNCF registry backed by the tenant's R2 or S3 bucket, and a cluster CA whose key the app holds, encrypted with `APP_KEY`.
- **Sandboxes** on those nodes, running or stopped (snapshots in the registry), with sessions, ports (including `lewbox-portd` proxies inside them), agent conversations (agentd state in `/root/.lewbox/agents/<cnv>` in each sandbox), and the app's records of all of it.
- **Lewagent** (`lewagent-legacy`), calling the legacy Admin and Consumer APIs through the PHP SDK, from the browser with sandbox keys, and receiving webhooks ([migration/lewagent](migration/lewagent.md)). One profile per Lewagent user, created by `ProvisionLewboxProfile`.
- **Secrets** encrypted with Laravel's `APP_KEY` (AES-256-CBC with HMAC-SHA256): sandbox `env`, credentials (cluster PATs, CA keys, DNS and storage keys, GitHub App keys), profile credentials (agent keys, subscription tokens, Git tokens), telemetry settings and headers, webhook secrets, template env.
- **Token hashes:** SHA-256 of the whole token, prefix included (`app/Concerns/IssuesApiTokens.php`), for tenant, profile and sandbox keys, setup links, node and installation tokens.
- **Public ids:** a prefix and 14 mixed-case alphanumerics (`HasPublicId`), the same shape the new ids use ([foundation/ids-and-keys](foundation/ids-and-keys.md)).

## Goals

1. **No lost data,** and nothing a user has to redo: keys keep working, sandboxes keep their ids and files, conversations stay readable and promptable.
2. **No restart of running sandboxes.** The new control plane adopts what's running.
3. **Short, reversible cutover.** The legacy system stays ready to take over again until we're sure.
4. **Lewagent moves deliberately,** on its own schedule, tested against the new API before the switch.
5. **The migration is code, tested:** a `lewbox migrate legacy` command, run against copies of production until it's boring.

### Non-goals

- Running legacy and new control planes against the same clusters at the same time, beyond a short read-only overlap ([cutover](migration/cutover.md)).
- Migrating Laravel's operational tables (`jobs`, `failed_jobs`, `job_batches`, `cache`, `cache_locks`, `sessions`, `password_reset_tokens`).
- Self-hosted legacy installs other than lewbox.com. There are none outside Harlew yet; the tool works for any, and the docs say how.

## Design

| Plan | What it covers |
|---|---|
| [data](migration/data.md) | Every legacy table mapped to the new schema; ids; secrets re-encrypted; tokens; the `lewbox migrate legacy` tool; verification |
| [lewagent](migration/lewagent.md) | Every Lewbox call Lewagent makes today, mapped to the new API; webhooks; the PHP SDK update; the order of changes; testing Lewagent against the new API |
| [cutover](migration/cutover.md) | The day itself: freeze, migrate, adopt clusters, switch DNS, move Lewagent, verify, roll back |

### Sequence

```
Alpha (Dec 2026)          Beta (Q1 2027)
──────────────────────────────────────────────────────────────────────────────────────▶
new installs only │ migrate tool  │ rehearsals on   │ nodes get     │ cutover │ legacy
                  │ + Lewagent on │ prod copies     │ lewbox node   │  day    │ read-only,
                  │ new SDK in CI │ (repeat)        │ (agent only)  │         │ then off
```

1. **Build** the migration tool and Lewagent's move against the new API, in CI, with legacy fixtures ([data](migration/data.md), [lewagent](migration/lewagent.md)).
2. **Rehearse** on copies of production: restore a MySQL dump, run the tool into a fresh Postgres, start `lewbox serve` read-only against a cluster snapshot (or the fake cluster with production's shape), verify, and time it. Repeat until it's under the downtime budget.
3. **Prepare nodes:** install `lewbox node` beside legacy's timers on every node, in a mode that does nothing until the new control plane takes over ([cutover](migration/cutover.md#nodes)). AerolVM stays v0.6.4 until after cutover; moving to our fork is a separate rollout.
4. **Cut over** ([cutover](migration/cutover.md)).
5. **Keep legacy** read-only for 30 days for reference and rollback, then archive the MySQL dump (encrypted) and turn it off.

### What changes for users

| Who | What changes |
|---|---|
| Profile key holders (API, SDK) | The API's paths and fields change. Keys keep working. Old SDK versions stop working at cutover; the new SDKs are released before. |
| Admin key holders (tenant keys) | `lbt_` keys keep working for 90 days as admin keys, then must be replaced with `lba_` keys ([data](migration/data.md#tokens)) |
| Dashboard users | New URL shapes (`/t/{tenant}` redirects to `/p/{project}`), a new UI, the same sign-in by email |
| Profile owners with setup links | Unused links keep working |
| Sandboxes | Nothing visible; port URLs may change only if a node's domain changes, which it doesn't |
| Lewagent users | Nothing, if Lewagent moves cleanly |

## Security

- The MySQL dump and `APP_KEY` are handled only by the migration host, never committed or uploaded elsewhere; the tool reads `APP_KEY` from the environment and holds plaintext secrets only in memory, re-encrypting each with the new keyring immediately ([data](migration/data.md#secrets)).
- After cutover, `APP_KEY` is rotated out of every place it was stored, and legacy's credentials that can be rotated (cluster PATs, GitHub App private key, webhook secrets) are rotated on a schedule within 90 days ([cutover](migration/cutover.md#after)).

## Testing

- The tool has unit tests per table mapping, and an end-to-end test that runs legacy's own migrations and seeders into MySQL in CI (with `lewbox-legacy` checked out), runs `lewbox migrate legacy`, and compares every row through both APIs.
- Lewagent's test suite runs against `lewbox serve --fake-cluster` with the new PHP SDK ([lewagent](migration/lewagent.md#testing)).
- Rehearsals on production copies, with a checklist and timings recorded in the rehearsal log.

## Build plan

1. `lewbox migrate legacy` with the table mappings, secrets and tokens. (Beta)
2. Verification command: `lewbox migrate legacy --verify`. (Beta)
3. Adoption of clusters and running sandboxes by the new control plane. (Beta)
4. Lewagent's move to the new SDK and API, behind a feature flag. (Beta)
5. Rehearsals; cutover runbook; rollback plan. (Beta)
6. Cutover; legacy read-only; turn off. (Beta)

## Open questions

1. **Does lewbox.com have customers other than Lewagent** whose integrations must move? (If not, the API change is ours alone, and cutover is simpler.)
2. **Downtime budget:** how many minutes of API unavailability are acceptable? (Proposed: aim for under 15, with sandboxes running throughout.)
3. **Move nodes to the AerolVM fork before or after cutover?** (Proposed: after, as its own rollout, so we change one thing at a time.)
4. **How long do `lbt_` keys keep working?** (Proposed: 90 days.)
