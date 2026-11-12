---
title: Customers, plans and limits
description: Let people sign up, give each customer a plan in the regions you choose, and control approvals, suspension and limits.
---

An operator can resell an instance. When sign-up is open, anyone can create an account and gets an organization of their own. A customer can use a region only while you have given it a plan there.

## Choose how people sign up

```bash
lewbox settings                                              # instance_name, signup, default_plan, default_regions
lewbox settings set --signup approval
lewbox settings set --name "Acme Sandboxes" --default-plan free --default-regions eu-west,us-east
lewbox settings set --default-plan none --default-regions none
```

`signup` is one of:

| | |
|---|---|
| `open` | A new account is active at once and can use its organization |
| `approval` | The organization is `pending` until you approve it |
| `closed` | No sign-up; you create accounts yourself (the default) |

At sign-up the new organization is subscribed to `default_plan` in each existing region listed in `default_regions`. With no default plan or no regions, it starts with no plan anywhere until you assign one. The sign-in page reads the instance name and mode from the public `GET /v0/instance` route.

## Plans

A plan is a set of limits. A limit is a positive whole number, or not set, which means unlimited. Plans have no price; billing is usage-based.

```bash
lewbox plan list
lewbox plan create pro --name "Pro" --max-sandboxes 20 --max-vcpus 16 --max-memory 32GB
lewbox plan show pro
lewbox plan update pro --max-sandboxes 50 --max-memory unlimited
lewbox plan delete old
```

`--max-memory` takes `4096`, `4096MB` or `4GB`; `unlimited` clears a limit. `0` and negative numbers are refused. The `free` plan is seeded. A plan can't be deleted while a subscription uses it or while it is the instance's default plan.

Sign-up is rate limited to 5 attempts per client IP per hour, separately from the sign-in budget.

## Subscriptions: a plan in each region

Only instance admins assign subscriptions. A customer's organization has a plan in each region it may use, and each subscription may override the plan's limits.

```bash
lewbox subscription list --org acme
lewbox subscription set eu-west --plan pro --org acme
lewbox subscription set eu-west --plan pro --max-sandboxes 10 --max-vcpus 4 --max-memory 8GB --org acme
lewbox subscription set eu-west --plan pro --max-sandboxes plan --org acme   # back to the plan's limit
lewbox subscription remove us-east --org acme
lewbox member share eu-west dev@acme.dev --max-memory 1GB --max-vcpus 0.5 --max-sandboxes auto --org acme
lewbox org list --status pending
lewbox org approve acme                        # status: active
lewbox org suspend acme                        # status: suspended, reason "admin"
lewbox org restore acme
```

An override takes a positive whole number or `plan`; `unlimited` is refused, because an organization cannot exceed its plan. A limit of `0` or less is refused everywhere.

`subscription remove` revokes the organization's live sandbox keys in that region (it refuses unless you pass `force` if a revoke fails), pushes the blocking quota and stops its running sandboxes there. Its sandboxes and usage history stay; you or the customer can delete them.

Only instance admins change a subscription or an organization's status. Owners and admins still rename their organization and manage members and keys. A `pending` or `suspended` organization is readable by its members but cannot be managed: no new keys, members or settings until it is active.

Inside a workspace, each region's plan is split between its members. Every member has their own AerolVM team, derived from their membership ID and never reused. An owner or admin sets a member's share:

```bash
lewbox member share eu-west dev@acme.dev --max-sandboxes 2 --max-vcpus 0.5 --max-memory 1GB --org acme
lewbox member share eu-west dev@acme.dev --max-sandboxes auto --org acme   # split what is left equally
```

A member without an explicit share gets an equal part of what is left (sandboxes and memory in whole numbers, the remainder to the earliest members; vCPUs rounded down to 0.01). Explicit shares may not add up to more than the plan (`409`). A `viewer` has no share and cannot create keys. When a member's share of any limited kind is 0, they are blocked in that region until an owner gives them some.

Removing a member revokes their sandbox keys, blocks their team and stops their running sandboxes. Their sandboxes stay visible to owners and admins.

## Regions

Customers place sandboxes in **regions**: the clusters that are ready or degraded. Instance admins see every one; a customer sees only the regions its organization is subscribed to, with the plan and limits there. A signed-in customer with no organization chosen sees none. The route carries no admin URL, token, node or version.

```bash
lewbox region list --org acme
```

A sandbox key can only be created for a subscribed region. Without one, the request fails with `412` and the message "your account has no plan in region …; ask your provider".

## How limits are enforced

Each member's AerolVM **team quota** is exactly their share of the subscription; there is no cross-region headroom and no unused capacity shared between members. On each sync pass (every 15 seconds) Lewbox pushes the quota:

- an active, subscribed member gets their share;
- a viewer, or a member whose share of a limited kind is 0, gets a **blocking quota**;
- a member of a workspace that is not active, or that has no subscription in that region, gets a **blocking quota**;
- when every kind is unlimited for the member, Lewbox deletes the quota.

A blocking quota (`max_cpu: 0.001`, `max_memory_mb: 1`) is used because AerolVM reads a quota of `0` as "no limit". Lewbox only touches a member's team on regions the member has used (a quota state, a live key or a mirrored sandbox), so a region nobody used is left alone. If the plan shrinks or members are added so explicit shares add up to more than the plan, they are scaled down proportionally when pushed, and the workspace page says so until an owner fixes them.

AerolVM enforces the quota atomically, so a create that would exceed it fails with `429 team_quota_exceeded`. Lowering a limit never removes a running sandbox. Suspending an organization pushes the blocking quota and the next sync stops its running sandboxes; the sync also stops sandboxes in a region the organization is no longer subscribed to.

Quotas are only pushed when they change, when the last push failed, or after ten minutes; a subscription or share change pushes at once. `lewbox limits` shows the plan, each member's share and live use, and who is blocked.

## What customers see

| | Instance admin | Org owner/admin | Org member/viewer | Org API key |
|---|---|---|---|---|
| Clusters, nodes, accounts, plans, settings | yes | no | no | no |
| Subscribed regions, with plan and limits | all | yes | yes | yes |
| Subscriptions of the organization | all | read | read | read |
| Assign or remove a subscription | yes | no | no | no |
| Set a member's share | yes | yes | no | no |
| Change the workspace's status | yes | no | no | no |

## Next

- [Organizations and API keys](organizations-and-api-keys.md)
- [HTTP API](../reference/http-api.md#subscriptions)
