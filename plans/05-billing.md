# Phase 5: prices, credits and automatic suspension

**Goal:** the operator can put a price on compute and either just show customers what they've spent, or require prepaid credits and stop customers who run out. No payment provider: credits are added by the operator (console, CLI or API from their own billing system). See DECISIONS D12.

## Money

All amounts are integers in **micro-units** of the instance currency (1 000 000 = 1.00). The currency is a display label (`USD`, `EUR`, `credits`…). JSON fields carry `_micros` in their name; the UI and CLI format them with two decimals.

## Settings (added to Phase 1's settings)

```json
{"currency":"USD",
 "price_vcpu_hour_micros":50000,        // $0.05 per vCPU-hour (E2B/Daytona level)
 "price_memory_gib_hour_micros":16000,  // $0.016 per GiB-hour
 "billing":"off",                       // off | show | enforce
 "signup_credits_micros":0}             // one-time credit for new sign-ups
```

- `off`: no prices anywhere.
- `show`: usage shows cost; balances are tracked but nobody is stopped.
- `enforce`: an organization whose balance is ≤ 0 is suspended (`status_reason: credits`); it's restored automatically when its balance goes above 0.

## Schema (`00010_billing.sql`)

```sql
ALTER TABLE plans ADD COLUMN monthly_credits_micros INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_hourly ADD COLUMN charged_at DATETIME;

CREATE TABLE credit_txns (                 -- append-only ledger
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    amount     INTEGER NOT NULL,           -- micro-units, + credit / − charge
    kind       TEXT NOT NULL,              -- grant | usage | monthly | signup
    reference  TEXT NOT NULL UNIQUE,       -- makes every write idempotent
    note       TEXT NOT NULL DEFAULT '',
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX credit_txns_org ON credit_txns(org_id, created_at);
```

Balance = `SUM(amount)` for the organization. References:

| kind | reference | when |
|---|---|---|
| `usage` | `usage:<org>:<cluster>:<hour RFC3339>` | each closed hour of `usage_hourly` with cost > 0, charged by the sync pass after the hour ends |
| `monthly` | `monthly:<org>:<YYYY-MM>` | first sync pass of each calendar month (UTC) for orgs whose plan has monthly credits; also right after sign-up/plan change if this month's grant is missing |
| `signup` | `signup:<org>` | at sign-up when `signup_credits_micros > 0` |
| `grant` | caller's `reference`, or `grant:<txn id>` | operator adds or removes credits |

`cost = floor(vcpu_seconds × price_vcpu_hour / 3600 + memory_mb_seconds × price_memory_gib_hour / (1024 × 3600))`. Charging runs only when `billing` isn't `off`; when billing is turned on, hours before that moment are marked charged without a transaction (no surprise back-charges). The insert and the `charged_at` update happen in one transaction (add a `*sql.DB` to the service for this; it currently only has `*db.Queries`).

## Enforcement (in the sync pass, `enforce` only)

- Active org with balance ≤ 0 → `status = suspended`, `status_reason = credits` → Phase 3 blocks its quota and stops its sandboxes.
- Suspended org with `status_reason = credits` and balance > 0 → `status = active`, reason cleared.
- Turning `billing` away from `enforce` restores every `credits`-suspended org.
- Admin suspensions (`status_reason = admin`) are never touched by billing.

## API

| Route | Who | What |
|---|---|---|
| `GET /v0/credits` | members (their org), org API key, instance admins | `{"currency","balance_micros","month_spend_micros","unbilled_micros","monthly_credits_micros","data":[txn…]}` (newest 100) |
| `POST /v0/credits {"amount_micros","note","reference"}` | instance admins | Adds a ledger entry (negative removes credits); `409` if the reference exists |

`unbilled_micros` is the cost of the current, not yet charged hours (an estimate for the UI). Usage responses (Phase 3) gain `cost_micros` per bucket and in totals when billing isn't `off`. Organization responses gain `balance_micros` when billing isn't `off`. Plans gain `monthly_credits_micros`. `/v0/instance` gains `billing` and `currency` so the portal knows whether to show the Billing page.

## CLI

```
lewbox credits [--org acme]
lewbox credits add 25 --org acme --note "Invoice 1042"   # 25.00 in the currency; --reference optional
lewbox settings set --billing enforce --price-vcpu-hour 0.05 --price-memory-gib-hour 0.016 --currency USD --signup-credits 5
lewbox plan update pro --monthly-credits 50
```

## Done when

- Tests: cost math (rounding down), hourly charging idempotent across passes and restarts, no charge for the open hour, monthly grant once per month, signup credit, enforce suspend/restore, admin suspension untouched, switching billing modes, `POST /v0/credits` admin-only and idempotent by reference, scoping.
- Parity tests; docs: `docs/accounts/billing-and-credits.md` (operator) and the customer page mentions credits; references updated.
- The four Go checks pass.
