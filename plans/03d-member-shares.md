# Phase 3d: members' shares of a workspace's plan

**Asked for on 2026-11-12** (DECISIONS D25–D28). A workspace (the code's `organization`) has a plan in each region (Phase 3b). Inside the workspace, that plan is **split between members**: for example, a 4 GB plan with 4 members gives each member 1 GB. AerolVM enforces each member's share exactly, so the workspace can never go over its plan.

## How it's enforced

AerolVM can only cap a **team**. So each **member** of a workspace gets their own AerolVM team, and each member's share in a region is pushed as that team's quota. The workspace itself no longer has a team.

- Member team name: `lbx-` + the membership's ID as 32 hex digits. It's unique, valid as a team name, and never reused (a member who leaves and rejoins gets a new membership, so a new team).
- A member's sandbox keys are bound to the member's team. A member's keys see and touch only that member's sandboxes and snapshots (D26).
- In the portal, owners and admins see and stop everyone's sandboxes; members and viewers see only their own.

## Shares (per region, D27)

For each region where the workspace has a plan, and each kind (`sandboxes`, `vcpus`, `memory_mb`):

1. An owner or admin may set a member's **share** explicitly (a number, or `null` = automatic). Instance admins may too.
2. Members without an explicit share split **what's left** equally: `left = plan limit − Σ explicit shares`, divided by the number of automatic members.
   - `sandboxes` and `memory_mb` are whole numbers. The remainder goes one each to the earliest members (by membership creation time).
   - `vcpus` may be fractional and is rounded down to 0.01.
3. Setting explicit shares whose sum exceeds the plan's limit is refused (`409`: "shares add up to 6 GB but the plan in eu-west allows 4 GB").
4. If the plan is unlimited for a kind, shares for that kind are optional caps. Automatic members are unlimited for that kind.
5. A member whose share of any limited kind is 0 is **blocked** in that region: they get the blocking quota, can't create keys there (`412`: "you have no share of the plan in eu-west; ask your workspace owner"), and the portal says so.
6. Lowering the plan, or adding members, can make explicit shares add up to more than the plan. Shares are then scaled down proportionally when pushed (rounded down), and the workspace page warns "shares exceed the plan; they're scaled down" until an owner fixes them.

`viewer` members get no share and can't create keys (as today).

## Schema (`00013_member_shares.sql`)

```sql
CREATE TABLE member_shares (
    membership_id TEXT NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
    cluster_id    TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    max_sandboxes INTEGER,   -- NULL = automatic
    max_vcpus     REAL,
    max_memory_mb INTEGER,
    PRIMARY KEY (membership_id, cluster_id)
);
ALTER TABLE sandbox_keys ADD COLUMN membership_id TEXT REFERENCES memberships(id) ON DELETE CASCADE;
ALTER TABLE sandboxes ADD COLUMN membership_id TEXT;      -- from the member's team; NULL when unknown
-- usage_hourly gains membership_id: rebuild it with
--   PRIMARY KEY (org_id, membership_id, cluster_id, hour), membership_id NOT NULL DEFAULT ''
```

Nothing has been released, so existing sandbox keys and mirrored sandboxes don't need to keep working across this change. The migration must apply cleanly on the dev database (existing rows may get `membership_id = NULL`/`''`). `organizations.team` stops being used. Drop it in this migration if SQLite allows it cleanly, otherwise leave it and stop reading it.

## Changes to existing behavior

- **Keys** (`POST /v0/sandbox-keys`) belong to a member. By default that's the caller. An instance admin or an org API key must pass `"member": "<email or membership id>"`. The key's team is the member's team. `SandboxKey` JSON gains `member: {email}` (an operator creator still shows as "operator").
- **Sync**: attribute sandboxes by team → membership → organization. Meter usage per member. Push quotas per member team per region (the member's share; blocking when the workspace isn't active, has no plan there, or the share is 0 — only on clusters that member used, as in 3b). Stop the sandboxes of suspended workspaces as before.
- **Removing a member** revokes their sandbox keys (fail closed, the same helper), pushes the blocking quota for their team, and stops their running sandboxes. Their sandboxes stay visible to owners/admins.
- **Sandboxes API**: `Sandbox` gains `member: {email}`. Members (not owners/admins) see only their own; stop/delete on someone else's is `404`. `?member=` filter for owners/admins.
- **Usage API**: `?member=` filter, and `members: {email: {vcpu_hours…}}` per bucket for owners/admins. Members see only their own usage.
- **Limits API** (`GET /v0/limits`): per region, the plan's limits, the workspace's total use, and `members: [{email, share: {sandboxes,vcpus,memory_mb}, explicit: {…}, used: {…}, blocked}]`. Members see only their own row.

## New API

| Route | Who | What |
|---|---|---|
| `PUT /v0/members/{ref}/shares/{region}` `{"max_sandboxes","max_vcpus","max_memory_mb"}` (number or null = automatic) | workspace owners/admins, instance admins | Set a member's explicit share. Validates the sum, pushes the affected members' quotas at once (best effort) |

CLI: `lewbox member share eu-west dev@acme.dev --max-memory 1GB --max-vcpus 0.5 --max-sandboxes auto [--org acme]`. `lewbox limits` shows the per-member table.

## Web

- Portal **Team** page: per region, a table of members with their share (auto or set), use now, and an edit action (owners/admins).
- Portal **Usage** and **Home** meters show the caller's own share for members, and the whole workspace plus per-member breakdown for owners/admins.
- Console customer page: the same per-region member table, read-only plus edit.
- Rename the customer-facing word **Organization** to **Workspace** throughout the UI (D28). The console's "Customers" list rows are workspaces.

## Done when

- Tests: equal split with remainders and fractional vCPUs; explicit shares and the sum check; unlimited kinds; zero share → blocked and key refused; scaling when the plan shrinks; member removal revokes, blocks and stops; key ownership (admin/API key must name a member); sandbox/usage/limits scoping for members vs owners; attribution through member teams; migration.
- `customers.sh`: customer a invites a second member (add an existing account, as today), the plan in the region is 2 sandboxes, each member gets 1 by default; member 2's key can create 1 sandbox and gets `429` on the 2nd; member 2 can't see a's sandbox through its key or through Lewbox; a (owner) sees both in Lewbox.
- Docs: `docs/accounts/customers-and-plans.md` and `docs/customers/*` explain shares in a few lines; CLI/API references.
