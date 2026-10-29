# Limits

> Status: Draft · Milestone: Alpha (M1); budgets Beta · Plugin: `limits` · Updated: 2026-10-29

A profile's **limits** cap what its sandboxes may use: how many run at once and their total size, how big one sandbox may be and how long it may idle or run, how much disk its persistent sandboxes hold, how many templates it keeps, and (with the [credits](credits.md) plugin) how many compute units it spends a day or a month. A limit that isn't set is unlimited. Limits protect a project's hardware from one customer and give its plans something to sell.

## Plugin

Limits are the **`limits` plugin** ([plugins](plugins.md)), compiled in and **on by default**. A self-hoster running only their own work can turn it off for an instance or a project: creates and starts are then checked against nothing but the cluster's capacity and the per-sandbox ranges core validates (e.g. `cpu` 0.1 to 256). Turning it off keeps every limit row; turning it back on enforces them again from the next create or start. The [`credits`](credits.md) plugin requires it, as budgets are `compute_units` limits.

It plugs into core through ([plugins/extension-points](plugins/extension-points.md)):

| Extension point | What limits does with it |
|---|---|
| Admission (in the create and start transaction, profile row locked) | The checks below; `429 limit_exceeded` or `422 validation_failed` |
| Defaults resolution (sizes and timers left out of a create) | The profile's per-sandbox limits as caps and defaults |
| Migrations, routes, permissions, settings | `profile_limits`; `…/profiles/{prf}/limits`; `profiles.write`; the project's defaults for new profiles |
| Templates and agents admission | `templates`, `template_storage_mb`, `conversations_running` |

Its code is `plugins/limits/` (was planned as `internal/profiles/limits`). The plan sat under profiles until 2026-10-29.

## Today

- `profile_limits` (`ProfileLimit`): one row per `(profile_id, kind, period)`, `value` an unsigned integer in the kind's unit; missing means unlimited. Kinds in `App\Enums\Profile\LimitKind`.
- Enforced by `EnsureProfileCanRun` (`lewbox-legacy/app/Actions/Sandboxes/EnsureProfileCanRun.php`), called by `ReserveSandbox` and starts **with the profile row locked**: refuses a suspended profile, sums active workloads' count, `cpu` and `memory_mb` plus the new one against `running_sandboxes`, `vcpus`, `memory_mb`; `ensureStorageFor` sums persistent non-deleted sandboxes' `disk_gb` against `storage_gb`; `spendingRefusal` checks `compute_units` budgets and credits.
- Per-sandbox limits are checked on create and then passed to AerolVM as `cpu`, `memory_mb`, `disk_gb`, and `lifecycle` (`stop_if_idle_for`/`stop_at_age` or `destroy_*`).
- **Asking for more than a limit is refused, never lowered**; a size left out gets the default or the limit, whichever is lower; a timer left out gets the limit.
- Helper sandboxes for sign-ins (`purpose = sign_in`) don't count.
- `PUT /api/v1/profiles/{prf}/limits` replaces all of them; `GET` lists them (`docs/admin/profiles/limits.md`). Consumers read them on `GET /v1/profile` ([lewbox.com/docs/profile/limits](https://lewbox.com/docs/profile/limits)).
- `templates` and `template_storage_mb` are documented but **not enforced**.
- **Defaults for new profiles** were planned in `config/profiles.php` and never built.
- Lewagent sets `running_sandboxes`, `vcpus`, `memory_mb`, `sandbox_vcpus`, `sandbox_memory_mb`, `sandbox_disk_gb`, `storage_gb` from its plans, and has asked for **limits per cluster** (`lewagent-legacy/docs/todo.md`, *Waiting on Lewbox*).

## Goals

- Every documented kind enforced, including template limits.
- Optional per-cluster scope on any kind that's about capacity.
- Enforcement that can't be raced: one transaction, the profile row locked, the sandbox and its operation inserted before the lock is released.
- Project-level defaults for new profiles.
- Clear refusals: `limit_exceeded` carries which limit, its value and scope, and what's in use.

### Non-goals

- Limits on a project across all its profiles. The project's capacity is its nodes; instance quotas are a separate thing ([projects/instance-administration](projects/instance-administration.md#quotas)).
- Rate limits (requests per minute) as limits. They live in [api/rate-limits](api/rate-limits.md).

## Design

### Kinds

| Kind | Caps | Unit | Period | Cluster scope | Enforced when |
|---|---|---|---|---|---|
| `running_sandboxes` | Sandboxes running at once (`creating`, `running`, `starting`, `stopping`*) | sandboxes | | yes | create, start |
| `vcpus` | vCPUs across those | millicores | | yes | create, start |
| `memory_mb` | Memory across those | MB | | yes | create, start |
| `storage_gb` | `disk_gb` of persistent sandboxes not deleted, running or stopped | GB | | yes | create (persistent) |
| `sandbox_vcpus` | One sandbox's `cpu` | millicores | | yes | create |
| `sandbox_memory_mb` | One sandbox's `memory_mb` | MB | | yes | create |
| `sandbox_disk_gb` | One sandbox's `disk_gb` | GB | | yes | create |
| `sandbox_idle_seconds` | One sandbox's `idle_timeout` | seconds | | no | create, settings change |
| `sandbox_lifetime_seconds` | One sandbox's `lifetime` | seconds | | no | create, settings change |
| `conversations_running` | Agent turns running at once across the profile (new) | turns | | no | prompt |
| `templates` | The profile's own templates | templates | | no | template create |
| `template_storage_mb` | Their images' size, counted on each cluster built | MB | | yes | template create, rebuild |
| `compute_units` | Units spent in the period (Beta, [credits/budgets-and-usage](credits/budgets-and-usage.md)) | units | `day`, `month` | no | create, start, and continuously |

\* `stopping` counts, so a stop followed at once by a start can't briefly double-book; metering still ends at the stop ([credits/metering](credits/metering.md)).

Helper sandboxes (sign-ins) never count. vCPUs are millicores because AerolVM takes fractional CPUs; the dashboard shows friendlier units (vCPUs, GB, minutes, hours), and the API always takes and returns the stored unit, as documented.

`conversations_running` is new: each running turn costs about 1 GB of a sandbox's memory, and Lewagent's notes show 17 idle chats holding 2.5 GB. A per-profile cap on concurrent turns lets a project bound agent load without capping sandboxes. Refused prompts are `429 limit_exceeded` with `limit.kind = conversations_running` ([agents/conversations](agents/conversations.md)).

### Per-cluster limits

A limit row may name a cluster. A cluster-scoped limit applies to sandboxes on that cluster only; an unscoped one applies across all clusters. Both apply: a create on `eu-west` must fit the profile-wide `running_sandboxes` **and** `eu-west`'s, if set. Typical use: "3 sandboxes anywhere, but at most 1 on the GPU cluster", or Lewagent's plans differing per region.

Per-sandbox kinds scoped to a cluster override the profile-wide value for that cluster when lower, and also when higher (a GPU cluster may allow bigger sandboxes than the default cap): the cluster's value is the cap there. For totals, both must hold.

### Isolation isn't a limit

Which [isolation levels](clusters/runtimes.md#isolation-levels) a profile may use (`allowed_isolation`, `default_isolation`) is a profile setting, not a limit row: it's a set of allowed values, not a number, and has no period or usage. It's checked in the same create transaction, before the limits ([profiles § Isolation](profiles.md#isolation)). Limits per isolation level (say, at most two `microvm` sandboxes, as their memory is reserved) are an open question in [clusters/runtimes](clusters/runtimes.md#open-questions).

### Data model

```sql
create table profile_limits (
  id          bigint generated always as identity primary key,
  profile_id  bigint not null references profiles(id) on delete cascade,
  kind        text not null,
  period      text,                      -- day | month, only for compute_units
  cluster_id  bigint references clusters(id) on delete cascade,
  value       bigint not null check (value >= 0),
  unique nulls not distinct (profile_id, kind, period, cluster_id)
);
```

`unique nulls not distinct` (Postgres 15+) makes "one profile-wide value per kind and period" a constraint, not a convention. Deleting a cluster deletes its scoped limits.

### Defaults for new profiles

The project's `project_profile_defaults.limits` ([projects/settings](projects/settings.md#profile-defaults)) are copied into a new profile's limits unless the create gives `limits` itself (which replace the defaults wholesale, including `[]` for none). Changing defaults never touches existing profiles; the dashboard offers "Apply to all profiles" as an explicit, audited bulk action.

### Resolving a sandbox's size and timers

On create, for each of `cpu`, `memory_mb`, `disk_gb`, `idle_timeout`, `lifetime`:

1. The **cap** is the cluster-scoped per-sandbox limit for the chosen cluster if there is one, else the profile-wide one, else none.
2. Asked for and within the cap (and the [sandbox field's own range](sandboxes/lifecycle.md)): use it.
3. Asked for and over the cap: `422 validation_failed` on that field, "May be at most 2 vCPUs for this profile". Never lowered.
4. Not asked for: the template's default, else Lewbox's (`2` vCPU, `4096` MB, `20` GB; no timers); then the lower of that and the cap. A timer with no default gets the cap (so a profile with `sandbox_idle_seconds` always has sandboxes that idle out).

Changing a running sandbox's `idle_timeout` or `lifetime` checks the same caps; `null` in a change means "the longest allowed", i.e. the cap, or none without one.

### Enforcement: one transaction

Reserving a sandbox ([sandboxes/lifecycle](sandboxes/lifecycle.md)) and starting one run the same check, inside the transaction that inserts the sandbox (or its `start` operation):

```sql
begin;
select id, status from profiles where id = $profile for update;      -- serializes this profile's creates and starts
-- refuse suspended profile / project (403)
select coalesce(count(*), 0)            as running,
       coalesce(sum(cpu), 0)            as cpu,
       coalesce(sum(memory_mb), 0)      as memory
  from sandboxes
 where profile_id = $profile and purpose = 'workload'
   and status in ('creating','running','starting','stopping')
   and ($cluster is null or cluster_id = $cluster);                    -- once per scope that has a limit
select coalesce(sum(disk_gb), 0) as stored
  from sandboxes where profile_id = $profile and persist and status <> 'deleted' and purpose = 'workload';
-- compare totals + this sandbox against each applicable limit; refuse 429 limit_exceeded
-- Beta: budget and credit checks (credits plugin, its own admission hook)
insert into sandboxes (…) …;  insert into operations (…) …;  insert into jobs (…) …;
commit;
```

- The profile row lock serializes creates and starts of **one** profile only; different profiles never wait on each other.
- Counting from `sandboxes` with a partial index on `(profile_id, cluster_id) where status in (…)` stays fast at thousands of sandboxes per profile; no counters to drift.
- Statuses that leave the active set (stopped, deleted, failed) free capacity at once, in the transaction that sets them.
- **Lowering a limit never stops anything.** It refuses new creates and starts until usage is under it. (Budgets and credits are the exception, Beta.)
- Starts recheck totals but not `storage_gb`, which the sandbox already counts in, so a lowered storage limit refuses new persistent sandboxes, never starts.

### Template limits

Enforced in [templates](templates.md) on create and rebuild, with the profile row locked:

- `templates`: count of the profile's templates not deleted.
- `template_storage_mb`: sum of `size_mb` of the profile's builds that are kept (ready, not yet garbage-collected), each counted on its own cluster; scoped rows count only that cluster's builds. A build's size is known only after it's built, so the check is before; one build may take the profile over, after which creates and rebuilds are refused until something is deleted. The refusal says so.

### The refusal

```json
{
  "message": "This profile can run at most 3 sandboxes at once on eu-west. Stop one, or ask for a higher limit.",
  "code": "limit_exceeded",
  "limit": { "kind": "running_sandboxes", "period": null, "cluster": { "id": "clu_…", "name": "eu-west" },
             "value": 3, "used": 3, "requested": 1 }
}
```

`429` with `limit_exceeded` is never retried by the SDKs (retrying won't help until something stops). `used` and `requested` are new and let a UI say "3 of 3 in use".

## API

Management (`/v0/projects/{prj}/profiles/{prf}/limits`; owner/admin or admin key with `profiles.write`):

| Method | Path | What |
|---|---|---|
| `GET` | `…/limits` | `{ data: [{ kind, period, cluster, value }] }` |
| `PUT` | `…/limits` | `{ limits: [...] }` replaces all; `[]` removes all; answers the new set |
| `PATCH` | `…/limits` | `{ set: [...], remove: [{kind, period, cluster}] }`: change some without resending all (new; Lewagent replaces whole sets today, which races with dashboard edits) |
| `GET` | `…/usage` | Current totals against each limit: `{ kind, period, cluster, value, used }`, so a dashboard shows bars without summing sandboxes itself |

Consumer: `GET /v0/profile` lists `limits` (with `cluster`) as the key sees them: cluster-scoped limits for clusters the key doesn't reach are left out. `GET /v0/profile/usage` (new) gives the same `used` view.

### Errors

| Status | Code | When |
|---|---|---|
| `422` | `validation_failed` | Unknown kind; a `period` on a kind that takes none or missing on `compute_units`; a cluster scope on a kind that takes none; a cluster not in the project; the same kind, period and cluster twice; negative value |
| `429` | `limit_exceeded` | A create, start, prompt or template build over a limit; carries `limit` |

## Security

- Only owners, admins and admin keys with `profiles.write` change limits; a profile key reads them.
- Limits are enforced in the database transaction that commits the work, so a fast client can't slip past them with concurrent requests.

## Observability

- Audit: `profile.limits_replaced` / `profile.limits_changed` with before and after.
- Activity: a refused create or start is logged as `request.refused` with `code: limit_exceeded` and the limit, so a project's support can see why a customer was refused ([activity-and-webhooks/activity-log](activity-and-webhooks/activity-log.md)).
- Metrics: `lewbox_limit_refusals_total{kind}`.

## Testing

- Each kind refused at exactly its value, allowed below.
- Cluster-scoped and profile-wide limits together; per-sandbox caps higher and lower on a cluster.
- Concurrency: 50 parallel creates against `running_sandboxes = 3` make exactly 3 (run against real Postgres).
- Defaults copied on create; `limits` on create replacing them.
- Lowering a limit doesn't stop anything; starts don't check storage.
- Template limits on create and rebuild.

## Build plan

In `plugins/limits/` (types, validation, resolution, the admission hook), called by core through the admission and defaults extension points from `internal/sandboxes` (reserve, start), `internal/templates` and `internal/agents`.

1. Table and `GET/PUT/PATCH` endpoints; defaults on profile create.
2. Size and timer resolution (pure functions, table-tested).
3. The reserve/start check in the sandbox transaction, with the partial indexes.
4. Template limits.
5. `conversations_running`.
6. Usage endpoints.

## Open questions

1. **`stopping` counting as running.** It prevents double-booking during slow stops but may surprise a user who just stopped one. Keep it?
2. **Per-cluster per-sandbox caps above the profile-wide cap.** Useful for GPU clusters, but less obvious than "the lowest wins". Which rule?
3. **`conversations_running` in the alpha?** Or wait for evidence it's needed beyond sandbox memory?
4. **Units in the API.** Keep millicores and seconds (exact, as documented), or accept friendlier strings (`"2"` vCPUs, `"30m"`) too?
