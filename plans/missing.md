# Missing: what the old design had that this app doesn't

**Written on 2026-11-13.** This compares the archived rebuild plans (`~/dev/Archive/lewbox/plans/`, "the old design") with what is built here, checked against the code: the service methods, routes, CLI commands, web pages and migrations. It is a backlog to choose from, not a commitment. Nothing here is scheduled.

Left out on purpose:
- agent credentials, agent sign-ins and the rest of the "agents" area;
- taking snapshots and running sandbox actions from the UI;
- DNS profiles and S3 snapshot storage, which get a plan of their own.

**Fit** says whether the feature suits a thin wrapper around stock AerolVM:
- **fits:** it can be built on what AerolVM and Lewbox already have;
- **maybe:** possible, but it needs a product decision first, or more scope than it looks;
- **out:** it needs a forked AerolVM, an agent inside sandboxes, or Lewbox in the sandbox request path (D6).

Archive paths are relative to its `plans/` folder.

## Accounts and sign-in

| Feature | Old design | Here | Fit |
|---|---|---|---|
| Email: verification, password reset, notification mail | `projects/users-and-login.md` | Not built (D4); STATUS next step 1 | fits: needs SMTP settings |
| Invitations by email or link, an accept page, pending invitations | `projects/members-and-roles.md` | Not built (D18); invite links are the next step | fits: links work without SMTP |
| List your sessions, revoke one, sign out everywhere | `projects/users-and-login.md` | Only logout, and an admin disabling a user | fits |
| Asking for the password again before dangerous actions | `projects/users-and-login.md` | Not built | fits |
| Sign-up allowlist by email domain | `projects/instance-administration.md#policies` | Sign-up is open, approval or closed, plus setup links | fits: small |
| OAuth, OIDC, passkeys, 2FA | `projects/users-and-login.md#later` ("Later" there too) | Not built | maybe |

## Workspaces and instance administration

| Feature | Old design | Here | Fit |
|---|---|---|---|
| Audit log for a workspace and the instance: keys made and revoked, roles changed, suspensions, settings | `projects.md#audit-log`, `projects/instance-administration.md` | Not built | fits. AerolVM's audit export could add sandbox-level records (REVIEW §2) |
| Notifications (bell, inbox, email): node offline, cluster degraded, certificate expiring, outdated AerolVM, key expiring | `projects/settings.md#notifications`, `clusters/health-and-metrics.md`, `dashboard/navigation-and-layout.md` | Only a certificate warning on the cluster page | fits: the health data is already collected |
| Deleting a workspace; transferring ownership; an instance admin adding an owner to a stranded workspace | `projects.md`, `projects/members-and-roles.md#ownership` | Not built: no `DeleteOrganization` method or route | fits |
| Caps on members and keys per workspace | `projects/instance-administration.md#quotas` | Only "10 workspaces per user" | maybe |
| "Needs attention" panel, command palette, live updates (SSE) | `dashboard/project-pages.md`, `dashboard/navigation-and-layout.md` | Not built | fits: UI only |

## Keys

| Feature | Old design | Here | Fit |
|---|---|---|---|
| Read-only sandbox keys | `profiles/profile-keys.md` | Sandbox keys are always AerolVM `runner` tokens | fits: AerolVM has a `viewer` role |
| Per-key permissions on management (`lba_`) keys | `projects/admin-keys.md` | `lba_` keys have full access | maybe |

## Limits, usage and money

| Feature | Old design | Here | Fit |
|---|---|---|---|
| Exact metering from start and stop events | `credits/metering.md` | 15 s polling (D9); STATUS next step 4 | fits: through AerolVM's audit export |
| Usage CSV export; usage per sandbox | `credits/budgets-and-usage.md` | Not built | fits: small |
| Credits, prices, budgets, top-ups, suspension at zero | `credits.md`, `credits/*` | Dropped (D24); the design is kept in `05-billing.md` | maybe: hourly usage is already metered |
| Per-sandbox caps (vCPU, memory, disk), idle and lifetime caps, `storage_gb` | `limits.md` | Not built (D5): AerolVM team quotas cover count, CPU and memory | out per customer; a region-wide cap fits through AerolVM's `SB_CREATE_POLICY_FILE` |
| Sub-tenants in a workspace (profiles for a customer's end users, with limits) | `profiles.md` | Not built (D1, D29) | out: AerolVM only caps teams |

## Clusters and fleet

| Feature | Old design | Here | Fit |
|---|---|---|---|
| Rolling upgrade of a whole cluster | `clusters/node-agent.md` | Planned (`lewbox cluster upgrade`); `update --aerolvm` refuses on clusters with more than one node | fits: AerolVM has `/v1/cluster/upgrade` |
| Cordon and drain a node; mark a cluster suspended or draining | `clusters/placement-and-capacity.md#cluster-status` | Drain only happens inside node removal | fits: AerolVM has drain, cordon, uncordon |
| Prometheus `/metrics` on the orchestrator | `clusters/health-and-metrics.md#prometheus`, `foundation/observability.md` | Not built | fits |
| Instance health page: version, database size, failing jobs | `projects/instance-administration.md#instance-health` | Partly: `/v0/health`, `serve status` | fits |
| Capacity view, refusing sizes no node can fit, overcommit per cluster | `clusters/placement-and-capacity.md` | The console shows vCPU allocated | maybe for the view; out for a capacity queue (needs the request path) |
| Node agent with host CPU, disk and network metrics; per-sandbox stats | `clusters/node-agent.md`, `clusters/health-and-metrics.md#sandbox-stats` | Not built (D11) | out for sandbox stats (fork); `node_exporter` is a maybe |
| Choosing gVisor or Firecracker per sandbox, tunnel and NAT connectivity, Lewbox as the cluster CA | `clusters/runtimes.md`, `clusters/connectivity.md`, `clusters/pki-and-dns.md` | Not built | out: fork work, and AerolVM owns its PKI |

## Developer surface

| Feature | Old design | Here | Fit |
|---|---|---|---|
| Templates: a Dockerfile built into an image in each region, build logs, a catalog | `templates.md`, `templates/*` | Not built | maybe: the biggest gap against E2B. Wrapping AerolVM's `POST /v1/images/build` per region fits; build contexts and logs need the fork (REVIEW §5.3) |
| Outbound webhooks, signed and retried | `activity-and-webhooks/webhooks.md` | Not built (STATUS, AGENTS) | maybe: events could come from the mirror's diffs or the audit export |
| An OpenAPI file for the `/v0` management API | `api/openapi-and-codegen.md` | None | fits |
| Per-sandbox activity log (commands, files, ports) | `activity-and-webhooks/activity-log.md` | Not built | out (needs the request path); showing AerolVM's audit records is a maybe |
| Lewbox-proxied sandbox API: create, exec, files, sessions, ports, private ports, SSH, operations, idempotency, hooks, browser keys | `inside-a-sandbox/*`, `sandboxes/*`, `api/idempotency.md`, `api/streaming.md` | Customers call AerolVM directly (D6) | out |
| Lewbox SDKs (TypeScript, PHP, Go), an MCP server, a customer CLI with `sandbox create`, `exec`, `shell` | `sdks-and-cli/*` | Customers use the AerolVM and E2B SDKs; `lewbox sandbox` has list, stop, delete, usage, limits | out |
| Git and the GitHub App | `git.md` | Not built | out: it writes tokens into sandboxes |
| Plugin system | `plugins.md` | Not built | out: not needed at this size |
| Postgres, several orchestrator replicas | `foundation/database.md` | SQLite (D2) | out |
| Migrating legacy data | `migration.md` | n/a | out |

## Here, but not in the old design

- An operator-assigned plan per region, pushed as a native AerolVM team quota (D20, D29, D31), with approve and suspend.
- Setup links that carry a plan and a region, for closed instances (D33).
- Machines set up on stock AerolVM: `install`, `node add`, `update`, `firewall enable`, with a pinned installer hash and secret masking.
- 30-day uptime bars and a watch on node certificate expiry (D22), with no node agent.
- The E2B SDK supported from the start (the old design had it as "Later").
- A fleet-wide sandbox list, and snapshot listing through short-lived team tokens (D23).
- Two documentation books embedded at `/v0/docs` and `/v0/platform`.
- Byte-identical CLI and API JSON, enforced by parity tests.
- Keys revoked when a member leaves (D30).
- If Lewbox is down, sandboxes keep working: the reverse of the old proxy design.
