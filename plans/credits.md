# Credits

> Status: Draft · Milestone: Beta (not in the alpha, D7) · Plugin: `credits` (requires `limits`) · Updated: 2026-10-29

Lewbox measures what running sandboxes use, converts it to **compute units** at the project's own rates, and, for profiles that use credits, spends those units from **credit buckets**: a renewing allowance and one-off top-ups. When a profile's credits run out or it reaches a **budget**, its running sandboxes are stopped and new ones are refused. Lewbox never handles money and bills nobody: a project prices compute units and bills its customers itself, adding credits through the API from its own billing system (e.g. a Stripe webhook). That's why this is called *credits*, not billing (decided 2026-10-29).

The alpha meters nothing and enforces only [limits](limits.md) on size and concurrency. This is planned in full so the alpha's data model has room for it: sandboxes record when they ran, at what size, from the start.

| Plan | What it covers |
|---|---|
| [credits/metering](credits/metering.md) | Runs: when a sandbox is metered, at what size and rate; rates; turning runs into compute units exactly; stops Lewbox learns about late; offline nodes |
| [credits/balances](credits/balances.md) | Buckets, the ledger, spending order, charging usage without a 5-second loop, renewals, top-ups, adjustments, running out, verifying balances |
| [credits/budgets-and-usage](credits/budgets-and-usage.md) | `compute_units` budgets per day and month, warnings, usage reporting and its endpoints, hourly roll-ups |

---

## Plugin

Credits are the **`credits` plugin** ([plugins](plugins.md)), shipped and compiled in by default but off until a project turns it on. It requires the [`limits`](limits.md) plugin, as budgets are limits of the `compute_units` kind. Disabling it never deletes balances or the ledger; it stops checking credits on create and start, and refuses the credit endpoints with `plugin_disabled`. It uses these core extension points ([plugins/extension-points](plugins/extension-points.md)):

| Extension point | What credits does with it |
|---|---|
| Admission (in the create and start transaction) | Refuses with `402 credits_exhausted` or `429 limit_exceeded` (budget) |
| Lifecycle (sandbox started, stopped, deleted; node offline and back) | Opens and closes runs; checkpoints usage; reschedules the exhaustion job |
| Jobs and periodic jobs | `credits.checkpoint` (hourly), `credits.exhaust` (scheduled per profile), `credits.renew_buckets` |
| Migrations | `credit_buckets`, `credit_transactions`, `credit_transaction_lines`, `usage_hourly` (`sandbox_runs` is core, written by `sandboxes.Transition`, so usage history exists whether or not the plugin is on) |
| Routes, permissions, events, settings | `…/credit-buckets`, `…/credit-transactions`, `…/usage`; `profiles.credits`; `profile.credits_*` webhooks; the project's rates (`/v0/projects/{prj}/settings/credits`) |

Code lives in `plugins/credits/` (was planned as `internal/billing`).

## Today

Legacy built all three (`lewbox-legacy/docs/profiles.md` § *Usage is metered on reserved resources*, *Spending is measured in compute units*, *Credits are spent from two buckets*, *4. Usage metering*, *5. Enforce budgets and credits*; `app/Actions/Credits/*`, `app/Console/Commands/Sandbox/MeterSandboxes.php`, `app/Jobs/Credits/ResetSubscriptionBuckets.php`, `app/Console/Commands/Credits/VerifyBalances.php`):

- **Runs** (`sandbox_runs`): a run starts when the gateway reports the sandbox `running`, at the tenant's rates then, and ends the moment a stop or delete is asked for. Stored with Unix-second integers so cost is `rate × seconds / 3600` in plain SQL.
- **Rates** per tenant: `vcpu_hour_rate`, `memory_gib_hour_rate`, `disk_gb_hour_rate` in millionths of a credit per hour, and `free_disk_gb` (20) per sandbox.
- **Credits**: `credit_buckets` (subscription with an ISO 8601 interval or renewing manually; one-time top-ups), `credit_transactions` with `reference` unique per profile, `credit_transaction_lines` per bucket. Each running sandbox has a **live transaction** whose open line is rewritten every pass and **moved between buckets** as they run dry (`OpenUsageTransaction`, `MeterUsage`, `MoveUsageLines`, `SettleUsageTransaction`).
- **`sandboxes:meter` every 5 seconds**, on one server, meters, moves lines, and stops profiles out of credits or over a budget (`StopProfileSandboxes`).
- **`settled_balance`** per bucket kept by hand; `credits:verify-balances [--fix]` finds where it drifted.
- **Renewal** every 5 minutes (`ResetSubscriptionBuckets`).
- Not built: usage endpoints, `usage` and `credits` on `GET /v1/profile`, budget warnings, a minimum charge (`docs/todo.md` line 102), expiring top-ups, per-profile credits on/off.

What's wrong:

1. **A 5-second loop on one server** that rewrites every live transaction, needs a lock that once skipped every pass in production (`docs/todo.md` line 264), and is only as exact as its last pass.
2. **Lines moving between buckets** is the most intricate code in the app, and `settled_balance` can drift from the ledger.
3. **Stops are billed until noticed**: a sandbox AerolVM stopped on its idle timeout keeps billing until the reconciler sees it, up to a minute later.

## The new approach, in short

- **Runs stay the source of truth.** A run's cost is exact and computable at any instant from its rate and times. Nothing needs rewriting every few seconds to be right.
- **Charge at checkpoints, not on a loop.** Usage is charged to the ledger when something changes (a run starts or ends, credits are added, a bucket renews) and once an hour, in one transaction per profile, split across buckets in spending order. Between checkpoints, a balance is *read* as "stored balance minus what's accrued since", exactly.
- **Schedule the moment credits run out.** A profile's spend rate is known, so the instant its balance reaches zero (or it reaches a budget) is known too. A job is scheduled for that instant and rescheduled when anything changes. No polling; enforcement within a second instead of five.
- **End runs when the sandbox actually stopped.** Our AerolVM fork reports when it stopped a sandbox ([clusters/aerolvm-fork](clusters/aerolvm-fork.md)), so runs end at the true time, not when Lewbox noticed.

## Who sees what

| | Admin key / dashboard | Profile key |
|---|---|---|
| Rates | Read and change (project settings) | |
| A profile's buckets, ledger, adjustments | ✓ | Balance and buckets (read), on `GET /v0/profile` |
| Usage | Whole project, per profile, per sandbox | Its own |
| Budgets | Set (as limits) | Read (as limits) |

## Build order (Beta)

1. Rates and runs (metering) — runs are recorded from M1 so usage history exists when the credits plugin ships; only the conversion and endpoints wait.
2. Usage roll-ups and endpoints.
3. Buckets, the ledger and checkpoints.
4. Scheduled exhaustion, budgets and warnings.
5. Dashboard pages: project rates, profile credits, ledger, usage charts.

## Open questions

1. **Do runs start in the alpha?** Proposed: yes, `sandbox_runs` is written from M1 (cheap, and the history is wanted later), even though nothing reads it until Beta.
2. **Minimum charge** per run (`docs/todo.md` line 102): a per-project setting, e.g. 60 seconds? Proposed in [metering](credits/metering.md#minimum-charge).
3. **Stopped storage**: charge snapshot storage per GB-month? Legacy deliberately didn't; projects pay their bucket directly.
4. **Measured rather than reserved usage** (CPU time, working set): later, when nodes report per-sandbox stats.
