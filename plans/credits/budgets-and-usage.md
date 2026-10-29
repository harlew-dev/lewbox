# Budgets and usage

> Status: Draft · Milestone: Beta · Plugin: `credits` · Updated: 2026-10-29

A **budget** caps how many compute units a profile may spend in a day or a calendar month, whether or not it uses credits: a runaway sandbox can't spend a large top-up in an afternoon. **Usage** reports what was spent, and how many vCPU-, memory- and disk-hours ran, per profile, sandbox, cluster and time period, for the project's dashboards and its customers' own.

Budgets are limits of kind `compute_units` ([limits](../limits.md) owns the limit model and the API for setting them); this plan covers how they're measured and enforced, warnings, and usage reporting.

---

## Today

Legacy (`lewbox-legacy/docs/profiles.md` § *5. Enforce budgets and credits*; `app/Enums/Profile/LimitKind.php`, `LimitPeriod.php`; `EnsureProfileCanRun::spendingRefusal()`; `SandboxRun::rateSecondsSql()`):

- `profile_limits` rows of kind `compute_units` with `period` `day` or `month` (UTC calendar), unique per (profile, kind, period), so a profile can have both.
- Checked before every create and start under the profile lock; checked every 5 seconds by `sandboxes:meter`, summing run costs overlapping the period; reaching one stops the profile's running sandboxes (reason `limit_exceeded`) and refuses creates and starts with `429 limit_exceeded` (carrying the `limit`) until the period ends.
- **Planned, not built**: warnings at 80% of a budget (a `budget_warning` tenant notification, once per budget per period), usage endpoints, `usage` on `GET /v1/profile` (documented at [lewbox.com/docs/profile/credits-and-usage#usage](https://lewbox.com/docs/profile/credits-and-usage)), hourly roll-ups (`profile_usages`) for charts, Lewagent's "usage endpoints (hours run per computer)" (`lewagent-legacy/docs/todo.md`).

## Goals

- Budgets enforced at the instant they're reached, scheduled like credit exhaustion, without a loop.
- Warnings, once per budget per period, as webhooks and dashboard notifications.
- Usage readable live for any period, and cheaply over long ones through hourly roll-ups.
- The same numbers everywhere: dashboard, Admin API, profile key, webhooks.

### Non-goals

- Budgets in money.
- Budgets per sandbox (that's `lifetime` and size limits).
- Per-cluster budgets (an open question; Lewagent asked for per-cluster *limits*, [limits](../limits.md)).

## Design

### Measuring spend in a period

```
spent(profile, period_start, t) = Σ cost(run, max(started_at, period_start) → min(coalesce(ended_at, t), t))
                                   over runs overlapping [period_start, t)
```

From runs, exactly, at any `t` ([metering](metering.md)). For a long period it reads roll-ups for whole past hours plus runs for the current hour.

### Enforcing

For each budget, the instant it's reached is known while the profile's rate is constant:

```
reaches_at = t + (limit − spent(t)) × 3600 / rate
```

- `budget_meters` (profile, kind, period): `period_start`, `reaches_at`, `job_id`, `warned_at`, `reached_at`. Recomputed at every run start and end (the same hook as credit checkpoints) and when the limit changes.
- A unique job per budget at `reaches_at` (`budgets.reached`): recompute; if `spent ≥ limit`, in one transaction mark `reached_at`, request stops for running sandboxes (reason `limit_exceeded`, with the limit in the activity and webhook), raise `profile.budget_reached`.
- Create and start check every budget under the profile lock and answer `429 limit_exceeded` with `limit: {kind: "compute_units", period, value}`.
- At the next period start (a job at midnight UTC or the 1st), `reached_at` clears, and `profile.budget_reset` is raised. Nothing restarts on its own.
- A limit lowered below what's spent this period stops the profile at once; raised, it clears `reached_at`.
- The credits safety sweep also checks budgets ([credits](balances.md#running-out)).

### Warnings

- At `warn_at = t + (limit × threshold − spent) × 3600 / rate`, a job raises `profile.budget_warning` (threshold per project, default 80%), once per budget per period (`warned_at`).
- Shown in the project's notifications in the dashboard ([dashboard](../dashboard.md)).

### Roll-ups

`usage_hourly`:

| Column | Type |
|---|---|
| `hour` | timestamptz (start of the hour, UTC) |
| `project_id`, `profile_id`, `sandbox_id`, `cluster_id` | fk |
| `seconds` | int — seconds running in the hour |
| `vcpu_seconds` | bigint — millicore-seconds / 1000 |
| `memory_mb_seconds`, `disk_gb_seconds` | bigint |
| `compute_units` | bigint — micro-units |

Primary key (`hour`, `sandbox_id`). Written by a leader job a minute after each hour ends, from runs overlapping it (idempotent upsert, so a rerun or a late-ended run fixes it: the job also re-rolls hours touched by runs whose `ended_observed_at` is in the last hour). Partitioned by month; kept 13 months by default (`LEWBOX_USAGE_RETENTION`). Daily and monthly figures sum hours.

### Usage reports

One query shape for every caller:

| Parameter | |
|---|---|
| `from`, `to` | ISO 8601; `to` exclusive; at most 13 months apart |
| `granularity` | `hour`, `day`, `month` (UTC) |
| `group_by` | Any of `profile`, `sandbox`, `cluster` (project-level); `sandbox`, `cluster` (profile-level) |
| `profile`, `sandbox`, `cluster` | Filters |

Answer:

```json
{
  "data": [
    { "start": "2026-10-29T00:00:00Z", "end": "2026-10-30T00:00:00Z",
      "sandbox": "sbx_Qm4pL0sX2bN8cV",
      "hours": "7.50", "vcpu_hours": "30.00", "memory_gib_hours": "90.00", "disk_gb_hours": "75.00",
      "compute_units": "123.45" }
  ],
  "totals": { "hours": "7.50", "compute_units": "123.45" },
  "meta": { "live_until": "2026-10-29T18:42:00Z" }
}
```

Past hours come from roll-ups; the current hour from runs, live (`live_until`). Decimal strings, two decimal places for display, with `precision=micro` for raw micro-units.

`format=csv` exports the same rows.

### On the profile

`GET /v0/profile` (and the admin profile object) gains `usage`:

```json
"usage": [
  { "period": "day",   "start": "2026-10-29T00:00:00Z", "compute_units": "41.20" },
  { "period": "month", "start": "2026-10-01T00:00:00Z", "compute_units": "803.90" }
]
```

and each `compute_units` limit gains `spent` and `reached`.

## API

| Method and path | Key | Does |
|---|---|---|
| `GET /v0/profile/usage` | profile | The profile's usage |
| `GET /v0/projects/{prj}/usage` | admin | Whole project |
| `GET /v0/projects/{prj}/profiles/{prf}/usage` | admin | One profile |
| `GET /v0/sandboxes/{sbx}/usage` | profile, sandbox key | One sandbox (hours run, units), for Lewagent's "hours run per computer" |

Budgets are set as limits: `PUT /v0/projects/{prj}/profiles/{prf}/limits` ([limits](../limits.md)).

Errors: `422 validation_failed` (range too long, bad granularity), `429 limit_exceeded` on creates and starts.

## Security

- A profile key sees only its own usage; a sandbox key only its sandbox's.
- Units only, never rates, for profile keys.

## Observability

- Webhooks: `profile.budget_warning`, `profile.budget_reached`, `profile.budget_reset`.
- Metrics: `lewbox_budget_reach_lag_seconds`, `lewbox_usage_rollup_duration_seconds`, `lewbox_usage_rollup_lag_seconds`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_USAGE_RETENTION` | `13mo` | Roll-ups |
| `LEWBOX_BUDGET_WARNING_THRESHOLD` | `0.8` | Default for new projects |

## Testing

- `spent` from roll-ups plus live runs equals `spent` from runs alone, for random runs and periods.
- Budget reached at the predicted instant; lowered limits; period rollover.
- Roll-up re-runs fix late-ended runs.
- Warning once per period.

## Build plan

In `plugins/credits/` ([plugins](../plugins.md)). Budgets are `compute_units` limits, stored and checked by the `limits` plugin ([limits](../limits.md)); the credits plugin meters them and schedules their jobs.

1. Roll-ups and the usage query (needs only runs, so it can come before credits).
2. Usage endpoints and `usage` on the profile.
3. Budget meters and jobs; warnings; webhooks.
4. Dashboard charts.

## Open questions

1. **Project timezone** for day and month periods, rather than UTC?
2. **Per-cluster budgets** and limits (Lewagent's seats per location)?
3. **Budget scope**: also a project-wide budget across all profiles, for self-hosters protecting their own hardware?
