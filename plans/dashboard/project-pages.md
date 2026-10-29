# Dashboard: project pages

> Status: Draft · Milestone: Alpha (M1 to M4); credit rates Beta · Updated: 2026-10-29

The pages around a project rather than inside one of its resources: signing in, choosing and creating a project, the project's overview, its members and admin keys, its webhooks and settings, and the instance admin area. The data and rules behind each are owned by [projects](../projects.md), [activity-and-webhooks](../activity-and-webhooks.md) and [git](../git.md); this plan is about what the pages show and do.

## Today

| Legacy page (`lewbox-legacy/resources/js/pages/`) | What it does | Lines |
|---|---|---|
| `Login.svelte` | Email, then a 6-digit code (`harrisonclewis/laravel-passwordless`); anyone may register | 129 |
| `tenants/Select.svelte` | List the user's tenants | 52 |
| `tenants/Create.svelte` | Name a new tenant | 49 |
| `tenants/Account.svelte` | Name and email | 29 |
| `tenants/Dashboard.svelte` | A stub | 23 |
| `tenants/Settings.svelte` | Links to the settings pages | 42 |
| `tenants/settings/ApiKeys.svelte` | Tenant keys: create (shown once), list with last four, revoke | 220 |
| `tenants/settings/Credits.svelte` | Rates per vCPU-hour, GiB-hour, GB-hour, free disk, multiple subscription buckets | 132 |
| `tenants/settings/Profiles.svelte` | Git mode (`token` or `github_app`; Git mode is dropped, GitHub App only), registering the GitHub App by manifest, under an organisation | 217 |
| `tenants/settings/AgentSignIn.svelte` | Sign-in cluster, max sign-in sandboxes, the `agents` template's build there and rebuild | 173 |
| `tenants/settings/Observability.svelte` | Activity retention, successful-command retention, recording stderr | 130 |
| `tenants/settings/Webhooks.svelte` | Endpoints with 7-day activity, response times and errors | 109 |
| `tenants/settings/webhooks/Show.svelte` | An endpoint: deliveries (searchable by id, event id or type prefix), attempts, event data, redeliver, signing secret and roll, test | 545 |

Gaps: no members or invitations ("every account is an administrator"), notifications stored but never shown, no overview, no instance-wide view.

## Goals

- Sign-in that's quick and safe, for people who may belong to several projects.
- An overview that answers "is anything wrong, and what's running?" at a glance.
- Members with roles, and invitations, so a project isn't one person's.
- Webhooks you can debug without reading logs.

## Design

### Sign-in

`/sign-in` (Bare layout).

1. **Email.** One field. Submitting always says "We've sent a code to …", whether or not the address has an account, so the form doesn't reveal who's registered ([projects/users-and-login](../projects/users-and-login.md)).
2. **Code.** `/sign-in/code`: six boxes that accept a paste, auto-submit when full, and show "Resend" after 30 s. Wrong codes count against the address's attempts; after too many the page says to wait.
3. **Done.** Goes to `?next=` if it's a dashboard path, else the last project used, else `/projects`.

- If the instance allows open sign-up, a new email gets an account on the first code. If it's invite-only, an unknown address gets the same "we've sent a code" message but no email, and a member must invite them ([projects/users-and-login](../projects/users-and-login.md)).
- **Later:** passkeys, and sign-in with GitHub or an OIDC provider, for instances that want them.

### Choosing a project

`/projects` (Account layout): the user's projects as cards, each with its name, logo, role, running sandboxes and any problem (a cluster down, a webhook endpoint disabled). "Create project" opens `/projects/new`: a name, then straight into the new project's overview, whose empty state says to add a cluster.

Instances can limit who may create projects (instance admins only, or anyone). The button hides when the user can't.

### Account

`/account`: name; email (changing it sends a code to the new address); signed-in sessions with device, location and last seen, each with "Sign out"; email notification preferences; "Delete account" (refused while the user is the last owner of a project).

### Overview

`/p/{project}`: what's running, and what needs attention.

```
Needs attention                                  Running now
 ● Cluster eu-west degraded: node_… offline 6m    42 sandboxes   86 vCPU   172 GB
 ● Template desktop failed on us-east             by cluster  ▇▇▇▇▇▇ eu-west 30 · us-east 12
 ● Webhook endpoint https://… disabled            by profile  top 5
 ● 3 sandboxes failed in the last 24 h

Clusters                       Recent activity                       Profiles
 eu-west  ● healthy  4 nodes    sbx_… stopped (idle timeout)  2m       128 profiles, 3 suspended
 us-east  ● healthy  2 nodes    sbx_… create failed: image… 5m         Top by running sandboxes
                                prf_… key created           12m
```

- **Needs attention** lists problems from one summary query: clusters not healthy, nodes offline, certificates expiring, builds failed, sandboxes failed recently, webhook endpoints failing or disabled, sign-ins failing, a node's AerolVM older than the cluster's pinned version. Each links to its page. Empty, it says "All clear."
- **Running now:** counts and totals, by cluster and by profile.
- **Recent activity:** the project-wide activity log's problems and operations, newest first, live.
- In Beta, a usage chart (compute units by day) joins it.

### Members

`/p/{project}/settings/members` ([projects/members-and-roles](../projects/members-and-roles.md)).

- A table of members: name, email, role, joined, last active. Owners can change roles and remove members; nobody can remove the last owner.
- **Invite:** email addresses (several at once) and a role. Pending invitations are listed with Resend and Revoke. An invitation link (`/invitations/{token}`) works for a week; accepting signs in by code if needed.
- Roles shown here are the ones [projects/members-and-roles](../projects/members-and-roles.md) defines; the page explains each in one line.

### Admin keys

`/p/{project}/settings/admin-keys`. Replaces `tenants/settings/ApiKeys.svelte`.

- **Create:** name, optional expiry, and later scopes if [projects/admin-keys](../projects/admin-keys.md) adds them. The token (`lba_…`) is shown once in a `SecretReveal`, with snippets for `LEWBOX_ADMIN_KEY` in the CLI, TypeScript and PHP.
- **List:** name, last four, created by, created, last used, expires. Revoke asks to type the key's name.
- A banner when a key hasn't been used in 90 days suggests revoking it.

### Webhooks

`/p/{project}/settings/webhooks` and `/{whk}`. Replaces `tenants/settings/Webhooks.svelte` and `webhooks/Show.svelte` ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)).

- **Endpoints list:** URL, description, events subscribed, enabled, and the last 7 days as a sparkline of deliveries with failures in red, median response time, and the last error. A disabled endpoint shows why (`disabled_reason`) and since when.
- **Add or edit an endpoint:** URL (HTTPS only, checked against private addresses on save as the server does), description, events as a tree of checkboxes grouped by prefix (`sandbox.*`, `operation.*`, `activity.*`, …) with `*` wildcards, enabled. The signing secret (`whsec_…`) is shown once.
- **An endpoint:**
  - **Deliveries:** filter by status and by event type prefix; search by delivery id or event id. Each row: event type, event id, status, attempts, last response code and time.
  - **A delivery:** the event's JSON (CodeMirror, read-only), every attempt with its time, response status, duration, response headers and the first 4 KB of body, and Redeliver.
  - **Secret:** Roll (the old secret keeps signing for 24 h, and the page says until when).
  - **Test:** sends `webhook.test`; the result appears as a delivery, live.
  - **Enable** again after it was disabled, which clears `failing_since`.
- **Verifying deliveries:** a panel with snippets for verifying signatures with each SDK ([sdks-and-cli](../sdks-and-cli.md)).

### Settings

`/p/{project}/settings` goes to General. The settings sidebar lists: General, Members, Admin keys, Webhooks, Plugins, Git, Agents, Observability, then a section for each plugin with project settings that's on (Credits, Beta), contributed through the `project.settings.sections` slot ([plugins/dashboard-slots](../plugins/dashboard-slots.md#slots)).

#### General

Name; the project's id (copy); **branding for setup pages**: display name, logo (upload, stored by the control plane), accent colour, support URL, and allowed origins that may embed the setup pages in an iframe ([setup-pages](setup-pages.md)); danger zone: delete the project, refused while it has clusters with nodes or profiles with sandboxes, and confirmed by typing its name.

#### Plugins

`/p/{project}/settings/plugins` ([projects/settings](../projects/settings.md#plugins), [plugins](../plugins.md)). One row per plugin the instance allows, grouped by kind (agents, Git, DNS, storage, features): title, description, version, an on/off switch, and why it can't change when it can't ("Required by Credits", "Used by 2 clusters", "Always on for this instance"). Turning one off asks for confirmation and says what happens: nothing is deleted, new use is refused, what it wrote into running sandboxes (tokens, logins, keys) is removed from them now. Changes appear across the dashboard at once ([dashboard-slots](../plugins/dashboard-slots.md#how-plugins-reach-the-dashboard)).

#### Git and the GitHub App

`/p/{project}/settings/git`. Replaces the Git half of `tenants/settings/Profiles.svelte` ([git/github-app](../git/github-app.md)).

- **Sandbox Git access cap:** the most any sandbox in the project may be given: *No cap* (default), *Read*, *Write*, or exact permissions. The page says plainly that without a cap sandboxes default to *Full*, so agents in them can push and change workflows, and that *Read* is the setting for "nothing in a sandbox can push" ([git/tokens](../git/tokens.md#the-projects-cap)).
- **Allowed return origins** for install links.
- **GitHub App:** if none is registered, "Register a GitHub App" with an optional organisation, which starts GitHub's manifest flow and comes back here. Or "Bring an App": a form for an App registered by hand. Once registered: its slug, owner, permissions and events (with any Lewbox would ask for that it lacks, and a link to add them on GitHub), the installations it has and how many profiles link each, the webhook URL and whether GitHub's deliveries arrive, and recent deliveries with what each was forwarded as ([github-webhooks](../activity-and-webhooks/github-webhooks.md)). "Delete registration" warns that every profile's App connection stops working.

#### Agents and sign-ins

`/p/{project}/settings/agents`. Replaces `tenants/settings/AgentSignIn.svelte` ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)).

- **Sign-in cluster:** which cluster runs the short-lived sandboxes where profiles sign in their Claude and ChatGPT subscriptions. Only clusters with storage are offered, since the sign-in template is built there, and the page says why.
- **At most this many sign-ins at once**, with how many run now and how many wait.
- **The sign-in template:** its build status on the sign-in cluster, live; "Build" or "Rebuild"; a callout when a newer version from this Lewbox release is waiting to be built.
- **Agents available to profiles:** the agent plugins on for the project (Claude Code and Codex by default; turned on and off on the Plugins page), and for each, which sign-in kinds are allowed (API key, subscription).

#### Observability

`/p/{project}/settings/observability`. Replaces `tenants/settings/Observability.svelte` ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md)).

- Activity retention in days (blank for the default, up to the instance's maximum); successful commands' retention; whether failed commands' stderr is recorded.
- The project's own telemetry export is a profile setting ([profiles/telemetry](../profiles/telemetry.md)); a project-wide default may come later ([open question](#open-questions)).

#### Credit rates (Beta)

`/p/{project}/settings/credits`, the `credits` plugin's settings section, shown when the plugin is on for the project. Replaces `tenants/settings/Credits.svelte` ([credits/metering](../credits/metering.md)): price per vCPU-hour, per GiB-hour of memory and per GB-hour of disk, free disk, whether profiles may have several subscription buckets, with a worked example ("a 2 vCPU, 4 GB, 20 GB sandbox for an hour costs …") that updates as you type.

### Instance admin

`/admin/*`, only for instance admins ([projects/instance-administration](../projects/instance-administration.md)).

- **Projects:** every project, its owners, clusters, profiles, running sandboxes, created; open one (as an instance admin, with a banner saying so and every action logged); suspend; delete.
- **Users:** every user, their projects, last sign-in; make or remove an instance admin (when not fixed by config); sign them out everywhere; delete.
- **Instance settings:** what configuration set (sign-up mode, who may create projects, SMTP, public URL, version), read-only where it came from the environment, with links to the docs.
- **Plugins:** every compiled-in plugin, whether it's on for the instance, how projects get it, its instance settings (secrets redacted) and how many projects use it. **Read-only**: the page says it's set in configuration and links to the docs, as instance enablement has no switch ([plugins/building-and-enabling](../plugins/building-and-enabling.md#configuration)).
- **Health:** the control plane's replicas, job queue depth and oldest job, last leader election, Postgres version and size, and the version of every node across projects.

## API

From other plans: the session and sign-in endpoints ([projects/users-and-login](../projects/users-and-login.md)); members and invitations ([projects/members-and-roles](../projects/members-and-roles.md)); admin keys ([projects/admin-keys](../projects/admin-keys.md)); webhook endpoints, deliveries and stats ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)); project settings and branding ([projects](../projects.md)); a project summary for the overview (counts, problems); notifications; the instance admin endpoints.

## Testing

Playwright: sign-in with a code from the fake SMTP inbox; invite a member and accept as them; create and revoke an admin key, then see the API refuse it; add a webhook endpoint pointed at a test receiver, send a test, see the delivery arrive live, roll the secret, redeliver; the overview's needs-attention list with a fake node taken offline.

## Build plan

1. Sign-in and code pages; account. (M0)
2. Projects list and create. (M1)
3. Admin keys; general settings. (M1)
4. Members and invitations. (M1)
5. Git and GitHub App settings; agents settings. (M3)
6. Observability; webhooks list, endpoint, deliveries. (M4)
7. Overview with needs-attention and running-now. (M4)
8. Instance admin area. (M4, or after the alpha, [open question](#open-questions))
9. Credit rates. (Beta)

## Open questions

1. **Project-wide telemetry default**, which profiles inherit unless they set their own? Legacy only had per-profile telemetry.
2. **Instance admin in the alpha**, or configuration and the CLI only?
3. **Should viewing another project as an instance admin be read-only** unless they "take action" explicitly? (Proposed: read-only by default.)
4. **Branding:** are a logo, name and accent colour enough, or do projects want custom domains for setup pages (Later, with [custom domains](../api.md))?
