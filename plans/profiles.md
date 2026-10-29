# Profiles

> Status: Draft · Milestone: Alpha (M1 core, M3 setup) · Updated: 2026-10-29

A **profile** is who a project's sandboxes belong to: usually one of the project's own customers (Lewagent makes one per user), sometimes an environment or a team. A profile holds what its sandboxes are allowed and given: **limits** on what they may use, **keys** to create and work with them, **agent credentials** so Claude Code and Codex are signed in, a **Git connection** and identity, **SSH keys**, and a **telemetry** endpoint for its agents. Profiles don't own infrastructure: sandboxes run on the project's clusters, and a profile decides who may create them, how big, and what they're signed in to.

```
Project (prj_)
 └─ Profile (prf_)            external_id, status, default cluster, Git identity
     ├─ Profile keys (lbp_)    which clusters, which permissions           profiles/profile-keys
     ├─ Limits                 per sandbox, at once, per cluster, budgets  limits            (plugin: limits)
     ├─ Agent credentials      Claude Code, Codex: keys or subscriptions   profiles/agent-credentials (plugins: agents/*)
     │   └─ Sign-ins (asi_)    subscription sign-in in a helper sandbox    profiles/agent-sign-ins    (plugins: agents/*)
     ├─ GitHub connection      the project's GitHub App, many accounts     git/github-app, git/tokens (plugin: git/github)
     ├─ SSH keys (ssh_)        written into every sandbox                  profiles/ssh-keys
     ├─ Telemetry              agents' OpenTelemetry → the customer's OTLP profiles/telemetry (plugin: telemetry)
     ├─ Setup links (psl_)     hosted page for the customer to do the above profiles/setup-links
     ├─ Credits (Beta)         buckets and transactions                    credits/balances  (plugin: credits)
     └─ Sandboxes (sbx_)       and their conversations                     sandboxes, agents
```

## Plugins

Profiles, profile keys, setup links and SSH keys are core. What a profile is *given* comes mostly from plugins ([plugins](plugins.md)), which each add their section to the profile, its API, its setup page and its sandboxes' setup:

| Plugin | Adds to a profile | Extension points |
|---|---|---|
| `limits` | Limits and their enforcement ([limits](limits.md)) | Admission, defaults resolution, settings |
| `agents/claude-code`, `agents/codex`, `agents/opencode` (planned) | Credential methods (API key, subscription), verification, sign-in flows, what's written into sandboxes ([agent-credentials](profiles/agent-credentials.md), [agent-sign-ins](profiles/agent-sign-ins.md)) | Agent provider ([plugins/agent-providers](plugins/agent-providers.md)), setup contributor, setup-page section |
| `git/github` | GitHub connections: people and installations ([git/github-app](git/github-app.md)) | Git provider ([plugins/git-providers](plugins/git-providers.md)), setup-page section |
| `telemetry` | An OTLP endpoint for its agents ([telemetry](profiles/telemetry.md)) | Setup contributor, settings, setup-page section |
| `credits` (Beta) | Buckets, ledger, usage ([credits](credits.md)) | Admission, lifecycle, jobs |

A profile's API shows a plugin's section only while the plugin is on for the project; its data is kept when it's turned off.

## Today

Legacy profiles (`lewbox-legacy/app/Models/Profile.php`, design in `docs/profiles.md`, admin docs `docs/admin/profiles/*`) are built and in use by Lewagent:

- `profiles`: `public_id` (`prf_`), `tenant_id`, `external_id` (unique per tenant among live profiles), `name` (unique per tenant), `status` (`active`, `suspended`), `default_cluster_id`, `git_name`, `git_email`; soft-deleted.
- Created in the dashboard (`Tenant\Profile\ProfileController`) or the Admin API (`POST /api/v1/profiles`); looked up by `GET /profiles?external_id=`.
- Suspending refuses its keys (`403 profile_suspended`) and stops its running sandboxes; deleting is refused while it has sandboxes (`409 profile_has_sandboxes`) and revokes its keys.
- Consumers read their own: `GET /v1/profile` on the gateway (via the app's `whoami`), with `default_cluster`, reachable `clusters`, `limits`, `usage`, `credits`. `PATCH /v1/profile` (to change the default cluster) is documented at [lewbox.com/docs/profile/overview](https://lewbox.com/docs/profile/overview) but not built.
- Lewagent provisions one per user in `app/Jobs/Users/ProvisionLewboxProfile.php`: find by `external_id` (the user's public id) or create with a default cluster, replace limits from its plan (`Plan::lewboxLimits()`), make a key with cluster access `all`, and store the token on the user.
- The profile's setup (agents, Git, SSH keys, telemetry) is applied to its sandboxes as a manifest the gateway runs ([sandboxes/profile-setup](sandboxes/profile-setup.md)).

What's wrong:

- The key lookup (`whoami`) crosses HTTP and is cached 60 s, so a revoked key or a suspension takes up to a minute.
- Profile-level template limits (`templates`, `template_storage_mb`) are documented and enforced nowhere.
- No per-cluster limits, which Lewagent asked for.
- Two key kinds for one consumer feature (see [projects/admin-keys](projects/admin-keys.md#acting-on-a-profiles-sandboxes)).
- Default limits for new profiles were planned and never built.

## Goals

- Keep the profile model: flat, one project, isolated from its siblings.
- Make every profile setting reachable three ways with the same rules: the dashboard, the management API (admin key), and the hosted setup page (setup link).
- Changes take effect at once: a revoked key or a suspended profile is refused on the next request, a changed credential reaches running sandboxes through `apply_setup`.
- Enforce every documented limit, including templates and per-cluster ones.

### Non-goals

- Nesting profiles. A project that wants "customer → environment" makes a profile per leaf and names them so.
- Profiles sharing credentials. Each signs in its own subscriptions; a project's backend may set the same API key on many profiles, which is its choice.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | `prf_` ids, `external_id`, `name`, statuses, default cluster, Git identity, deletion refused while sandboxes exist. Limits as rows with kinds. Credentials write-only. Setup links. The consumer's `GET /v0/profile`. |
| **Change** | Key lookups are direct database reads (no 60 s cache). Limits gain an optional cluster scope, and template limits are enforced. New profiles get the project's default limits. `PATCH /v0/profile` is built. Setup links are rendered by the React dashboard. Sign-ins run as jobs, not a 5-second sweep. |
| **Drop** | `tenant_id` (now `project_id`). Name uniqueness (names are labels; `external_id` is the unique handle). |
| **Add** | `metadata` on profiles (string pairs, like sandboxes'). Profile webhooks events. Admin-API sign-ins (start one and give the customer the link in the project's own product). |

## Design

### Data model

```sql
create table profiles (
  id                 bigint generated always as identity primary key,
  public_id          text not null unique,                 -- prf_…
  project_id         bigint not null references projects(id) on delete cascade,
  name               text not null,
  external_id        text,                                 -- the project's own id, e.g. a customer id
  status             text not null default 'active',       -- active | suspended
  suspended_reason   text,
  default_cluster_id bigint references clusters(id) on delete set null,
  allowed_isolation  text[],                               -- container | gvisor | microvm; null = any a cluster offers
  default_isolation  text,                                 -- null = the cluster's default
  git_name           text,
  git_email          text,
  metadata           jsonb not null default '{}',          -- up to 50 string pairs
  created_by_user    bigint references users(id) on delete set null,
  created_by_key     bigint references api_keys(id) on delete set null,
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now(),
  deleted_at         timestamptz
);
create unique index on profiles (project_id, external_id) where deleted_at is null and external_id is not null;
create index on profiles (project_id, created_at) where deleted_at is null;
```

### Statuses

| Status | Keys | Sandboxes | Reversible |
|---|---|---|---|
| `active` | Work | Run | |
| `suspended` | Every key refused with `403 profile_suspended`, even for reading | Running ones stopped with `stop` operations, reason `profile_suspended` (persisting ones keep files; others are deleted). Creates and starts refused | Yes: `PATCH status: active`; sandboxes stay stopped |
| *deleted* | Revoked | None may exist | No |

Suspending also cancels the profile's sign-ins under way, pauses its ChatGPT login refreshes, and leaves its stored conversations readable through the management API.

### Creating a profile

`POST /v0/projects/{prj}/profiles` with `name`, optional `external_id`, `default_cluster` (name or id, one of the project's), `metadata`, and optionally `limits` to set at once (else the [project's defaults](limits.md#defaults-for-new-profiles)). Takes an `Idempotency-Key`. A live profile with the same `external_id` is `409 external_id_taken`, carrying the existing profile's `id`, so a sign-up flow that runs twice can recover without a separate lookup. One transaction: the profile, its limits, the audit event, the webhook event `profile.created`.

Lewagent's provisioning becomes:

```
POST /v0/projects/{prj}/profiles          { name, external_id: usr_…, default_cluster: "eu-west",
                                            limits: [...plan limits...] }        (Idempotency-Key: usr_…)
POST /v0/projects/{prj}/profiles/{prf}/keys  { name: "Lewagent", cluster_access: "all" }
```

### Changing and deleting

- `PATCH /v0/projects/{prj}/profiles/{prf}`: `name`, `external_id`, `default_cluster`, `allowed_isolation`, `default_isolation` ([isolation](#isolation)), `git_name`, `git_email`, `metadata`, `status`, `suspended_reason`. Git identity changes re-apply setup to running sandboxes ([sandboxes/profile-setup](sandboxes/profile-setup.md)).
- `DELETE …/profiles/{prf}`: refused with `409 profile_has_sandboxes` while it has sandboxes that aren't `deleted`, naming how many; `?force=true` (owner or admin, or an admin key with `sandboxes.delete` and `profiles.write`) deletes them first as `delete` operations, and deletes the profile once they're all gone (the request answers `202`). Deleting revokes keys, removes credentials, SSH keys, telemetry and setup links, cancels sign-ins, and keeps conversations for the project's retention (they belong to the project's history, [agents/storage-and-uploads](agents/storage-and-uploads.md)).

### The consumer's view

`GET /v0/profile` answers for the profile a key belongs to:

```json
{
  "data": {
    "id": "prf_kS0xNEbAsnp8hP",
    "name": "Acme",
    "external_id": "cus_123",
    "status": "active",
    "default_cluster": { "id": "clu_dOMNHWyaFGfMBf", "name": "eu-west" },
    "clusters": [{ "id": "clu_dOMNHWyaFGfMBf", "name": "eu-west",
                   "isolation": [{ "level": "gvisor", "default": true, "capabilities": { "persist": true, "gpu": false, "nested_sandbox": false } },
                                 { "level": "microvm", "default": false, "capabilities": { "persist": false, "gpu": false, "nested_sandbox": true } }] }],
    "limits": [{ "kind": "running_sandboxes", "period": null, "cluster": null, "value": 3 }],
    "usage": [{ "period": "day", "compute_units": 12.5 }, { "period": "month", "compute_units": 402.75 }],
    "credits": null,
    "agents": [{ "agent": "claude-code", "configured": true, "method": "subscription" },
               { "agent": "codex", "configured": false, "method": null }],
    "git": { "connected": true, "accounts": ["acme"], "user_authorized": true },
    "key": { "id": "key_…", "permissions": null, "cluster_access": "all", "expires_at": null }
  }
}
```

- `clusters` is what *this key* reaches; `default_cluster` is `null` if the key doesn't reach it ([profiles/profile-keys](profiles/profile-keys.md#cluster-access)).
- Each cluster's `isolation` lists the levels **this profile may use** there, with which one a create gets by default and what each can do ([isolation](#isolation)).
- `agents` and `git` say what's set up, never secrets, so a consumer can tell its user "connect GitHub first".
- `key` describes the calling key, so a consumer can check its permissions.
- A sandbox key can't read it (`403 wrong_key_kind`), as legacy.

`PATCH /v0/profile { default_cluster }` changes the default to a cluster the key reaches; that's the only thing a profile key may change on its profile.

### Isolation

How strongly a profile's sandboxes are isolated is the project's choice, per profile. Lewbox offers three [isolation levels](clusters/runtimes.md#isolation-levels), weakest to strongest: `container` (Docker), `gvisor` and `microvm` (Firecracker). A cluster offers some of them; a profile is allowed some of them; a sandbox is created at one that's both ([clusters/runtimes § Choosing a sandbox's level](clusters/runtimes.md#choosing-a-sandboxs-level)). **In the alpha only `container` exists** ([D16](README.md#decisions)): the settings are built and stored, so a project can already say "at least `gvisor`" for a profile, but such a profile can't create sandboxes until `gvisor` is offered in the beta, and the dashboard says so.

| Setting | Values | Means |
|---|---|---|
| `allowed_isolation` | A list of levels, or `null` | The levels this profile's sandboxes may use. `null`: any level a cluster offers. A minimum is the list from that level up: `["gvisor", "microvm"]` is "at least gVisor" |
| `default_isolation` | A level, or `null` | What a create gets when it doesn't name one, if the cluster offers it. `null`: the cluster's default |

- **Set by the project** (`PATCH /v0/projects/{prj}/profiles/{prf}`, by a session or an admin key with `profiles.write`), never by a profile key: a profile can't raise its own isolation's reach any more than its limits. New profiles take the project's defaults ([projects/settings](projects/settings.md)).
- **Checked** on create: `default_isolation` must be in `allowed_isolation`; levels the instance doesn't allow (`LEWBOX_ALLOWED_ISOLATION`) are `422 validation_failed`.
- **Tightening** the list doesn't touch existing sandboxes: they keep their level for life. It applies to creates from then on. The management view counts sandboxes at levels the profile is no longer allowed, so a project can replace them.
- **Shown to the consumer** on `GET /v0/profile`, per cluster, so an app can offer only what will work.

Lewagent, for example, would set `allowed_isolation: ["microvm"]` on its profiles once `microvm` persists, to keep its "every computer runs in its own microVM" promise, and `["gvisor", "microvm"]` until then.

### The management view

`GET /v0/projects/{prj}/profiles` lists profiles, filtered by `status`, `external_id`, `q` (name), `metadata[key]=value`, cursor-paged; each with counts of running sandboxes and keys. `GET …/profiles/{prf}` adds limits, setup state (agents, Git, SSH keys count, telemetry provider), and usage. Profiles are found by `prf_` id or, with `?by=external_id`, by external id: `GET /v0/projects/{prj}/profiles/cus_123?by=external_id`.

### Setup reaches sandboxes

Agent credentials, the Git connection and identity, SSH keys and telemetry are written into sandboxes by **profile setup**, applied on create and every start, cleaned up before a stop, and re-applied to running sandboxes as an `apply_setup` operation when any of them change. That mechanism is planned in [sandboxes/profile-setup](sandboxes/profile-setup.md); the part plans here say what each contributes.

## API summary

Management (`/v0/projects/{prj}/…`; session or admin key):

| Method | Path | Permission (admin key) |
|---|---|---|
| `GET` | `/profiles`, `/profiles/{prf}` | none |
| `POST` | `/profiles` | `profiles.write` |
| `PATCH` | `/profiles/{prf}` | `profiles.write` |
| `DELETE` | `/profiles/{prf}` | `profiles.write` |
| | `/profiles/{prf}/keys…` | `profiles.keys`, [profile-keys](profiles/profile-keys.md) |
| | `/profiles/{prf}/limits` | `profiles.write`, [limits](limits.md) |
| | `/profiles/{prf}/agents…`, `/sign-ins…` | `profiles.credentials`, [agent-credentials](profiles/agent-credentials.md), [agent-sign-ins](profiles/agent-sign-ins.md) |
| | `/profiles/{prf}/git…`, `/repositories…` | `profiles.credentials`, [git](git.md) |
| | `/profiles/{prf}/ssh-keys…` | `profiles.credentials`, [ssh-keys](profiles/ssh-keys.md) |
| | `/profiles/{prf}/telemetry` | `profiles.credentials`, [telemetry](profiles/telemetry.md) |
| | `/profiles/{prf}/setup-links` | `profiles.credentials`, [setup-links](profiles/setup-links.md) |
| | `/profiles/{prf}/credit-buckets…` | `profiles.credits`, [credits/balances](credits/balances.md) (Beta) |

Consumer (`/v0/…`; profile key): `GET /v0/profile`, `PATCH /v0/profile`, `GET /v0/profile/github…` (connection, repositories, tokens with `git.tokens`) ([git](git.md)).

### Errors

| Status | Code | When |
|---|---|---|
| `403` | `profile_suspended` | Any key of a suspended profile |
| `404` | `not_found` | Another project's profile, or a deleted one |
| `409` | `external_id_taken` | Create or change to an `external_id` a live profile has; carries `profile` |
| `409` | `profile_has_sandboxes` | Delete without `force` while sandboxes exist; carries `sandboxes` (count) |
| `422` | `validation_failed` | e.g. a `default_cluster` that isn't the project's |

### Webhook events

`profile.created`, `profile.updated`, `profile.suspended`, `profile.unsuspended`, `profile.deleted`; `profile.setup_changed` (any of agents, Git, SSH keys, telemetry changed, with which); `sign_in.completed`, `sign_in.failed`. The catalogue lives in [activity-and-webhooks/webhooks](activity-and-webhooks/webhooks.md).

## Security

- Profiles in a project never see each other: every consumer query is scoped by the key's profile, every management query by the project.
- Secrets (agent keys, subscription logins, Git tokens, telemetry headers) are encrypted with the keyring ([foundation/secrets-and-encryption](foundation/secrets-and-encryption.md)), never returned, and redacted from setup reports. They are, by design, readable inside the profile's sandboxes; the setup page says so.
- `external_id` and `metadata` are the project's data, shown back as given; never interpreted.

## Observability

- Audit: `profile.*` events ([projects](projects.md#audit-log)).
- Metrics: `lewbox_profiles_total{status}`; per-profile running sandboxes are a query, not a metric (cardinality).

## Testing

- Isolation between profiles and projects on every consumer and management route.
- Suspension: keys refused immediately on two replicas; sandboxes stopped through operations; unsuspend.
- Delete with and without `force`; conversations kept.
- Idempotent create and `external_id_taken` recovery.

## Build plan

Core: `internal/profiles` (profiles, setup links, SSH keys, the credentials store), `internal/signins` (the sign-in machinery and refresh scheduling), with keys in `internal/auth`. Plugins: `plugins/limits`, `plugins/telemetry`, `plugins/agents/{claude-code,codex}` (credential methods, verification, sign-in flows), `plugins/git/github`.

1. Profiles CRUD, statuses, the consumer `GET/PATCH /v0/profile`.
2. Profile keys ([profile-keys](profiles/profile-keys.md)).
3. Limits and their enforcement in sandbox reserve ([limits](limits.md)).
4. Credentials, SSH keys, telemetry, and their setup steps (with [sandboxes/profile-setup](sandboxes/profile-setup.md)).
5. Setup links and the hosted page.
6. Sign-ins and ChatGPT refresh.

## Parts

| Part | Covers | Milestone |
|---|---|---|
| [profile-keys](profiles/profile-keys.md) | `lbp_` keys: cluster access, permissions, expiry, revocation | M1 |
| [limits](limits.md) | The `limits` plugin: every limit kind, per-cluster limits, defaults, enforcement in one transaction (now a top-level plan) | M1 |
| [setup-links](profiles/setup-links.md) | The hosted setup page for a profile's customer, branding, sessions | M3 |
| [agent-credentials](profiles/agent-credentials.md) | API keys and subscription logins for Claude Code and Codex, verification, how they reach sandboxes | M3 |
| [agent-sign-ins](profiles/agent-sign-ins.md) | Signing in subscriptions in helper sandboxes, the state machine, ChatGPT refresh | M3 |
| [ssh-keys](profiles/ssh-keys.md) | Public keys on the profile and on single sandboxes | M2 |
| [telemetry](profiles/telemetry.md) | Agents' OpenTelemetry to the customer's OTLP endpoint | M3 |

Git (connection, identity, repositories) is in [git](git.md); credits are in [credits](credits.md).

## Open questions

1. ~~**Lewagent's mapping.**~~ Decided (2026-10-29): one profile per user, as today; a computer gets its user's subscriptions and GitHub.
2. **Profile-scoped webhooks.** Deferred (Later): webhook endpoints belong to the project in the alpha; profile endpoints if a project's customers ask to integrate directly.
3. ~~**Names unique?**~~ Decided (2026-10-29): no; names are for display, and `external_id` (unique per project) is the handle.
4. ~~**Deleting with `force`.**~~ Decided (2026-10-29): allowed, as the API above describes: owners, admins, or an admin key with `sandboxes.delete` and `profiles.write`; `DELETE …?force=true` deletes the profile's sandboxes first, each as a normal delete operation, and is recorded in the audit log.
