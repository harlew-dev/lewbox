# Metering

> Status: Draft · Milestone: Beta (runs recorded from M1) · Plugin: core (`sandbox_runs`) and `credits` (rates, cost) · Updated: 2026-10-29

A running sandbox uses what it **reserved**: its vCPUs, memory and disk, for every second it runs. Lewbox records each stretch of running as a **run**, with the sandbox's size and the project's rate at the time, and converts runs into **compute units**. Everything else in [credits](../credits.md), credits and budgets and usage, reads runs.

---

## Today

Legacy (`lewbox-legacy/docs/profiles.md` § *Usage is metered on reserved resources*, *4. Usage metering*; `app/Models/SandboxRun.php`, `app/Actions/Sandboxes/StartSandboxRun.php`, `EndSandboxRun.php`, `RestartSandboxRuns`, `EndOfflineSandboxRuns`):

- `sandbox_runs`: `sandbox_id`, `profile_id`, `cpu` (millicores), `memory_mb`, `disk_gb` as reserved, `rate` in millionths of a credit per hour at the tenant's rates when the run started, `started_at`/`ended_at` as Unix seconds. Cost is `rate × seconds / 3600`, rounded down (`SandboxRun::costAt()`, `costSql()`).
- A run starts when the gateway reports `running` (`PATCH /api/gateway/v1/sandboxes/{sbx}` status running) and ends the moment a stop or delete is asked for, so saving files isn't billed.
- Rates are `vcpu_hour_rate`, `memory_gib_hour_rate`, `disk_gb_hour_rate` and `free_disk_gb` on the tenant. Changing them ends every running run and starts a new one at the new rate (`RestartSandboxRuns`), so rates aren't retroactive.
- Stops AerolVM makes itself (idle timeout, lifetime) end the run when the reconciler reports them, within about a minute.
- Nodes marked offline after 3 minutes without a heartbeat end their sandboxes' runs (`EndOfflineSandboxRuns`).

## Goals

- Exact: a run's cost at any instant is a pure function of its row and the clock.
- Runs recorded from the alpha, in the same transaction as the status change that starts or ends them, so history can't drift from what happened.
- Runs end when the sandbox really stopped, not when Lewbox noticed.
- Cheap: no per-sandbox work on a timer.

### Non-goals

- Measuring actual CPU or memory use.
- Charging for stopped storage or network (both noted as later).

## Design

### Units

| Quantity | Stored as |
|---|---|
| Compute units | `bigint` **micro-units** (millionths), shown to two decimal places |
| Rates | micro-units per hour, `bigint` |
| vCPU | millicores, `int` (AerolVM takes fractional CPUs) |
| Memory | MB, `int` |
| Disk | GB, `int` |
| Times | `timestamptz`, to the microsecond |

A run's rate:

```
rate = vcpu_hour_rate   × cpu_millicores / 1000
     + memory_gib_hour_rate × memory_mb / 1024
     + disk_gb_hour_rate × max(0, disk_gb − free_disk_gb)
```

computed in integer arithmetic (each term rounded down), stored on the run, never recomputed.

A run's cost at time `t`:

```
seconds = extract(epoch from (least(coalesce(ended_at, t), t) − started_at))
cost    = floor(rate × seconds / 3600)         -- numeric, then bigint
```

The same function exists in Go (`credits.RunCost(run, t)`) and SQL (`credits_run_cost(run, t)`), tested to agree.

### Table

`sandbox_runs`:

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | |
| `sandbox_id`, `profile_id`, `project_id`, `cluster_id` | fk | Denormalized for reporting |
| `cpu_millicores`, `memory_mb`, `disk_gb` | int | As reserved |
| `rate` | bigint | Micro-units per hour |
| `rate_card_id` | fk | The project rates in force ([below](#rates)) |
| `started_at` | timestamptz | |
| `ended_at` | timestamptz | Null while running |
| `end_reason` | text | `stop_requested`, `delete_requested`, `idle_timeout`, `lifetime_reached`, `credits_exhausted`, `limit_exceeded`, `profile_suspended`, `node_lost`, `failed`, `rate_changed` |
| `ended_observed_at` | timestamptz | When Lewbox learned of it, if later than `ended_at` |

Indexes: (`profile_id`, `started_at`), partial on (`profile_id`) where `ended_at is null`, (`sandbox_id`, `started_at`). A partial unique index on (`sandbox_id`) where `ended_at is null`: a sandbox has at most one open run.

### When a run starts and ends

| Event | Run |
|---|---|
| A create or start operation reaches step `running` ([sandboxes/operations](../sandboxes/operations.md)) | Starts, at the moment the node said the sandbox was running |
| A stop, restart's stop, or delete is **asked for** (the operation row is inserted) | Ends, at that moment, `stop_requested` or `delete_requested`. So the time spent saving files is never metered |
| The reconciler finds a sandbox stopped or deleted by AerolVM (idle timeout, lifetime) | Ends at the time **AerolVM stopped it** (reported by our fork's lifecycle events, [clusters/aerolvm-fork](../clusters/aerolvm-fork.md)), else at the last time the sandbox was seen running, `ended_observed_at` set to now |
| Lewbox stops it for credits, a budget, or a suspension | Ends when the stop is asked for, with that reason |
| Its node goes offline (no heartbeat for 3 minutes) | Ends at the node's `last_seen_at`, `node_lost`. If the sandbox turns out to be running when the node returns, a new run starts then (the gap isn't metered) |
| A create fails after `running` | Ends at the failure, `failed` |
| Project rates change | Every open run in the project ends now (`rate_changed`) and a new one starts now at the new rate, in one transaction |

Each start and end is written in the **same transaction** as the sandbox status change or operation insert that causes it, and calls the credits checkpoint hook ([credits](balances.md#checkpoints)).

### Restart

A `restart` operation ends the run when asked for and starts a new one when the sandbox is running again. Two runs, as with a stop and a start.

### Rates

Per project, versioned so a run can say which rates it used:

`rate_cards`: `id`, `project_id`, `vcpu_hour`, `memory_gib_hour`, `disk_gb_hour`, `free_disk_gb`, `minimum_charge_seconds`, `effective_from`, `created_by`. The project's current card is its latest. A change inserts a new card and restarts open runs.

Defaults for new projects come from configuration (`LEWBOX_RATES_*`). The dashboard shows what a 2 vCPU, 4 GiB, 20 GB sandbox costs an hour at the rates being entered, as legacy's did.

### Minimum charge

Requested in legacy `docs/todo.md` line 102 ("start a sandbox, close it instantly, charged 0"). `minimum_charge_seconds` on the rate card (default 0): a run that ended shorter is charged as if it ran that long. Applied by `RunCost` once `ended_at` is set, so a live run's cost is never inflated mid-run.

### Usage before the credits plugin ships

The alpha records runs with `rate = 0` and no rate card (projects have no rates yet). When the credits plugin ships, rates take effect from then; past runs keep their zero rate but their sizes and durations still give vCPU-hours and GB-hours for usage reports ([budgets-and-usage](budgets-and-usage.md)).

## API

Runs aren't an API resource of their own; they surface as usage ([budgets-and-usage](budgets-and-usage.md)) and on a sandbox:

- `GET /v0/sandboxes/{sbx}` gains `usage: {compute_units, running_since}` (this run's cost so far, and all runs' total) — Beta.
- Rates: `GET`/`PUT /v0/projects/{prj}/rates` (admin key, dashboard), answering the current card and its `effective_from`.

## Security

- Rates are project settings; only project admins change them.
- A profile key sees its own usage and never the rates (a project may not want its customers to see unit prices; the profile sees units only).

## Observability

- Metrics: `lewbox_sandbox_runs_open{project}`, `lewbox_run_end_lag_seconds` (`ended_observed_at − ended_at`), `lewbox_compute_units_total{project}` (counter, incremented at checkpoints).
- Activity: none of its own; `operation` and `sandbox.stopped` events already carry the reasons.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_RATES_VCPU_HOUR` | `0` | Micro-units; new projects' default |
| `LEWBOX_RATES_MEMORY_GIB_HOUR` | `0` | |
| `LEWBOX_RATES_DISK_GB_HOUR` | `0` | |
| `LEWBOX_RATES_FREE_DISK_GB` | `20` | |

## Testing

- `RunCost` in Go and SQL agree on random runs, including minimum charges and runs spanning rate changes.
- Status transitions open and close runs exactly once (property test over random operation sequences against the fake cluster).
- Late-observed stops end at the fork's reported time.
- Node offline and return.

## Build plan

Runs are core (`internal/sandboxes`, written by `Transition`); rates, cost and charging are in the `credits` plugin, `plugins/credits/` ([plugins](../plugins.md)).

1. M1: `sandbox_runs` written on status changes (rate 0), with the one-open-run index.
2. Beta (`plugins/credits`): rate cards, rates API, `RunCost`, restarting runs on rate changes, minimum charge.
3. Fork: lifecycle events with stop times ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)).

## Open questions

1. **Disk while running above a free allowance** (legacy) vs disk always: keep legacy's?
2. **GPU rates** once GPU clusters exist (`gpu_hour` per vendor/model).
3. **Should a run end at the stop request, or at the stop's completion**, for stops Lewbox makes for credits? (Proposed: at the request, as for any stop.)
