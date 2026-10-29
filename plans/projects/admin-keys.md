# Admin keys

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

An **admin key** lets a project's own servers manage the project through the API: make profiles and their keys, set limits and credentials, read sandboxes and their activity, run templates and webhooks. Lewagent's backend is the typical holder: it makes a profile for each user and hands out profile keys. Admin keys were **tenant keys** (`lbt_`) in legacy; they're now `lba_`, and gain optional permissions and an expiry that's actually used.

## Today

- `tenant_api_keys` (`TenantApiKey`, `database/migrations/2026_10_07_023223_create_tenant_api_keys_table.php`): `name`, `token_hash`, `last_four`, `created_by`, `last_used_at`, `expires_at`.
- Made only in the dashboard, Settings › API Keys (`Tenant\Settings\ApiKeyController`); a tenant key can't make tenant keys.
- Full access to the Admin API (`/api/v1`), rate-limited to 120 requests a minute per key (`lewbox.admin_api.requests_per_minute`).
- Refused by the Consumer API with `401 wrong_key_kind`, and vice versa, by prefix.
- Lewagent uses it (`LEWBOX_ADMIN_KEY`) for provisioning profiles, templates, pushes, pull requests and setup links, and a profile key per user for everything else, so one feature (its Git panel) goes through two auth paths with different rules.

## Goals

- Same shape as profile keys: hashed, shown once, a name, optional expiry, optional permissions, `last_used_at`.
- Reaches the whole project: its management API (`/v0/projects/{prj}/…`), and every sandbox of every profile, inside too ([acting on a profile's sandboxes](#acting-on-a-profiles-sandboxes)).
- Least privilege possible: a key that only provisions profiles shouldn't be able to delete clusters.
- One key for a project's backend, so consumers like Lewagent don't need two keys, and two sets of rules, for one feature.

### Non-goals

- Keys that reach several projects. An integration that manages many projects holds a key per project.
- Personal access tokens for users (see [Open questions](#open-questions)).

## Design

### Format

`lba_` + 64 lowercase hex (32 random bytes). Stored as SHA-256 and the last four characters, as every key ([foundation/ids-and-keys](../foundation/ids-and-keys.md)). The prefix lets secret scanners find it; we register the `lba_`, `lbp_` and `lbs_` patterns with GitHub's secret scanning partner program (Later).

### Data model

All keys share one table, so authentication is one lookup by hash, whatever the kind:

```sql
create table api_keys (
  id              bigint generated always as identity primary key,
  public_id       text not null unique,          -- key_…
  kind            text not null,                 -- admin | profile | sandbox
  project_id      bigint not null references projects(id) on delete cascade,
  profile_id      bigint references profiles(id) on delete cascade,   -- profile and sandbox keys
  sandbox_id      bigint references sandboxes(id) on delete cascade,  -- sandbox keys
  parent_key_id   bigint references api_keys(id) on delete cascade,   -- the key a sandbox key was made by
  name            text,
  token_hash      bytea not null unique,
  last_four       text not null,
  permissions     text[],                        -- null = full access, including permissions added later
  cluster_access  text not null default 'all',   -- all | restricted (profile keys)
  created_by_user bigint references users(id) on delete set null,
  last_used_at    timestamptz,
  expires_at      timestamptz,
  revoked_at      timestamptz,
  created_at      timestamptz not null default now(),
  check ((kind = 'admin') = (profile_id is null))
);
create table api_key_clusters (key_id bigint references api_keys(id) on delete cascade,
                               cluster_id bigint references clusters(id) on delete cascade,
                               primary key (key_id, cluster_id));
```

Profile keys and sandbox keys are planned in [profiles/profile-keys](../profiles/profile-keys.md) and [sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md); they share the table.

### Permissions

An admin key has **full access** (`permissions` null, including permissions added later) or a chosen list, fixed once made. The permission names are the API's one registry, [api/permissions](../api/permissions.md): an admin key may hold any of the **management permissions** (`profiles.write`, `profiles.keys`, `profiles.credentials`, `clusters.write`, `webhooks.write`, …) and any of the **sandbox permissions** (`sandboxes.create`, `sandboxes.start`, `sandboxes.stop`, `sandboxes.delete`, `sandboxes.exec`, `sandboxes.ports`, `sandboxes.agents`, `sandboxes.activity`, `templates.create`, …), the same names a profile key uses. So "this key may run commands" means the same thing on every kind of key.

Reading the project and what it holds (profiles, clusters, sandboxes, templates, webhooks) needs no permission, as reading sandboxes needs none with a profile key.

Lewagent's backend, for example, would hold `profiles.write`, `profiles.keys`, `profiles.credentials`, the sandbox permissions and `templates.create`, but not `clusters.write` or `webhooks.write`.

### What an admin key can't do

- Make, list or revoke admin keys. Only a signed-in owner or admin does that, after [re-authentication](users-and-login.md#re-authentication). A leaked key can't mint itself successors.
- Manage members or invitations.
- Delete the project, change its slug, register or remove the GitHub App.
- Reach another project: anything of another project's answers `404`.

### Acting on a profile's sandboxes

**Admins are admins.** An admin key reaches every sandbox in its project, of every profile, and works inside them directly, with the permissions it holds ([api.md, decision A3](../api.md#decisions)). There is one set of sandbox routes (`/v0/sandboxes/…`), and an admin key uses them as a profile key does:

- **Creating** a sandbox names the `profile` it's for (`422 profile_required` without it). The sandbox belongs to that profile: its limits, its clusters, its credentials and Git connection, its default cluster.
- **Lifecycle, reads and work inside** (commands, files, sessions, ports, agents, activity) take the sandbox's id, as for a profile key. No sandbox key is minted first, and nothing is proxied: the request is checked once, against the admin key's permissions and the sandbox's project.
- **Lists** (`GET /v0/sandboxes`, `/v0/conversations`) cover the whole project; filter with `profile=`.
- **Sandbox keys for browsers.** An admin key may still mint a [sandbox key](../sandboxes/sandbox-keys.md) for any sandbox in the project (`POST /v0/sandboxes/{sbx}/keys`), to hand to a browser, as a profile key does. Its permissions can't exceed the admin key's.

Legacy split Lewagent's Git panel between the tenant key (pushes, pull requests) and the profile key (commands). Now one key does both: it works inside the sandbox directly, and issues the GitHub tokens Lewagent pushes and opens pull requests with from its own servers ([git/tokens](../git/tokens.md)). The activity log shows the admin key as the actor, so a project can tell its backend's work from a user's.

Profiles can't opt out of their project's admin access: the project runs Lewbox for its customers and is responsible for what its keys do. A project that wants a key that can't read its customers' files makes an admin key without `sandboxes.exec` and `sandboxes.agents`.

### Expiry, last used, revoking

- `expires_at` is optional, from an hour to 2 years ahead. An expired key is refused with `401 invalid_key`, and the dashboard shows keys expiring within 14 days; a [notification](settings.md#notifications) is sent 7 days and 1 day before.
- `last_used_at` is written at most once a minute per key (an in-memory throttle per replica, then a single `UPDATE … WHERE last_used_at < now() - interval '1 minute'`).
- Revoking sets `revoked_at`; the key stops working at once on every replica (key lookups are cached for a few seconds at most, invalidated by `NOTIFY` on revoke; see [api/authentication](../api/authentication.md)).
- Rotation is "make a new key, deploy it, revoke the old one"; keys are cheap.

### Rate limits

Each admin key gets 600 requests a minute by default (raised from legacy's 120, since one key now does what Lewagent split over two), set per instance and overridable per project. See [api/rate-limits](../api/rate-limits.md).

## API

| Method | Path | Who | What |
|---|---|---|---|
| `GET` | `/v0/projects/{prj}/admin-keys` | owner, admin | List: `id`, `name`, `permissions`, `last_four`, `created_by`, `last_used_at`, `expires_at`, `created_at` |
| `POST` | `/v0/projects/{prj}/admin-keys` | owner, admin (session, re-authenticated) | `{ name, permissions?, expires_at? }`; answers the key with `token`, once |
| `DELETE` | `/v0/projects/{prj}/admin-keys/{key}` | owner, admin | Revoke, `204` |
| `GET` | `/v0/projects/{prj}/admin-keys/current` | admin key | The calling key itself (handy for "check the key works") |

### Errors

| Status | Code | When |
|---|---|---|
| `401` | `invalid_key` | Malformed, unknown, revoked or expired |
| `401` | `wrong_key_kind` | A profile or sandbox key on the management API |
| `422` | `profile_required` | Creating a sandbox without naming its `profile` |
| `403` | `permission_denied` | Missing a permission; the message names it |
| `403` | `project_suspended` | The project is suspended |
| `404` | `not_found` | Any project but the key's own |

## Security

- A key reaches one project; `/v0/projects/{other}` answers `404`.
- Admin keys are never accepted by browsers' CORS with credentials; they're for servers. The docs say so in bold, as legacy's do.
- Created and revoked keys are in the audit log with who did it.
- Admin keys show in the activity log as actor `admin_key` whenever they act on a sandbox, lifecycle or inside, so a project can tell its backend's work from a user's.
- An admin key can read every customer's files and prompt their agents. Projects should give their backend's key only what it needs; the dashboard's key form says what each sandbox permission exposes.

## Observability

- Audit: `admin_key.created`, `admin_key.revoked`, `admin_key.expired` (written by the pruning job when it passes).
- Metrics: `lewbox_api_requests_total{key_kind="admin"}`.

## Testing

- Each permission: allowed with it, `403` without, full-access keys allowed.
- Admin keys can't reach other projects, admin-key or member endpoints.
- Admin keys work inside every profile's sandboxes in their project (commands, files, sessions, ports, agents), and `404` on another project's.
- Creating a sandbox with an admin key: needs `profile`; counts against that profile's limits; uses its credentials.
- Sandbox keys made by admin keys: scoped to the sandbox, never broader than the admin key, revoked with it.
- Revocation takes effect on a second replica within a second.

## Build plan

In `internal/auth` (lookup, permissions) and `internal/projects` (endpoints).

1. `api_keys` table shared by every kind; the lookup by hash with the short NOTIFY-invalidated cache.
2. Admin keys endpoints, re-authentication on create.
3. Permission constants and checks in every management handler.
4. Admin keys on the sandbox routes: the project-wide sandbox lookup, `profile` on create, `profile=` on lists; sandbox keys minted by admin keys.
5. Expiry notifications.

## Open questions

1. ~~**Permission names.**~~ Decided: admin keys use the API's one registry, the same names as profile keys ([api/permissions](../api/permissions.md)).
2. **Personal tokens.** Should users be able to make tokens tied to themselves (revoked when they leave), for the CLI and scripts, instead of project-wide admin keys?
3. **Cluster reach for admin keys.** Profile keys can be restricted to clusters; is that worth having on admin keys too (e.g. a regional backend)?
4. **One table for all keys.** Proposed for one lookup path. Any reason to keep admin keys separate?
