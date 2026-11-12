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
lewbox org list --status pending
lewbox org approve acme                        # status: active
lewbox org suspend acme                        # status: suspended, reason "admin"
lewbox org restore acme
```

An override takes a positive whole number or `plan`; `unlimited` is refused, because an organization cannot exceed its plan. A limit of `0` or less is refused everywhere.

`subscription remove` revokes the organization's live sandbox keys in that region (it refuses unless you pass `force` if a revoke fails), pushes the blocking quota and stops its running sandboxes there. Its sandboxes and usage history stay; you or the customer can delete them.

Only instance admins change a subscription or an organization's status. Owners and admins still rename their organization and manage members and keys. A `pending` or `suspended` organization is readable by its members but cannot be managed: no new keys, members or settings until it is active.

An organization also has a **team** (`lbx-…`) for AerolVM, derived from its ID and never reused. Only instance admins see the team.

## Regions

Customers place sandboxes in **regions**: the clusters that are ready or degraded. Instance admins see every one; a customer sees only the regions its organization is subscribed to, with the plan and limits there. A signed-in customer with no organization chosen sees none. The route carries no admin URL, token, node or version.

```bash
lewbox region list --org acme
```

A sandbox key can only be created for a subscribed region. Without one, the request fails with `412` and the message "your account has no plan in region …; ask your provider".

## How limits are enforced

Each region's AerolVM **team quota** is exactly that subscription's limits; there is no cross-region headroom. On each sync pass (every 15 seconds) Lewbox pushes the quota:

- an active, subscribed organization gets its subscription's limits;
- an organization that is not active, or has no subscription in that region, gets a **blocking quota**;
- when all three limits are unlimited, Lewbox deletes the quota.

A blocking quota (`max_cpu: 0.001`, `max_memory_mb: 1`) is used because AerolVM reads a quota of `0` as "no limit". Lewbox only touches regions the organization has used (a quota state, a live key or a mirrored sandbox), so a region it never used is left alone.

AerolVM enforces the quota atomically, so a create that would exceed it fails with `429 team_quota_exceeded`. Lowering a limit never removes a running sandbox. Suspending an organization pushes the blocking quota and the next sync stops its running sandboxes; the sync also stops sandboxes in a region the organization is no longer subscribed to.

Quotas are only pushed when they change, when the last push failed, or after ten minutes; a subscription change pushes at once. What Lewbox last pushed and any error is stored per organization and region. `lewbox limits` shows it.

## What customers see

| | Instance admin | Org owner/admin | Org member/viewer | Org API key |
|---|---|---|---|---|
| Clusters, nodes, accounts, plans, settings | yes | no | no | no |
| Subscribed regions, with plan and limits | all | yes | yes | yes |
| Subscriptions of the organization | all | read | read | read |
| Assign or remove a subscription | yes | no | no | no |
| Change the organization's status | yes | no | no | no |

## Next

- [Organizations and API keys](organizations-and-api-keys.md)
- [HTTP API](../reference/http-api.md#subscriptions)
