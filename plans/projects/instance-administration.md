# Instance administration

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

An **instance** is one Lewbox installation: lewbox.com, a company's internal deployment, or someone's homelab. **Instance admins** run it: they decide who may sign up and make projects, and they can see and manage every project, user and cluster. Legacy had no such level: anyone could register and make tenants, and the only way to manage the installation was the database. A self-hosted single binary needs this from day one.

## Today

- Anyone reaching the app can enter an email, get a code, and create tenants (`/tenants/create`, `Tenant\CreateController`).
- No instance admins, no way to suspend a tenant, no list of tenants or users outside the database.
- Instance-wide defaults are environment variables in `config/lewbox.php` (~50 `LEWBOX_*` tunables) and `config/passwordless.php`.
- The internal docs set (`/api/private/docs`) is shown to any signed-in user.

## Goals

- Safe defaults for self-hosting: a fresh instance isn't open to the internet.
- A first-run that gets the first admin signed in without editing the database.
- Policies an instance admin can change in the dashboard, not only by redeploying.
- Instance admins can find, inspect, suspend and recover any project, and disable any user, with every action audited.
- Instance-wide quotas, for hosted plans (Later beyond the basics).

### Non-goals

- Billing projects. lewbox.com bills outside Lewbox for now; the quotas here are the hook billing would turn.
- Multiple instances federated together.

## Design

### Who is an instance admin

Two sources, combined:

1. **Configuration**: `LEWBOX_INSTANCE_ADMINS=alice@acme.dev,bob@acme.dev`. Anyone signing in with a listed email is an instance admin. This can't be removed from the dashboard, so an instance can't lock itself out.
2. **The database**: `users.instance_admin boolean`, set by another instance admin in the dashboard, or by `lewbox users promote <email>` on the command line.

`GET /v0/me` reports `instance_admin: true` for either. Instance admin actions need [re-authentication](users-and-login.md#re-authentication).

### First run

When `lewbox serve` starts with an empty database (after migrations):

- If `LEWBOX_INSTANCE_ADMINS` is set, those emails can sign in and are admins; nothing else is needed.
- Otherwise it logs, once, a **setup link**: `https://{host}/setup/instance/{token}` (a 64-hex token, single use, valid 24 hours, stored hashed), and `lewbox instance setup-link` prints a new one at any time. Opening it asks for an email, sends a code, and makes that user the first instance admin and owner of a first project named after the instance.
- The sign-up policy starts as `invite_only` and project creation as `admins`, so nothing is open until an admin opens it.

### Policies

Instance settings, in the `instance_settings` table (one row), editable at `/v0/instance/settings` and in the dashboard's Instance section. Environment variables seed them on first run and, if set, override them (shown as locked in the dashboard).

| Setting | Values | Default (self-hosted) | lewbox.com | Means |
|---|---|---|---|---|
| `name` | text | `Lewbox` | `Lewbox` | Shown in emails and the sign-in page |
| `sign_up` | `open`, `invite_only`, `allowlist` | `invite_only` | `open` | Who may create an account: anyone; only someone with a pending invitation (to a project or the instance); or emails at `sign_up_domains` |
| `sign_up_domains` | list of domains | `[]` | | For `allowlist` |
| `project_creation` | `anyone`, `admins` | `admins` | `anyone` | Who may create projects |
| `projects_per_user` | integer or null | null | 3 | How many projects a user may own (not member of) |
| `default_project_quotas` | object | none | see [Quotas](#quotas) | Copied to new projects |
| `audit_retention_days` | integer | 365 | 365 | Instance audit log |
| `support_email` | email | | `support@lewbox.com` | Shown in errors such as `project_suspended` |

**Plugins aren't an instance setting here.** Which compiled-in plugins are on for the instance, and how projects get them (`all`, `opt_in`, `opt_out`), is set in configuration only and changed by rolling the replicas ([plugins/building-and-enabling](../plugins/building-and-enabling.md#configuration)); the dashboard's Instance section shows it read-only (`GET /v0/instance/plugins`). A plugin that's off for the instance is off for every project, without deleting their data. Projects turn the plugins they're allowed on and off themselves ([settings](settings.md#plugins)).

An instance admin can also invite people to the instance itself (`POST /v0/instance/invitations { email }`), which lets them sign up on an `invite_only` instance without a project invitation, and, if `project_creation` is `admins`, can grant `can_create_projects` to a user.

### Quotas

Hosted instances need to cap what a project can have; self-hosted ones usually don't. `project_quotas`, one row per project, null meaning unlimited:

| Quota | Caps |
|---|---|
| `clusters` | Clusters in the project |
| `nodes` | Nodes across its clusters |
| `profiles` | Profiles that aren't deleted |
| `admin_keys` | Admin keys that aren't revoked |
| `webhook_endpoints` | Endpoints |
| `members` | Members plus pending invitations |

Over a quota, a create is refused with `429 quota_exceeded`, naming the `quota`, distinct from a profile's `limit_exceeded` ([limits](../limits.md)). Quotas aren't about sandboxes: those are the project's own business, through its profiles' limits, on its own nodes.

### Managing projects

| Method | Path | What |
|---|---|---|
| `GET` | `/v0/instance/projects` | Every project, filtered by `status`, `q` (name, slug, owner email); with member count, clusters, nodes, running sandboxes |
| `GET` | `/v0/instance/projects/{prj}` | One, with its owners and quotas |
| `PATCH` | `/v0/instance/projects/{prj}` | `status` (`active`, `suspended`) and `suspended_reason`; `quotas` |
| `POST` | `/v0/instance/projects/{prj}/owners` | `{ email }`: make a user an owner (recovery) |
| `DELETE` | `/v0/instance/projects/{prj}` | Delete it, with the same checks as an owner's delete, plus `force` to stop and delete its sandboxes first |

Instance admins can also open any project in the dashboard; they act with the `owner` role there, and every action they take in it is audited with actor `instance_admin`, visible to the project's owners. A banner in the dashboard says "You're viewing Acme as an instance admin".

**Suspending a project** refuses all its keys (`403 project_suspended`), stops its running sandboxes through `stop` operations with reason `project_suspended` (persisting ones keep their files, others are deleted), pauses its webhook deliveries (kept, sent on unsuspend if within their retry window) and its sign-ins. Members can still sign in and read. Unsuspending lets keys work again; sandboxes stay stopped.

### Users

| Method | Path | What |
|---|---|---|
| `GET` | `/v0/instance/users` | Every user, filtered by `q`, `status`, `instance_admin` |
| `GET` | `/v0/instance/users/{usr}` | One, with their memberships and sessions |
| `PATCH` | `/v0/instance/users/{usr}` | `status` (`active`, `disabled`), `instance_admin`, `can_create_projects` |
| `DELETE` | `/v0/instance/users/{usr}/sessions` | Sign them out everywhere |

A **disabled** user can't sign in, their sessions end, and sandbox keys made from their sessions are revoked. Their memberships stay, so re-enabling restores them. A disabled sole owner leaves their project ownerless in practice; the dashboard warns instance admins and offers to make someone else owner.

### Instance audit log

The same `audit_events` table as projects ([projects](../projects.md#audit-log)), with `project_id` null for instance-level events: `instance.settings_updated`, `instance_admin.granted`, `instance_admin.revoked`, `user.disabled`, `user.enabled`, `project.suspended`, `project.unsuspended`, `project.owner_added`, `project.quotas_updated`, `project.deleted_by_instance`. Read with `GET /v0/instance/audit-events`.

### Instance health

`GET /v0/instance/status`, for the dashboard's Instance page: version, uptime of each replica (from a heartbeat table), database size and migration version, job queue depth and oldest job by kind, failing webhook endpoints count, clusters by health, nodes by status and version, AerolVM fork versions in use, and whether mail is set up. The same numbers are Prometheus metrics ([foundation/observability](../foundation/observability.md)).

### Command line

Everything an operator needs without the dashboard, with database access:

```
lewbox instance setup-link              print a first-run link
lewbox users list [--admins]
lewbox users promote <email>            make an instance admin
lewbox users demote <email>
lewbox users disable <email>
lewbox users code <email>               print a sign-in code
lewbox projects list
lewbox projects suspend <slug> --reason "…"
lewbox projects add-owner <slug> <email>
```

These are subcommands of the one binary ([foundation/binary-and-commands](../foundation/binary-and-commands.md)), reading the same configuration as `serve`.

## API errors

| Status | Code | When |
|---|---|---|
| `403` | `instance_admin_required` | A non-admin on `/v0/instance` |
| `403` | `sign_up_disabled` | Not used on the API (sign-up refusals answer like any code request); reserved for OAuth sign-up (Later) |
| `403` | `project_creation_disabled` | Creating a project the policy doesn't allow |
| `429` | `quota_exceeded` | Over a project quota; carries `quota: { kind, value }` |

## Security

- Default closed: `invite_only` and admins-only project creation on a fresh self-hosted instance.
- The first-run link is single-use, short-lived, logged once, and replaced by printing another; it's the only unauthenticated way to become an admin.
- Instance admins' actions inside projects are audited where the project can see them.
- `/v0/instance` is never reachable by any key, only sessions of instance admins.
- The internal docs set (if we keep one) is instance-admin only.

## Observability

- Audit events above.
- Metrics: `lewbox_instance_users_total{status}`, `lewbox_instance_projects_total{status}`.

## Configuration

| Variable | | |
|---|---|---|
| `LEWBOX_INSTANCE_NAME` | Seeds `name` | |
| `LEWBOX_INSTANCE_ADMINS` | Comma-separated emails, always admins | |
| `LEWBOX_SIGN_UP` | `open`, `invite_only`, `allowlist`; overrides | |
| `LEWBOX_SIGN_UP_DOMAINS` | For `allowlist` | |
| `LEWBOX_PROJECT_CREATION` | `anyone`, `admins`; overrides | |

## Testing

- First run: empty database, link printed, used once, second use refused, admin made.
- Policies: each `sign_up` and `project_creation` value against new and invited users.
- Suspension: keys refused, sandboxes stopped through operations, webhooks paused and resumed.
- Quotas: each kind refused at its value.
- Every `/v0/instance` route refuses non-admins and every key.

## Build plan

Package `internal/instance` (settings, quotas, status), with users and projects reusing their own packages.

1. `instance_settings`, env seeding and overrides.
2. Instance admin resolution (env plus column) in `/v0/me` and the auth middleware.
3. First-run link and `lewbox instance setup-link`.
4. Policies applied to sign-up and project creation.
5. Projects and users endpoints; suspension.
6. Quotas.
7. Status endpoint; CLI subcommands.

## Open questions

1. **Shared clusters.** If lewbox.com offers its own clusters to projects, instance admins would own those clusters, and projects would choose them. Is that in scope, and when ([README](../README.md#open-questions))?
2. **Impersonation.** Is acting with the owner role inside a project enough for support, or do instance admins need to see the dashboard *as* a specific user?
3. **Hosted plans.** Should quotas carry plan names (free, team) so lewbox.com can switch a project's plan in one call?
