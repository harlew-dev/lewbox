# Migration: cutover

> Status: Draft · Milestone: Beta · Updated: 2026-10-29

The switch from legacy Lewbox to the new binary on lewbox.com, with Lewagent moving in the same window: what's prepared beforehand, the freeze, the migration, adopting the clusters, the DNS and URL changes, verification, rollback, and what's cleaned up after. Sandboxes keep running throughout; only the API pauses.

## Today

What's running, and the names things are reached by:

| What | Where | Who calls it |
|---|---|---|
| Laravel app: dashboard, Admin API (`/api/v1`), node API (`/api/node/v1`), GitHub App webhooks (`/api/github/v1/apps/{tenant}/webhooks`), `install.sh`, docs | `box.harlew.com` on Laravel Cloud, MySQL | Users, Lewagent's backend (`LEWBOX_ADMIN_URL`), nodes' systemd timers, GitHub |
| Gateway: Consumer API (`/v1`), agent uploads (`/v1/agent-uploads`) | `lewbox-gateway.laravel.cloud` (and `api.lewbox.com` planned, not final) | Lewagent's backend and browsers (`LEWBOX_API_URL`), `lewbox-agentd` in sandboxes (`GATEWAY_PUBLIC_URL`) |
| Nodes | Tenants' servers, AerolVM v0.6.4, `install.sh`'s timers | Called by the gateway with each cluster's PAT |
| Lewbox docs | lewbox.com/docs (served by the app) | Everyone |

Two of these are hard to move:

- **Hostnames we don't control:** `*.laravel.cloud` can't be pointed elsewhere by DNS.
- **URLs baked into running things:** nodes' timers call the app's URL written by `install.sh`; agentd daemons in sandboxes post to `GATEWAY_PUBLIC_URL` written into each conversation's `conversation.json`; GitHub calls the App's registered webhook URL.

## Goals

- Sandboxes keep running; no sandbox is stopped, restarted or lost.
- API downtime under 15 minutes ([open question](../migration.md#open-questions)).
- Every caller ends up on the new server: users, Lewagent, nodes, agentd, GitHub.
- A rollback that's quick before the new server takes writes, and a clear point after which we fix forward.

## Design

### Before the day

| When | What |
|---|---|
| Weeks before | New infrastructure: `lewbox serve` (2+ replicas) behind a load balancer with TLS, Postgres (managed, with point-in-time recovery), on the hostnames in [DNS](#dns). Instance configured: SMTP, public URL, keyring with the imported legacy link key ([data](data.md#secrets)) |
| Weeks before | New SDK releases (TypeScript, PHP) and the CLI published ([sdks-and-cli](../sdks-and-cli.md)); Lewagent's `lewbox-v1` branch passing against staging ([lewagent](lewagent.md)) |
| Rehearsals, repeated | [Rehearsals](#rehearsals) |
| 7 days before | Lower DNS TTLs to 60 s on every hostname that moves. Announce the window to anyone holding keys |
| 7 days before | Install `lewbox node` on every node in **passive** mode ([Nodes](#nodes)) |
| 1 day before | Final rehearsal on that morning's production copy; go/no-go |
| 1 hour before | Freeze changes to legacy code and config; scale the new servers up; open the runbook and a shared log of times |

### Rehearsals

Each rehearsal, on a copy of production:

1. Restore the latest MySQL backup into a scratch instance.
2. Run `lewbox migrate legacy` into a scratch Postgres, timing each table ([data](data.md#the-tool)).
3. Run `--verify`, and fix every difference.
4. Start a scratch `lewbox serve` on it, **with the reconciler, operations and webhooks turned off** (`--read-only`), pointed at the real clusters, and check through the API and dashboard that sandboxes, ports, conversations and nodes read correctly. Read-only mode never calls AerolVM's mutating endpoints.
5. Run Lewagent staging against it read-only.
6. Record times; the migration's total time is the downtime budget's biggest part.

A rehearsal passes with no unexplained differences and a total under the budget. Two passing in a row are needed for go.

### Nodes

Legacy nodes run `install.sh`'s systemd timers (certificate renewal daily, firewall sync every minute, metrics every minute, snapshot cleanup) which call the app's node API with each node's token.

1. **Passive install** (a week before): `lewbox node install --passive --adopt-legacy` puts the binary and a disabled systemd unit on the node, records the legacy config it found (node id, token file, AerolVM paths), and does nothing else. Run over SSH by us, or by the project's operator with a one-liner ([clusters/node-agent](../clusters/node-agent.md)).
2. **During cutover**, legacy timers keep calling `box.harlew.com/api/node/v1/…`. After [DNS](#dns) moves, that hostname reaches the new server, which serves the **legacy node API paths** (`PUT node/certificate`, `GET node/firewall`, `POST node/metrics`) as compatibility routes with the same tokens ([data](data.md#tokens)). Nothing on the node notices.
3. **Adoption** (the days after): `lewbox node adopt` enables the new agent, which enrolls with its existing token, takes over certificates, firewall, metrics and snapshot clean-up, and disables the legacy timers. Done node by node, watching each.
4. **AerolVM stays v0.6.4** through all this. Moving to our fork is its own rollout afterwards ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)).
5. The compatibility node routes are removed once every node is adopted.

### DNS

| Hostname | Before | After |
|---|---|---|
| `api.lewbox.com` | Gateway (if already used) | New server: the API |
| `lewbox.com` (dashboard) or `app.lewbox.com` | (dashboard on `box.harlew.com`) | New server: dashboard ([open question](#open-questions)) |
| `box.harlew.com` | Laravel app | New server: redirects dashboard paths to the new dashboard, serves legacy node API paths and GitHub webhook paths as compatibility routes, redirects `/install.sh` to the new installer |
| `lewbox-gateway.laravel.cloud` | Gateway | Can't move by DNS. The Laravel Cloud gateway app is **replaced with a forwarder** (a tiny reverse proxy, or the new binary's `lewbox forward --to https://api.lewbox.com`) for 30 days, so callers we missed and agentd daemons whose `GATEWAY_PUBLIC_URL` is this host keep working |
| lewbox.com/docs | Laravel app | The new docs site ([sdks-and-cli](../sdks-and-cli.md#docs)) |

The legacy API paths aren't forwarded as-is to new routes (they've changed); the forwarder only matters for paths kept on purpose: `/v1/agent-uploads` (kept for 30 days, [data](data.md#running-sandboxes)) and node routes. Anything else reaching an old host gets a `410 Gone` with a JSON error naming the new API, so a forgotten caller fails clearly.

**GitHub App webhooks:** each project's App's webhook URL is updated through GitHub's API (`PATCH /app/hook/config`, with the App's JWT) to the new path, as part of the cutover script. The old path on `box.harlew.com` stays as a compatibility route for 30 days in case one fails.

### The day

Times are relative to T, when legacy stops taking writes.

| Time | Step |
|---|---|
| T−30 m | Final checks: new servers healthy, backups taken, rehearsal log reviewed, Lewagent's release ready |
| T−5 m | Status page: maintenance starting |
| **T** | **Freeze legacy:** the app and gateway answer every API request with `503 app_unavailable` and `Retry-After: 300` (SDKs back off and retry; the dashboard shows a maintenance page). Node API routes keep working (heartbeats only). GitHub webhooks are queued by GitHub's own retries |
| T+1 m | Stop legacy's scheduler, queue workers and the gateway's loops (operations runner, reconciler, session watcher). Sandboxes keep running on nodes; AerolVM keeps enforcing idle timeouts and lifetimes on its own |
| T+2 m | **Drain:** wait for operations `pending` or `running` to finish (with the runner stopped, these are only ones it was mid-way through; the runbook says how to finish or fail each by hand), cancel in-flight sign-ins and pushes (legacy pushes can't resume on the new server, which doesn't push), send or drop pending webhook deliveries, end open `sandbox_runs` and settle live credit transactions at T |
| T+5 m | Take the final MySQL dump. Record its time |
| T+6 m | Run `lewbox migrate legacy` into production Postgres. Timed in rehearsals |
| T+? | Run `--verify` (counts and checksums; the API comparison against the frozen legacy in read-only) |
| | **Go / no-go 1:** any unexplained difference → [roll back](#rollback) |
| | Start `lewbox serve` in **read-only** mode. Reconciler first pass, read-only, reports differences between the database and AerolVM: sandboxes stopped by timers during the freeze are expected and listed |
| | **Switch:** DNS for `api.lewbox.com`, the dashboard host and `box.harlew.com`; deploy the Laravel Cloud forwarder; update GitHub App webhook URLs; deploy Lewagent's `lewbox-v1` release ([Lewagent](#lewagent)) |
| | **Smoke tests** (scripted, with the CLI and a test profile): create, run, file write and read, expose and link a private port, stop, start, delete; prompt a conversation; receive a webhook; an `lbt_` key works as an admin key; a legacy `lewbox-portd` accepts a new link; a legacy agentd uploads |
| | **Go / no-go 2** |
| | Turn off read-only: reconciler, operations, metering, webhooks and periodic jobs start. Reconciler records the freeze's timer stops as `stopped` with their reasons, and their webhooks go out |
| | Status page: done. Watch dashboards: error rates, job queue, webhook failures, refused requests |

### Lewagent

- Lewagent goes into its own maintenance mode at T (its UI says computers are briefly unavailable; agents in running computers keep working, since agentd runs in the sandbox).
- At the switch, deploy Lewagent's `lewbox-v1` release with `LEWBOX_API_URL=https://api.lewbox.com/v0` and an `lba_` admin key (its `lbt_` key would also work for 90 days).
- Lewagent's smoke tests: open a computer, chat, terminal, Git panel commit and push, screen, artifacts.
- Lewagent's webhook endpoint is already in the migrated project with its secret; deliveries resume when the new server leaves read-only.
- Lewagent's own database needs no migration; ids are kept ([data](data.md#ids)).

### Rollback

| When | How | Data loss |
|---|---|---|
| Before go/no-go 1 | Unfreeze legacy (start the loops, lift the 503s). The new database is discarded | None |
| Between 1 and 2 (new server read-only, DNS may have moved) | Point DNS back, remove the forwarder, revert GitHub webhook URLs, redeploy Lewagent's previous release, unfreeze legacy | None: the new server took no writes |
| After go/no-go 2 (new server taking writes) | **Fix forward.** A reverse migration isn't planned. In an emergency, legacy can be unfrozen with its frozen data, and sandboxes created since are found by the legacy reconciler only if they were made on legacy-known nodes; the runbook covers reconciling them by hand | Writes since go/no-go 2 |

The point of no return is go/no-go 2. Everything before it is undone in minutes.

### After

| When | What |
|---|---|
| Day 1–7 | Adopt nodes with `lewbox node adopt`, a few at a time ([Nodes](#nodes)) |
| Day 1–30 | Legacy app and database **read-only** (the app's dashboard for reference, behind a sign-in, with writes disabled); the MySQL instance kept |
| Day 30 | Remove the forwarder and `/v1/agent-uploads` compatibility (every legacy agentd has been replaced by then, since each is replaced on its sandbox's next prompt or start; the runbook checks none remain); remove GitHub webhook compatibility; drop `legacy_id_map` at day 90 |
| Day 30 | Archive the final MySQL dump, encrypted, for a year; turn legacy off |
| Day 30–90 | **Rotate:** each cluster's PAT, the GitHub Apps' private keys and webhook secrets (where projects agree), the imported port-link key (once no legacy portd remains), and destroy every copy of the legacy `APP_KEY` |
| Day 90 | `lbt_` keys stop working; projects warned at 60 and 80 days, by email and in the dashboard |
| After | Node by node, AerolVM moves to our fork ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)) |

### Read-only mode

`lewbox serve --read-only`, used in rehearsals and during the cutover:

- The API answers reads; writes get `503 app_unavailable` with `Retry-After`.
- Background jobs that change anything (operations, reconciler writes, metering, webhook delivery, token refresh, pruning) don't run; the reconciler can run in report-only mode.
- Calls to AerolVM are limited to reads (get, list, sandbox index, toolbox health).

This is a general feature, useful for maintenance later too ([foundation/binary-and-commands](../foundation/binary-and-commands.md)).

## Observability

- A cutover dashboard: requests by host and status (spotting callers still on old hosts or paths), `410`s by path and key, `lbt_` key use, legacy agent uploads, legacy node API calls, reconciler differences, job queue depth, webhook failures.
- Every compatibility route logs at `info` with the caller's key or node id, so we know who's left before removing it.

## Testing

- The cutover script (freeze, drain, migrate, verify, switch, smoke tests) runs end to end in each rehearsal against scratch copies.
- Smoke tests are the same CLI script used in rehearsals, in CI against the fake cluster too.
- The forwarder and every compatibility route have their own tests, with recorded legacy requests.

## Build plan

1. `--read-only` mode for `lewbox serve`. (Beta)
2. Legacy compatibility routes: node API paths, GitHub webhook path, `/v1/agent-uploads`; `410` for the rest; the forwarder. (Beta)
3. `lewbox node install --passive --adopt-legacy` and `lewbox node adopt`. (Beta)
4. Legacy freeze switch: a Laravel middleware answering `503` (in `lewbox-legacy`, the only legacy change). (Beta)
5. Cutover script and runbook; smoke test script. (Beta)
6. Rehearsals. (Beta)
7. The day; the 90 days after. (Beta)

## Open questions

1. **Dashboard hostname:** `lewbox.com` (with docs at `lewbox.com/docs`), `app.lewbox.com`, or the API host? (Proposed: `app.lewbox.com` for the dashboard, `api.lewbox.com` for the API, both served by the same binary.)
2. **Who runs `lewbox node install --passive` on nodes we don't administer?** If all lewbox.com clusters are Harlew's own today, we do; otherwise we need the projects' operators.
3. **Reverse migration:** worth building as insurance past go/no-go 2? (Proposed: no; rehearsals are the insurance.)
4. **Freeze during low traffic:** which day and hour, given Lewagent's users?
