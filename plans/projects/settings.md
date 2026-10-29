# Project settings

> Status: Draft · Milestone: Alpha (M1), parts Beta · Core; plugin areas registered by their plugins · Updated: 2026-10-29

A project's **settings** are the choices that apply across it: how its setup pages look, how long its activity is kept, where agent sign-ins run, how its profiles connect GitHub, what limits new profiles start with, what compute costs in credits, and who hears about problems. Legacy kept these as columns added to `tenants` one migration at a time; here they're grouped by area, each owned by the plan for that area, and edited in one place.

## Today

On `tenants` (`database/migrations/*_tenants_*`):

| Column | Area | Default |
|---|---|---|
| `vcpu_hour_rate`, `memory_gib_hour_rate`, `disk_gb_hour_rate` | Credits, millionths of a credit per hour | from `lewbox.credits.rates` |
| `free_disk_gb` | Credits | 20 |
| `allow_multiple_subscription_buckets` | Credits | false |
| `sign_in_cluster_id`, `max_sign_in_sandboxes` | Agent sign-ins | null, 5 |
| `activity_retention_days`, `succeeded_command_retention_days`, `record_command_stderr` | Activity | 30, 3, true |
| `git_mode` | Git | `token` |

Plus the GitHub App in `credentials` (kind `github_app`, on the tenant), and `tenant_notifications` (never shown). Dashboard pages: Settings › API Keys, Credits, Profiles (Git mode, GitHub App), Agent sign-in, Observability, Webhooks (`resources/js/pages/tenants/settings/*`).

Missing: branding for setup pages (they show the tenant's name only), default limits for new profiles (planned in `config/profiles.php`, never built), a way to see notifications.

## Goals

- One settings endpoint per area, typed, documented in the API, with defaults, so adding a setting is a field, not a migration on `projects`.
- Everything a dashboard page edits is editable with an admin key too (except the GitHub App's registration, which needs a browser).
- Notifications that reach people.

## Design

### Storage

One table per area keyed by `project_id`, created with defaults when the project is. Typed columns, not a JSON blob, so constraints and queries stay simple:

```sql
create table project_branding (
  project_id    bigint primary key references projects(id) on delete cascade,
  display_name  text,                  -- defaults to the project's name
  logo_url      text,                  -- https, or a data: URL up to 64 KB, stored in Postgres
  accent_color  text,                  -- #rrggbb
  support_url   text,
  support_email text,
  hide_powered_by boolean not null default false
);

create table project_activity_settings (
  project_id                        bigint primary key references projects(id) on delete cascade,
  retention_days                    int not null default 30,   -- 1..the instance's activity_retention_days_max
  succeeded_command_retention_days  int not null default 3,
  record_command_stderr             boolean not null default true,
  audit_retention_days              int not null default 365
);

create table project_sign_in_settings (
  project_id          bigint primary key references projects(id) on delete cascade,
  cluster_id          bigint references clusters(id) on delete set null,
  max_concurrent      int not null default 5                    -- 1..100
);

create table project_git_settings (
  project_id               bigint primary key references projects(id) on delete cascade,
  sandbox_access_max       jsonb,    -- null: no cap. A preset ("read", "write", "full") or a permission set:
                                     -- the most any sandbox's Git access may be (git/tokens)
  allowed_return_origins   text[] not null default '{}'   -- where install links may send people back
  -- the App itself is in github_apps; there is no Git mode: GitHub App only (git/github-app.md)
);

create table project_profile_defaults (
  project_id       bigint primary key references projects(id) on delete cascade,
  default_cluster_id bigint references clusters(id) on delete set null,
  limits           jsonb not null default '[]',                -- [{kind, period, value, cluster}]
  allowed_isolation text[],                                    -- copied to new profiles; null = any a cluster offers
  default_isolation text                                       -- copied to new profiles; null = the cluster's default
);

create table project_credit_settings (                         -- Beta, the credits plugin's own migration; see credits/balances.md
  project_id                      bigint primary key references projects(id) on delete cascade,
  vcpu_hour_rate                  bigint not null,             -- millionths of a credit
  memory_gib_hour_rate            bigint not null,
  disk_gb_hour_rate               bigint not null,
  free_disk_gb                    int not null default 20,
  allow_multiple_subscription_buckets boolean not null default false
);
```

### Areas

| Area | Endpoint | Owner plan | Milestone |
|---|---|---|---|
| **Plugins** | `/v0/projects/{prj}/settings/plugins` | [Plugins](#plugins) below | Alpha |
| Branding | `/v0/projects/{prj}/settings/branding` | here | Alpha |
| Activity and audit retention | `/v0/projects/{prj}/settings/activity` | [activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md) | Alpha |
| Agent sign-ins | `/v0/projects/{prj}/settings/sign-ins` | [profiles/agent-sign-ins](../profiles/agent-sign-ins.md) | Alpha |
| Git: sandbox access cap (default none), allowed return origins (core Git framework, with `git/github`) | `/v0/projects/{prj}/settings/git` | [git/tokens](../git/tokens.md#the-sandboxs-git-setting), [git/github-app](../git/github-app.md#install-links) | Alpha |
| Profile defaults (default cluster, isolation; default limits when the `limits` plugin is on) | `/v0/projects/{prj}/settings/profile-defaults` | [limits](../limits.md#defaults-for-new-profiles) | Alpha |
| Credit rates (the `credits` plugin) | `/v0/projects/{prj}/settings/credits` | [credits/balances](../credits/balances.md) | Beta |
| Notifications | `/v0/projects/{prj}/settings/notifications` | here | Alpha |

Each is `GET` (any member, any admin key) and `PATCH` (owner or admin; admin key needs the permission named by the area, e.g. `profiles.write` for profile defaults). `PATCH` takes any subset of fields; unknown fields are `422`. Each change writes `settings.updated` to the audit log with the area and the names of the fields changed.

### Plugins

Which plugins a project uses ([plugins](../plugins.md)). The instance decides which plugins are **compiled in** and which are **allowed** (`plugins/config.json` and the instance's configuration, [plugins/building-and-enabling](../plugins/building-and-enabling.md)); within that, a project's owner or admin turns each one **on or off** for the project.

```sql
create table project_plugins (
  project_id  bigint not null references projects(id) on delete cascade,
  plugin      text   not null,                  -- e.g. agents/codex, git/github, dns/libdns, storage/s3, telemetry, limits, credits
  enabled     boolean not null,
  updated_at  timestamptz not null default now(),
  updated_by  bigint references users(id),
  primary key (project_id, plugin)
);
```

- **Defaults:** a plugin without a row uses the instance's default for new projects. Shipped defaults: `agents/claude-code`, `agents/codex`, `git/github`, `dns/libdns`, `storage/s3`, `telemetry` and `limits` on; `credits` off (a project that sells compute turns it on); `agents/opencode` off until it's built.
- **API:** `GET /v0/projects/{prj}/settings/plugins` lists every plugin the instance allows, with `enabled`, its version, what it requires, and a `blocked_by` list when a dependency is off; `PATCH` takes `{"plugins": {"credits": true}}`. Turning on a plugin whose dependency is off is `422 plugin_dependency` naming it (`credits` needs `limits`); turning off a plugin another enabled one needs is refused the same way. Owner or admin, or an admin key with `project.write`.
- **Turning one off never deletes data.** Its settings, rows and history stay; new use is refused with `plugin_disabled`; what it wrote into running sandboxes is removed at their next `apply_setup`, which turning it off enqueues. Turning it back on resumes from the stored data.
- **Audit and webhooks:** `settings.updated` with area `plugins` and the plugins changed; `project.plugin_enabled`, `project.plugin_disabled` webhooks.

### Branding

Setup pages ([profiles/setup-links](../profiles/setup-links.md)) are shown to a project's customers, who shouldn't see Lewbox. Branding sets what they see:

- `display_name`: the name in the page header and title. Defaults to the project's name.
- `logo_url`: an `https` URL (fetched by the browser, never the server) or an uploaded image (PNG, SVG sanitized, or WebP, up to 64 KB, served from `/v0/branding/{prj}/logo` with a long cache and `Content-Security-Policy: default-src 'none'` for SVG).
- `accent_color`: buttons and links. The page checks contrast and falls back to the default if it's unreadable.
- `support_url` / `support_email`: a "Need help?" link.
- `hide_powered_by`: hides the small "Powered by Lewbox" footer. An instance can forbid hiding it (`instance_settings.allow_hide_powered_by`).

Custom domains for setup pages and the API are Later ([README](../README.md#milestones)); branding works without them.

### Sign-in settings

The cluster agent sign-ins run on, and how many at once. Choosing a cluster triggers a build of the provided `agents` template there if it hasn't one ([templates/provided-templates](../templates/provided-templates.md)); the settings page shows that build's status, with Rebuild. A cluster without cloud storage can still be chosen, since helpers don't persist, as long as the `agents` image can be built there (see [profiles/agent-sign-ins](../profiles/agent-sign-ins.md#where-it-runs)).

### Profile defaults

What a new profile starts with, so a project's backend (or a person in the dashboard) doesn't set the same limits every time:

- `default_cluster`: copied to the profile's `default_cluster` when the create doesn't give one.
- `limits`: copied into the profile's limits when it's created, in the same shape as `PUT …/limits` takes. Changing defaults never changes existing profiles.

### Notifications

Things a project should hear about without watching the dashboard. Legacy stored two kinds and showed none.

```sql
create table project_notifications (
  id             bigint generated always as identity primary key,
  public_id      text not null unique,        -- ntf_…
  project_id     bigint not null references projects(id) on delete cascade,
  kind           text not null,
  severity       text not null,               -- info | warning | critical
  subject_type   text,
  subject_id     text,
  title          text not null,
  body           text not null,
  dedupe_key     text,                        -- one open notification per key
  created_at     timestamptz not null default now(),
  resolved_at    timestamptz,                 -- set when the condition clears
  acknowledged_at timestamptz,
  acknowledged_by bigint references users(id) on delete set null
);
create unique index on project_notifications (project_id, dedupe_key) where resolved_at is null;
```

| Kind | Severity | Raised when | Resolved when |
|---|---|---|---|
| `certificate_expiring` | warning, critical within 3 days | A node's certificate expires within 14 days and renewal failed | It renews |
| `node_offline` | critical | A node misses heartbeats for 3 minutes | It's back |
| `cluster_degraded` | critical | A cluster loses quorum | It regains it |
| `aerolvm_outdated` | info | A node runs an AerolVM fork version older than the instance supports | It's updated |
| `webhook_endpoint_failing` | warning | An endpoint has failed for an hour | A delivery succeeds |
| `webhook_endpoint_disabled` | critical | After 72 hours of failures | It's re-enabled |
| `github_app_refused` | warning | GitHub refuses the project's App (suspended, permissions) | It works again |
| `sign_in_template_failed` | warning | The `agents` template failed to build on the sign-in cluster | A build succeeds |
| `admin_key_expiring` | info | An admin key expires within 7 days | Revoked, or expired |
| `quota_reached` | info | A [quota](instance-administration.md#quotas) is reached | It's under again |
| `budget_warning` | info | Beta: a profile passes 80% of a budget ([credits/budgets-and-usage](../credits/budgets-and-usage.md)) | Period ends |

- Raised by the code that notices (heartbeat checker, webhook sender, …) through one function, `notify(ctx, tx, project, kind, subject, dedupe)`, which inserts at most one open notification per `dedupe_key`.
- Shown in the dashboard's bell, unresolved first, and on the page of their subject (a node, an endpoint).
- **Emailed** to members by role and severity: `critical` to owners and admins at once; `warning` in a daily digest; `info` in the dashboard only. Each user can turn emails off per project (`GET/PATCH /v0/me/notification-preferences`).
- Also sent as webhooks, `notification.created` and `notification.resolved`, if an endpoint subscribes ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)).
- Settings: `/v0/projects/{prj}/settings/notifications` holds the project's email routing (which roles get which severities, an extra address such as an on-call alias).

| Method | Path | What |
|---|---|---|
| `GET` | `/v0/projects/{prj}/notifications` | Filter `status=open|resolved|all`, `severity` |
| `POST` | `/v0/projects/{prj}/notifications/{ntf}/acknowledgement` | Mark seen |

## Security

- Branding values are shown to the project's customers; they're escaped like any text, and the logo is never fetched by the server (no SSRF), only by browsers.
- Settings never hold secrets; the GitHub App's secrets live in `github_apps`, encrypted ([git/github-app](../git/github-app.md)).

## Testing

- Each area's defaults on project create, partial `PATCH`, validation bounds, role and permission checks, audit entry with field names.
- Notifications: dedupe, resolve, digest batching, per-user preferences, emails captured by the fake SMTP.
- SVG logo sanitizing.

## Build plan

Package `internal/projects/settings` for the tables and endpoints, including `project_plugins` (read by the plugin registry on every request through a short cache, [plugins/registry-and-lifecycle](../plugins/registry-and-lifecycle.md)); plugin-owned areas (credit rates) are registered by their plugins with their schema; `internal/notifications` for raising, resolving, email and webhooks.

1. Settings tables and defaults on project create.
2. Endpoints per area (the area plans own validation rules).
3. Branding with logo upload; served for setup pages.
4. Notifications: table, `notify`, dashboard endpoints, email immediately and as a digest job, preferences.

## Open questions

1. ~~**Retention bounds.**~~ Decided (2026-10-29): the instance sets the cap (`activity_retention_days_max` in [instance settings](instance-administration.md#policies)): 90 days on lewbox.com, unlimited on self-hosted by default; a project chooses within it.
2. ~~**Digest timing.**~~ Decided (2026-10-29): daily at a fixed UTC hour (08:00) in the alpha; per-user timezones Later.
3. **Branding on emails.** Deferred (Later): Lewbox sends no email to a project's customers yet; branding and reply-to come with the first that does.
