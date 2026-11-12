# Phase 3b: a plan per region (subscriptions)

**Changed on 2026-11-12 at the user's request** (DECISIONS D20). It replaces "one plan per organization, limits shared across regions" (D5, D10) with: **a customer is subscribed to a plan in each region separately, and only the operator assigns those subscriptions.** A customer can use a region only while it has a subscription there. Each region's AerolVM team quota is exactly that subscription's limits: no cross-region headroom maths.

Plans stay what they are: named limit sets (`max_sandboxes`, `max_vcpus`, `max_memory_mb`; `null` = unlimited, D14). Plans have no price (billing stays usage-based, Phase 5).

## Schema (`00011_subscriptions.sql`)

```sql
CREATE TABLE subscriptions (
    org_id        TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id    TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    plan_id       TEXT NOT NULL REFERENCES plans(id),
    max_sandboxes INTEGER,          -- per-subscription overrides; NULL = the plan's value
    max_vcpus     INTEGER,
    max_memory_mb INTEGER,
    created_at    DATETIME NOT NULL,
    PRIMARY KEY (org_id, cluster_id)
);
CREATE INDEX subscriptions_plan ON subscriptions(plan_id);
```

Data migration: every existing organization with a `plan_id` gets a subscription to that plan, with its overrides, on every existing cluster. Then drop `organizations.plan_id`, `max_sandboxes`, `max_vcpus`, `max_memory_mb` (SQLite `ALTER TABLE … DROP COLUMN`; drop the index/foreign key issues first if any).

Settings: `default_plan` stays (the plan new sign-ups get, nullable now: `null` = none) and gains `default_regions` (list of cluster slugs; `[]` = none). At sign-up, the new organization is subscribed to `default_plan` in each of `default_regions` that exists. With no default plan or no default regions, a new customer has no regions until the operator assigns one (the portal says so). Migration seeds `default_regions` = every existing cluster's slug.

## Rules

- **Keys**: `POST /v0/sandbox-keys` requires a subscription in that region (`412 precondition_failed`: "your account has no plan in region X; ask your provider"). The default region is the only *subscribed* region.
- **Quotas** (in the sync pass, per organization and cluster):
  - subscribed and the organization is `active` → the subscription's effective limits; all three unlimited → `DELETE` the quota;
  - not subscribed, or the organization isn't `active` → the blocking quota, but only on clusters where the organization has a quota state, a live sandbox key, or a mirrored sandbox (no need to touch clusters it never used);
  - a limit value can't be 0 (D14), so the "would be 0 → block" rule goes away with the headroom code. Delete `organizationQuota`'s cross-region logic and `sandboxUse`/`add` if nothing else needs them.
- **Unsubscribing** (`DELETE`): revoke the organization's live sandbox keys on that cluster (fail closed: if a revoke fails, refuse with `502` unless `force`), push the blocking quota, and stop its running sandboxes there. Its sandboxes and their usage history stay (the customer, or the operator, can delete them).
- **Sync** also stops running sandboxes of an organization on clusters where it has no subscription.
- **Regions** (`GET /v0/regions`): instance admins get every ready/degraded cluster (as now). Everyone else gets only the regions their organization (the `Lewbox-Organization` header, or the API key's org) is subscribed to, each with `plan: {slug, name}` and `limits: {sandboxes, vcpus, memory_mb}`. A signed-in customer without the header gets `[]`.
- **Limits** (`GET /v0/limits`): `{"regions":[{"slug","name","plan":{"slug","name"},"limits":{…},"used":{"sandboxes","vcpus","memory_mb"},"quota":{…}|null,"blocked":bool,"pushed_at","error"}]}` for subscribed regions only. Drop the top-level `limits`/`used` (there's no global limit any more).
- **Organization JSON**: remove `plan`, `limits`, `overrides`; add `regions: [{"slug","name","plan":{"slug","name"}}]` (subscribed regions, by slug). Admin-only fields stay admin-only.
- **Plans**: the `organizations` count becomes `subscriptions` (number of subscriptions on the plan). Deleting a plan in use by any subscription, or the default plan, is `409` (in the SQL statement, as now).
- `PATCH /v0/organizations/{ref}` loses `plan` and the `max_*` fields (they move to subscriptions). It keeps `name` and `status`.

## API (instance admins; the organization from the `Lewbox-Organization` header)

| Route | What |
|---|---|
| `GET /v0/subscriptions` | `{"data":[Subscription]}` for the organization; members of the org may read it too |
| `PUT /v0/subscriptions/{region}` `{"plan","max_sandboxes","max_vcpus","max_memory_mb"}` | Create or change the subscription (absent override = unchanged on change / null on create; `null` = use the plan's) and push the quota at once (best effort) |
| `DELETE /v0/subscriptions/{region}[?force=true]` | Unsubscribe, as above |

```json
// Subscription
{"region":{"slug":"eu-west","name":"EU West","endpoint_url":"…","runtime":"docker"},
 "plan":{"slug":"pro","name":"Pro"},
 "limits":{"sandboxes":20,"vcpus":40,"memory_mb":81920},
 "overrides":{"sandboxes":null,"vcpus":null,"memory_mb":null},   // instance admins only
 "created_at":"…"}
```

## CLI

```
lewbox subscription list [--org acme]
lewbox subscription set eu-west --plan pro [--max-sandboxes N|plan] [--max-vcpus N|plan] [--max-memory SIZE|plan] [--org acme]
lewbox subscription remove eu-west [--force] [--yes] [--org acme]
lewbox settings set --default-plan free --default-regions eu-west,us-east   # --default-plan none, --default-regions none
```

Remove `org update --plan/--max-*` and `org create --plan` (create takes `--plan` and `--region` instead? No: keep `org create` simple; assign with `subscription set`).

## Done when

- Tests: migration of existing data; sign-up gets the default subscriptions (and none when unset); keys refused without a subscription; quota per subscription (limits, unlimited → delete, not active → blocking, unsubscribed but used → blocking, never used → untouched); unsubscribe revokes keys, blocks, stops sandboxes, fail-closed without force; regions and limits scoped to subscriptions; plan counts and delete guard; admin-only writes.
- `deploy/dev-cluster/customers.sh` updated: the operator assigns the e2e plan in the region to customer a and b (or sets it as the default plan with `default_regions` = the region); add a step proving a customer without a subscription in the region can't create a key there.
- Docs: `docs/accounts/customers-and-plans.md` rewritten for per-region plans (short); `docs/customers/*` mention regions come from the provider; CLI/API references.
- The web app (separate task) follows.
