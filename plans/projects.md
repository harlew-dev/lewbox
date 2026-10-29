# Projects

> Status: Draft · Milestone: Alpha (M1) · Core · Updated: 2026-10-29

A **project** is what a team runs Lewbox as: its clusters, its profiles, its templates, its webhooks, its settings, and the people who manage them. One Lewbox instance hosts many projects, each walled off from the others. Projects were called **tenants** in legacy ([D5](README.md#decisions)); the model is the same, and this plan is where it gets real roles, invitations, an audit log and settings of its own.

```
Instance (lewbox.com, or a homelab)
 ├─ Instance admins            set in configuration
 ├─ Users                      sign in by emailed code
 └─ Projects (prj_)
     ├─ Members                users, each with a role: owner, admin, member, viewer
     ├─ Admin keys (lba_)      for the project's own servers
     ├─ Settings               branding, retention, sign-ins, Git, defaults
     ├─ Clusters (clu_)  ──────▶ Nodes (node_)                    see clusters.md
     ├─ Profiles (prf_)  ──────▶ Profile keys (lbp_), limits,      see profiles.md
     │                          credentials, sandboxes (sbx_)
     ├─ Templates                                                  see templates.md
     └─ Webhooks (whk_)                                            see activity-and-webhooks.md
```

## Today

Legacy (`lewbox-legacy`) has tenants:

- `tenants` (`app/Models/Tenant.php`, `tnt_`): a name, plus settings added as columns over time: credit rates (`vcpu_hour_rate`, `memory_gib_hour_rate`, `disk_gb_hour_rate`, `free_disk_gb`, `allow_multiple_subscription_buckets`), sign-ins (`sign_in_cluster_id`, `max_sign_in_sandboxes`), activity (`activity_retention_days`, `succeeded_command_retention_days`, `record_command_stderr`) and `git_mode`.
- `tenant_users` (`TenantUser`): a pivot with `role` `member` or `admin`, but **roles aren't enforced**: every member can do everything. There are **no invitations**: a second person can only join by being added in the database.
- `tenant_api_keys` (`TenantApiKey`, `lbt_`): hashed, made only in the dashboard (Settings › API Keys). A tenant key can't make tenant keys.
- `tenant_notifications` (`TenantNotification`, `ntf_`): `certificate_expiring` and `webhook_endpoint_disabled`, stored but **never shown**.
- Users sign in by an emailed 6-digit code (`harrisonclewis/laravel-passwordless`, `config/passwordless.php`), and **anyone may register and create tenants** (`/tenants/create`). There's no notion of an instance admin.
- The dashboard is scoped by URL, `/t/{tenant}/…`, with a picker at `/tenants/select`.
- `credentials` (polymorphic, `encrypted:json`) holds the tenant's GitHub App, beside cluster secrets.

What's wrong with it:

1. Roles that don't do anything, and no way to invite anyone.
2. No control over who may sign up or make tenants, which a self-hosted instance needs.
3. Settings scattered over columns with no shape: a new setting is a migration on `tenants`.
4. Notifications that nobody sees.
5. No record of who did what outside a sandbox (key made, member removed, profile suspended).

## Goals

- Rename tenant to project everywhere: in the database, the API, the dashboard and the docs.
- Real roles, enforced in one place, with invitations by email.
- An instance level: who can sign up, who can create projects, and instance admins who can see and manage every project.
- One audit log per project, for everything done to the project that isn't inside a sandbox.
- Notifications shown in the dashboard and sent by email.
- Settings grouped by area, each with its own plan, editable in the dashboard and the API.

### Non-goals

- Nested projects or organizations above projects. A team that wants several environments makes several projects, or several profiles in one.
- Billing projects for money. An instance (e.g. lewbox.com) bills its projects outside Lewbox, for now.
- SSO, SCIM and directory sync (Later, see [projects/users-and-login](projects/users-and-login.md#later)).

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Multi-tenancy, with everything scoped by project. Passwordless sign-in by emailed code. Hashed keys shown once. A project's clusters and profiles belong to it alone. |
| **Change** | Tenant → project, `lbt_` → `lba_`. Roles become real (owner, admin, member, viewer) and invitations exist. Settings move out of loose columns into grouped settings ([projects/settings](projects/settings.md)). Notifications are shown and emailed. Admin keys get optional permissions, like profile keys; they're still made only by a signed-in owner or admin, never by another key (see [projects/admin-keys](projects/admin-keys.md)). |
| **Drop** | `password` column and password-reset tables Laravel made but never used. The tenant picker page as its own step: the dashboard opens the last project used. |
| **Add** | Instance admins, signup and project-creation policies, invitations, an audit log, project slugs, branding for setup pages, project deletion. |

## Design

### What a project owns

Every row that belongs to a project carries `project_id`, directly or through its parent, and every query a key or session makes is scoped by it ([api/authentication](api/authentication.md)). Deleting a project deletes everything it owns (see [Deleting a project](#deleting-a-project)).

| Owned | Plan |
|---|---|
| Members and invitations | [projects/members-and-roles](projects/members-and-roles.md) |
| Admin keys | [projects/admin-keys](projects/admin-keys.md) |
| Settings, branding, notifications | [projects/settings](projects/settings.md) |
| Audit log | [Audit log](#audit-log), below |
| Clusters and nodes | [clusters](clusters.md) |
| Profiles, and through them sandboxes, keys, credentials, conversations | [profiles](profiles.md), [sandboxes](sandboxes.md) |
| Templates of its own, and builds | [templates](templates.md) |
| Webhook endpoints, events and deliveries | [activity-and-webhooks](activity-and-webhooks.md) |
| GitHub App (the `git/github` plugin) | [git/github-app](git/github-app.md) |
| Which plugins it uses, within what the instance allows | [projects/settings](projects/settings.md#plugins), [plugins](plugins.md) |

### Data model

```sql
create table projects (
  id              bigint generated always as identity primary key,
  public_id       text not null unique,          -- prj_…
  slug            text not null unique,          -- e.g. "acme", used in dashboard URLs
  name            text not null,
  status          text not null default 'active', -- active | suspended
  suspended_reason text,
  created_by      bigint references users(id) on delete set null,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now(),
  deleted_at      timestamptz                    -- set while deletion runs; row removed after
);
```

Settings live in their own tables, one per area ([projects/settings](projects/settings.md)), rather than as columns here.

### Statuses

| Status | Means | Who sets it |
|---|---|---|
| `active` | Normal | |
| `suspended` | Every key of the project (admin, profile, sandbox) is refused with `403 project_suspended`; its running sandboxes are stopped (persisting ones keep their files); members can still sign in and read, but not change anything | An instance admin ([projects/instance-administration](projects/instance-administration.md)) |
| *deleting* | `deleted_at` set. Keys refused with `401 invalid_key`; the dashboard shows the project as being deleted | An owner |

A suspended project is a lever for the instance (non-payment, abuse); a suspended **profile** is a lever for the project ([profiles](profiles.md#statuses)). Both stop sandboxes the same way, through `stop` operations with reason `project_suspended` or `profile_suspended`.

### Slugs

A project has a `slug`, lowercase letters, digits and hyphens, 3 to 40 characters, unique on the instance, chosen at create (suggested from the name) and changeable by an owner. The dashboard's URLs use it, `/p/acme/sandboxes`; the API uses the `prj_` id, and also accepts the slug wherever it takes a project, as clusters accept their name. Changing a slug keeps the old one as a redirect for the dashboard for 30 days, and never for the API.

### Creating a project

`POST /v0/projects` with `name` and optional `slug`, by a signed-in user the instance lets create projects ([instance policy](projects/instance-administration.md#policies)). In one transaction it:

1. Inserts the project, and the user as its `owner`.
2. Inserts default settings for each area.
3. Writes `project.created` to the audit log.

A new project has no clusters. The dashboard's empty state walks through adding one ([clusters/installation](clusters/installation.md)), or, on an instance with shared clusters, choosing one ([open question](README.md#open-questions)).

### Deleting a project

Deleting is an owner's, and is checked hard, because nodes and buckets outlive it:

1. Refused with `409 project_not_empty` while the project has sandboxes that aren't `deleted`. The owner deletes them first (the dashboard offers "Delete all sandboxes", as operations).
2. Refused while it has clusters with nodes online, with `409 project_not_empty`, naming them. Removing nodes is a [cluster](clusters.md) action, which uninstalls the node agent and AerolVM.
3. Then `deleted_at` is set, every key stops working at once, and a `delete_project` job removes the rest in batches: profiles and their keys, credentials, conversations, templates and their builds (and their images in the bucket), webhooks, members, settings. The audit log is kept for the instance's retention, keyed by the project's public id, so an instance admin can see what happened.

The owner confirms by typing the project's slug.

### Switching projects

A user can belong to many projects. The dashboard remembers the last one (`users.last_project_id`) and opens it; the project menu lists the others. There's no separate picker page.

### Audit log

Legacy logs what happens to and in sandboxes (the activity log, [activity-and-webhooks/activity-log](activity-and-webhooks/activity-log.md)), but not what's done to the project itself. The audit log is that:

```sql
create table audit_events (
  id            bigint generated always as identity primary key,
  public_id     text not null unique,            -- aud_…
  project_id    bigint references projects(id) on delete set null,
  project_public_id text not null,               -- kept after the project is gone
  kind          text not null,                   -- e.g. member.invited
  actor_type    text not null,                   -- user | admin_key | profile_key | instance_admin | lewbox | setup_link
  actor_id      text,                            -- usr_…, key_…, psl_…
  actor_name    text,
  subject_type  text,                            -- profile, admin_key, cluster, …
  subject_id    text,
  details       jsonb not null default '{}',     -- never secrets
  ip            inet,
  user_agent    text,
  request_id    text,
  occurred_at   timestamptz not null default now()
);
create index on audit_events (project_id, occurred_at desc);
```

What's logged (not exhaustive; each plan adds its own):

| Group | Kinds |
|---|---|
| Project | `project.created`, `project.renamed`, `project.slug_changed`, `project.suspended`, `project.unsuspended`, `project.deletion_requested`, `settings.updated` (with the area and the fields changed, never values of secrets) |
| Members | `member.invited`, `invitation.revoked`, `invitation.accepted`, `member.role_changed`, `member.removed`, `member.left`, `ownership.transferred` |
| Keys | `admin_key.created`, `admin_key.revoked`, `profile_key.created`, `profile_key.revoked` |
| Profiles | `profile.created`, `profile.updated`, `profile.suspended`, `profile.unsuspended`, `profile.deleted`, `profile.limits_replaced`, `profile.credential_set`, `profile.credential_removed`, `profile.git_connected`, `profile.git_disconnected`, `profile.telemetry_set`, `profile.telemetry_removed`, `profile.ssh_key_added`, `profile.ssh_key_removed`, `setup_link.created`, `setup_link.used`, `sign_in.started`, `sign_in.completed`, `sign_in.failed` |
| Clusters | `cluster.created`, `cluster.suspended`, `cluster.unsuspended`, `installation.created`, `node.enrolled`, `node.removed` |
| Templates | `template.created`, `template.updated`, `template.deleted` |
| Webhooks | `webhook_endpoint.created`, `webhook_endpoint.updated`, `webhook_endpoint.deleted`, `webhook_endpoint.secret_rolled`, `webhook_endpoint.disabled` |

- Written in the same transaction as the change, as webhook events are, so the log can't miss a change or record one that didn't happen.
- Kept for `project_settings.audit_retention_days` (default 365, at most what the instance allows), pruned daily.
- Read with `GET /v0/projects/{prj}/audit-events`, filtered by `kind`, `actor`, `subject`, `since` and `until`, cursor-paged. Members with `owner` or `admin` can read it; instance admins can read any project's.
- Not sent as webhooks by default; the webhook catalogue may include some (e.g. `profile.suspended`), decided in [activity-and-webhooks/webhooks](activity-and-webhooks/webhooks.md).

## API

Project management lives under `/v0/projects/{prj}`, reached by a signed-in member (session) or the project's admin key. Instance management lives under `/v0/instance`. The user's own account lives under `/v0/me`. Details of each in the part plans.

| Method | Path | Who | What |
|---|---|---|---|
| `GET` | `/v0/projects` | Session | The user's projects (an admin key gets its own one) |
| `POST` | `/v0/projects` | Session, if allowed | Create a project |
| `GET` | `/v0/projects/{prj}` | Any member, admin key | The project |
| `PATCH` | `/v0/projects/{prj}` | Owner, admin | `name`, `slug` (owner only) |
| `DELETE` | `/v0/projects/{prj}` | Owner (session only) | Request deletion, `202` |
| `GET` | `/v0/projects/{prj}/audit-events` | Owner, admin, admin key with `audit.view` | The audit log |
| | `/v0/projects/{prj}/members…`, `/invitations…` | | [projects/members-and-roles](projects/members-and-roles.md) |
| | `/v0/projects/{prj}/admin-keys…` | | [projects/admin-keys](projects/admin-keys.md) |
| | `/v0/projects/{prj}/settings/…`, `/notifications…` | | [projects/settings](projects/settings.md) |
| | `/v0/instance/…` | | [projects/instance-administration](projects/instance-administration.md) |

The project object:

```json
{
  "id": "prj_4kQ9mZr2LwXv7T",
  "slug": "acme",
  "name": "Acme",
  "status": "active",
  "role": "owner",
  "created_at": "2026-11-02T10:00:00Z"
}
```

`role` is the caller's role: present for a session, `null` for an admin key.

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `project_suspended` | Any key or write while the project is suspended |
| `403` | `role_forbidden` | The member's role doesn't allow it. The message names the role needed |
| `403` | `project_creation_disabled` | The instance doesn't let this user create projects |
| `409` | `project_not_empty` | Deleting a project with sandboxes or online nodes; carries `blocking` |
| `422` | `validation_failed` | e.g. a slug that's taken or reserved (`new`, `api`, `admin`, `settings`, `instance`, `setup`) |

## Security

- **Scoping is structural.** Handlers never take a project id from a body; it comes from the path, checked against the session's membership or the key's project, and every query in `internal/projects`, `internal/profiles` and the rest takes the project as its first argument. A test walks every route with a key of another project and expects `404` ([foundation/testing](foundation/testing.md)).
- **Not found, not forbidden.** A project the caller isn't a member of answers `404`, so ids can't be probed.
- **Destructive actions need a fresh session** (signed in within the last 15 minutes, else a code is asked for again): deleting a project, transferring ownership, registering a GitHub App, creating admin keys.

## Observability

- Audit events as above.
- Metrics: `lewbox_projects_total{status}`, `lewbox_project_members_total{role}`.
- Logs carry `project` on every request scoped to one.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_AUDIT_RETENTION_DAYS_MAX` | `730` | The most a project may keep its audit log |

Instance policies are in [projects/instance-administration](projects/instance-administration.md#configuration).

## Testing

- Tenancy isolation suite: for every route, a second project's session and admin key get `404` (or `403` where the route is unscoped).
- Role matrix: for every route, each role gets the answer [the matrix](projects/members-and-roles.md#what-each-role-can-do) says.
- Project deletion with sandboxes, with nodes, and the batched cleanup, against the fake AerolVM.

## Build plan

Packages: `internal/projects` (projects, settings, audit), `internal/users` (users, sessions, codes), `internal/members` (members, invitations, roles), `internal/auth` (sessions, keys, permission checks).

1. Migrations: `users`, `user_sessions`, `sign_in_codes`, `projects`, `project_members`, `project_invitations`, `admin_keys`, `audit_events`, settings tables.
2. Sign-in by code, sessions, `/v0/me` ([projects/users-and-login](projects/users-and-login.md)).
3. Projects CRUD with slugs; membership checks in the auth middleware.
4. Roles and the permission check helper; invitations and their email.
5. Admin keys.
6. Audit log writer (a helper taking the transaction), reader endpoint, pruning job.
7. Settings tables and endpoints; notifications.
8. Instance admins, policies, `/v0/instance`.
9. Project deletion job.

## Parts

| Part | Covers |
|---|---|
| [users-and-login](projects/users-and-login.md) | Users, sign-in by emailed code, sessions, the dashboard's CSRF protection, account settings, later OAuth and passkeys |
| [members-and-roles](projects/members-and-roles.md) | Roles and what each can do, invitations, ownership |
| [admin-keys](projects/admin-keys.md) | The project's API keys: format, permissions, reach, rotation |
| [instance-administration](projects/instance-administration.md) | Instance admins, signup and project policies, managing every project, bootstrapping a new instance |
| [settings](projects/settings.md) | Plugins on or off for the project, branding, activity retention, sign-ins, Git, profile defaults, credit rates, notifications |

## Open questions

1. **Shared clusters.** Can an instance offer clusters to many projects (lewbox.com's own servers), or does every project bring its own? It changes whether a cluster's `project_id` is nullable ([README](README.md#open-questions)).
2. **Viewer role.** Is a read-only role worth having in the alpha, or do owner, admin and member suffice?
3. **Project limits.** Should the instance cap what a project may have (clusters, nodes, profiles), for lewbox.com's free tier?
4. **Audit events as webhooks.** Which, if any, should be deliverable to the project's webhook endpoints?
5. **Slug in the API.** Accepting slugs as well as ids is friendly, but a renamed slug breaks scripts. Accept ids only in the API?
