# Credits

> Status: Draft · Milestone: Beta · Plugin: `credits` · Updated: 2026-10-29

A profile that uses credits holds a balance of compute units in **buckets**: a **subscription** allowance that renews every interval (or when the project renews it), and **one-time** top-ups that last until used. Running sandboxes spend from them, allowance first, oldest first. When the balance reaches zero, the profile's sandboxes are stopped and creates and starts are refused until credits are added or the allowance renews. Every change is a transaction in a ledger.

This plan keeps legacy's model, which works, and replaces how usage is charged: no 5-second loop, no lines moving between buckets, no hand-kept balance that can drift.

---

## Today

Legacy (`lewbox-legacy/docs/profiles.md` § *Credits are spent from two buckets*, *4. Usage metering*, *5. Enforce budgets and credits*; `docs/admin/profiles/credits.md`; `app/Actions/Credits/{AddCreditBucket,AdjustCredits,ChargeCredits,MeterUsage,MoveUsageLines,OpenUsageTransaction,ResetBucket,SettleUsageTransaction}.php`):

- **Buckets** (`credit_buckets`, `cbk_`): `subscription` (priority 1: `allowance`, ISO 8601 `interval` such as `PT5H`, `P1D`, `P1M`, `renews_at`; or renewing manually with neither) and `one_time` (priority 2, one per top-up). One subscription per profile unless the tenant allows several. `settled_balance` per bucket.
- **Spending order**: priority, then oldest first. A charge the buckets can't cover takes nothing (`InsufficientCreditsException`) unless it allows an overdraft (usage does), which leaves the last bucket negative.
- **Ledger** (`credit_transactions`, `ctx_`; `credit_transaction_lines`): renewals, forfeits, top-ups, usage, adjustments; `reference` unique per profile (a billing provider's invoice or payment id), so a retried webhook can't add twice.
- **Usage**: a live transaction per running sandbox, rewritten every 5 seconds, its open line moved to the next bucket when one runs dry and back when a higher-priority one refills.
- **Renewal** (`ResetBucket`, every 5 minutes): meter first, forfeit what's left, add the allowance, move `renews_at` on by the interval from the due time; renewing by hand moves the anniversary to now. Changing allowance or interval takes `now`, `next_renewal` or `on` a date.
- **Running out**: balance ≤ 0 → `402 credits_exhausted` on create and start, and every running sandbox gets a `stop` within 5 seconds (`StopProfileSandboxes`), reason `credits_exhausted`. The profile isn't suspended.
- **Admin API**: `GET/POST /profiles/{prf}/credit-buckets`, `PUT …/{cbk}`, `POST …/{cbk}/renewals`, `GET/POST /profiles/{prf}/credit-transactions` (`credit_bucket`: a bucket id, `new`, or `auto`).
- `credits:verify-balances [--fix]`, unscheduled, because `settled_balance` can drift.

## Goals

- Same buckets, spending order, overdraft rule, renewal semantics, references, errors and webhooks.
- Exact balances at any instant, without writing anything to read them.
- Enforcement at the moment credits run out, not up to 5 seconds later, without a loop.
- One place balances change, so they can't drift; a verification that runs on its own and alerts.
- Credits on or off per profile.

### Non-goals

- Money, invoices, taxes, payment providers.
- Credits for anything but compute units (agent token costs are the agent provider's, billed to the profile's login).

## Design

### Tables

`credit_buckets`:

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | Public `cbk_…` |
| `profile_id`, `project_id` | fk | |
| `kind` | text | `subscription` or `one_time` |
| `allowance` | bigint | Micro-units; subscriptions |
| `interval` | text | ISO 8601 duration; null when renewing manually |
| `renews_at` | timestamptz | Null when renewing manually |
| `pending_change` | jsonb | A scheduled allowance or interval change: `{allowance, interval, applies_at}` |
| `expires_at` | timestamptz | One-time only; later (expiring top-ups) |
| `balance` | bigint | Micro-units, **as of the profile's last checkpoint**. Changed only by inserting ledger lines (trigger-enforced) |
| `created_at`, `closed_at` | timestamptz | A one-time bucket at zero is closed after a checkpoint, kept for the ledger |

`credit_transactions`:

| Column | Type | Notes |
|---|---|---|
| `id` | bigint | Public `ctx_…` |
| `profile_id`, `project_id` | fk | |
| `kind` | text | `renewal`, `forfeit`, `top_up`, `adjustment`, `usage` |
| `amount` | bigint | Signed micro-units; the sum of its lines |
| `description` | text | |
| `reference` | text | Unique per profile, nullable |
| `usage_from`, `usage_until` | timestamptz | For `usage`: the interval charged |
| `actor` | actor | Admin key, user, or Lewbox |
| `created_at` | timestamptz | |

`credit_transaction_lines`: `transaction_id`, `bucket_id`, `amount` (signed). A trigger `after insert` adds `amount` to `credit_buckets.balance`; nothing else updates `balance` (enforced by a column privilege on the application role, or a check in the trigger that the update comes from it). Lines are immutable.

`credit_meters` (one per profile using credits):

| Column | Type | Notes |
|---|---|---|
| `profile_id` | pk | |
| `charged_until` | timestamptz | Usage up to here is in the ledger |
| `rate` | bigint | Sum of open runs' rates, micro-units per hour |
| `exhausts_at` | timestamptz | When the balance reaches zero at this rate; null when `rate` is 0 or already ≤ 0 |
| `exhaust_job_id` | bigint | The scheduled check |

`profiles.credits_enabled` (bool, default false) turns the whole thing on for a profile.

### Reading a balance

Between checkpoints, nothing is written. A profile's live balance at `t`:

```
accrued(t) = Σ over open-or-recent runs of cost(run, charged_until → t)    -- exact, from runs
           ≈ rate × (t − charged_until) / 3600                              -- equal while no run started or ended since charged_until
balance(t) = Σ bucket.balance − accrued(t)
```

Since every run start and end is a checkpoint, `rate` is constant between checkpoints and the two lines agree. Per bucket, the accrued amount is applied in spending order (Go: `credits.Apply(buckets, accrued)`), so the buckets endpoint shows each one exactly, live.

### Checkpoints

`Checkpoint(profile, t)` charges usage from `charged_until` to `t`:

1. Lock the profile's meter row (`SELECT … FOR UPDATE`).
2. `amount = Σ cost(run, charged_until → t)` over runs overlapping the interval (SQL, exact, including runs that ended in it and minimum charges finalized in it).
3. If `amount > 0`: insert a `usage` transaction (`usage_from`, `usage_until`), with lines taking from buckets in spending order (priority, then `created_at`), each up to its balance, the remainder from the last bucket (overdraft). Triggers update balances.
4. `charged_until = t`; recompute `rate` from open runs; recompute `exhausts_at = t + balance × 3600 / rate` (rounded up to the second); reschedule the exhaust job ([below](#running-out)).

Checkpoints run:

| Trigger | Why |
|---|---|
| A run starts or ends ([metering](metering.md)), in its transaction | `rate` changes |
| A top-up, adjustment, renewal, forfeit or bucket change, in its transaction | Usage so far is charged to the buckets as they were, and spending order applies from now |
| The exhaust job fires | Enforcement |
| Hourly, for profiles with open runs (`credits.checkpoint_hourly`, leader-only, batched) | Keeps the ledger at hourly granularity and `accrued` small |

Usage transactions: at most a few per profile per hour with running sandboxes, plus one per start or stop. Legacy wrote one per run and rewrote it every 5 seconds.

Usage per sandbox isn't on the ledger (one usage transaction covers all of a profile's runs in its interval); it comes from runs ([budgets-and-usage](budgets-and-usage.md)). The transaction's `details` lists each run's share for the dashboard.

### Running out

- `exhausts_at` is scheduled as a unique job per profile (`credits.exhausted`, replaced whenever the meter changes).
- When it fires: checkpoint at now; if the balance is still ≤ 0, in one transaction: mark the profile `credits_exhausted_at`, request a `stop` operation for each running sandbox (reason `credits_exhausted`; runs end at the request), raise `profile.credits_exhausted`. A sandbox busy with another operation gets its stop when that finishes (the operation runner checks the profile's state on completion).
- Creates and starts check `balance(now) > 0` under the profile lock and answer `402 credits_exhausted` otherwise.
- Adding credits clears `credits_exhausted_at` (after its checkpoint) when the balance is positive again, and raises `profile.credits_restored`. Nothing restarts on its own.
- **Safety sweep** every minute (leader): profiles whose `exhausts_at` has passed by more than 30 s with runs still open, in case a job was lost. It should never find any; it logs and counts when it does.
- Overspend is at most the job's latency (sub-second) plus the stop request's commit, against legacy's 5 seconds.

### Warnings

- `profile.credits_low` once a profile's balance falls below `credits_low_threshold` (project setting, default 20% of its subscription allowance, or a fixed amount), scheduled the same way (the instant is known), once per renewal period.

### Renewals

- Each subscription with `renews_at` has a scheduled job at that instant (`credits.renew`, unique per bucket).
- `Renew(bucket, due)`: checkpoint at `due`; insert a `forfeit` transaction taking what's left (or, if negative, a `forfeit` of zero and the overdraft carried: the renewal clears it, as legacy's did); insert a `renewal` adding `allowance`; `renews_at = due + interval` (calendar-aware for months, from the due time so anniversaries hold); apply a `pending_change` whose `applies_at` ≤ due; schedule the next.
- A missed renewal (the instance was down) renews once, at its next anniversary after now, as legacy's did.
- Manual renewal (`POST …/renewals`, optional `reference`): renews now, and for a bucket with an interval moves the anniversary to now.
- Changing allowance or interval (`PUT …/{cbk}`) with `timing`: `now` (renew now with the new allowance), `next_renewal` (store `pending_change` for `renews_at`), or `on` a date (store `pending_change` with that `applies_at`, and the renewal job also runs then).

### Top-ups and adjustments

`POST /v0/projects/{prj}/profiles/{prf}/credit-transactions`:

| Field | |
|---|---|
| `amount` | Signed units, as a decimal string or number with up to 6 decimals |
| `credit_bucket` | A bucket id; `new` (positive only: opens a one-time bucket); `auto` (negative only: takes in spending order) |
| `description` | |
| `reference` | Unique per profile; a repeat answers `409 reference_used` with the original transaction |

Taking more than a bucket has is `422 insufficient_credits` (no overdraft for adjustments). Each is a checkpoint first.

### Verification

`balance` changes only through line inserts, so it can't drift through application code. A leader job daily (`credits.verify`) compares each bucket's `balance` with `Σ lines` and each transaction's `amount` with `Σ its lines`, emits `lewbox_credit_balance_mismatches` and logs any; `lewbox credits verify [--fix]` does the same by hand. It should always find nothing.

### Turning credits on and off

- On (`PATCH …/profiles/{prf}` `credits_enabled: true`): creates the meter with `charged_until = now`, so usage before it is free (as legacy's first bucket did).
- Off: a final checkpoint, then the meter is removed. Buckets and ledger are kept. Runs keep being recorded.

## API

Admin key or dashboard; all under `/v0/projects/{prj}/profiles/{prf}`:

| Method and path | Does |
|---|---|
| `GET /credits` | `{enabled, balance, buckets, exhausted, exhausts_at}` |
| `GET /credit-buckets` | Buckets in spending order, each with its live `balance` |
| `POST /credit-buckets` | A subscription: `allowance`, `interval` (or `renews_manually: true`), `renews_at`. `409 subscription_exists` unless the project allows several |
| `PUT /credit-buckets/{cbk}` | Change a subscription: `allowance`, `interval`/`renews_manually`, `timing` (`now`, `next_renewal`, `on` + `applies_at`) |
| `POST /credit-buckets/{cbk}/renewals` | Renew now; optional `reference` |
| `DELETE /credit-buckets/{cbk}` | Close a subscription (forfeits what's left) |
| `GET /credit-transactions` | The ledger, newest first; filters `kind`, `bucket`, `since`, `until`; each with `lines` |
| `POST /credit-transactions` | Top-up or adjustment |

Profile key: `GET /v0/profile` includes `credits: {balance, buckets: [{kind, remaining, renews_at}]} | null` (as [lewbox.com/docs/profile/credits-and-usage](https://lewbox.com/docs/profile/credits-and-usage)).

Amounts are decimal strings in responses (`"1250.50"`) to avoid float rounding; requests take strings or numbers.

### Errors

| Status | Code | When |
|---|---|---|
| 402 | `credits_exhausted` | Create or start with balance ≤ 0 |
| 409 | `subscription_exists` | A second subscription when not allowed |
| 409 | `reference_used` | A reference already used on this profile; carries the `transaction` |
| 422 | `insufficient_credits` | An adjustment taking more than there is |
| 422 | `credits_disabled` | Buckets or transactions on a profile without credits |

## Security

- Only admin keys and project admins change credits. Every change records its actor.
- References make retries from a project's own billing webhooks (e.g. Stripe) safe.
- Ledger rows are immutable; corrections are new adjustments.

## Observability

- Webhooks: `profile.credits_low`, `profile.credits_exhausted`, `profile.credits_restored`, `credits.renewed` ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md#event-catalogue)).
- `sandbox.stopping`/`stopped` with reason `credits_exhausted`.
- Metrics: `lewbox_credit_checkpoints_total{trigger}`, `lewbox_credit_exhaust_lag_seconds` (job fire time − `exhausts_at`), `lewbox_credit_sweep_catches_total` (should stay 0), `lewbox_credit_balance_mismatches`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_CREDITS_CHECKPOINT_INTERVAL` | `1h` | |
| `LEWBOX_CREDITS_SWEEP_INTERVAL` | `1m` | |

Per project: `allow_multiple_subscriptions`, `credits_low_threshold`.

## Testing

- Property test: random sequences of runs starting and ending, top-ups, renewals, adjustments and checkpoints at random times; the ledger's total usage always equals `Σ RunCost` over runs, and each bucket's balance equals its lines.
- Spending order and overdraft, across subscription and several top-ups.
- Exhaustion at the predicted instant (fake clock), rescheduled by a top-up arriving just before.
- Concurrent top-up and exhaustion: the profile lock means either the stop or the top-up wins cleanly.
- Renewal anniversaries across month ends (`P1M` from Jan 31), missed renewals.

## Build plan

In `plugins/credits/` ([plugins](../plugins.md)): its own migrations, jobs and routes, and an admission hook for the create and start check.

1. Tables, triggers, `credits.Apply`, `Checkpoint`.
2. Lifecycle hooks from metering (run start/end) and the admission hook for create and start.
3. Scheduled jobs: exhaust, renew, hourly checkpoint, sweep, verify.
4. API, webhooks, dashboard.
5. Data migration from legacy buckets and ledger ([migration](../migration.md)): live transactions settled at cut-over.

## Open questions

1. **Several subscription buckets**: keep the option (legacy has it) or simplify to one?
2. **Expiring top-ups** (`expires_at`): Beta or later?
3. **Balance precision shown to profiles**: two decimals (legacy) — and do profile keys see per-bucket detail or only the balance?
4. **A grace amount**: let a profile run slightly negative (e.g. one hour) before stopping, as some projects may prefer warnings to stops?
