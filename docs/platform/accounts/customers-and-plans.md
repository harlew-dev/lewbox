---
title: Customers, plans and limits
description: Let people sign up and create workspaces, give each workspace a plan in the region you choose, and control approvals, suspension and limits.
---

An operator can resell an instance. When sign-up is open, anyone can create an account and then a workspace of their own. A workspace is in at most one region, and a customer can use that region only while you have given it a plan there.

## Choose how people sign up

```bash
lewbox settings                                              # instance_name, signup, default_plan, default_region
lewbox settings set --signup approval
lewbox settings set --name "Acme Sandboxes" --default-plan free --default-region eu-west
lewbox settings set --default-plan none --default-region none
```

`signup` is one of:

| | |
|---|---|
| `open` | A new account can create a workspace, which is active at once |
| `approval` | A new account can create a workspace, which is `pending` until you approve it |
| `closed` | No sign-up; you create accounts yourself, or invite someone with a [setup link](#setup-links) (the default) |

Creating a workspace subscribes it to `default_plan` in the existing region named by `default_region`; an instance admin creating a workspace for someone does not apply them. With no default plan or region, it starts with no region until you assign one. The sign-in page reads the instance name and mode from the public `GET /v0/instance` route.

## Setup links

A setup link is a single-use invitation that gets someone in even while sign-up is `closed`. You create it for one plan in one region; the person opens it, registers, and creates a workspace on that plan in that region, active at once. Nothing else changes hands: you never create their account or workspace for them.

```bash
lewbox setup-link create --plan pro --region eu-west --expires 7d --note "Acme onboarding"
lewbox setup-link list
lewbox setup-link revoke <id>
```

The command prints the `lbs_…` token once and the `/setup/<token>` path to send, prefixed by the server URL when the CLI knows it. `--expires` defaults to `7d`, in whole hours from `1h` to `720h`. The console's setup-link form creates the same thing.

The public page shows the instance, plan and region **names**, never slugs or who claimed it. When the person signs up with the token, the link is claimed by their new account. Creating a workspace with the same token consumes it and subscribes the link's plan in its region; reusing it then fails with `400`. A link is `unused`, `claimed`, `used` or `expired`; `revoke` deletes an unused one, and a used link cannot be revoked (`409`). Each link works once, so hand out one per customer.

## Plans

A plan is a set of limits. A limit is a positive whole number, or not set, which means unlimited. Plans have no price; usage is recorded for charts and reporting only.

```bash
lewbox plan list
lewbox plan create pro --name "Pro" --max-sandboxes 20 --max-vcpus 16 --max-memory 32GB
lewbox plan show pro
lewbox plan update pro --max-sandboxes 50 --max-memory unlimited
lewbox plan delete old
```

`--max-memory` takes `4096`, `4096MB` or `4GB`; `unlimited` clears a limit. `0` and negative numbers are refused. The `free` plan is seeded. A plan can't be deleted while a subscription uses it or while it is the instance's default plan.

Sign-up is rate limited to 5 attempts per client IP per hour, separately from the sign-in budget.

## Subscriptions: a plan in the workspace's region

Only instance admins assign subscriptions. A workspace is in exactly one region, so it has at most one subscription, and that subscription may override the plan's limits. Moving it to another region means removing the current subscription first (`409` otherwise).

```bash
lewbox subscription list --org acme
lewbox subscription set eu-west --plan pro --org acme
lewbox subscription set eu-west --plan pro --max-sandboxes 10 --max-vcpus 4 --max-memory 8GB --org acme
lewbox subscription set eu-west --plan pro --max-sandboxes plan --org acme   # back to the plan's limit
lewbox subscription remove eu-west --org acme
lewbox org list --status pending
lewbox org approve acme                        # status: active
lewbox org suspend acme                        # status: suspended, reason "admin"
lewbox org restore acme
```

An override takes a positive whole number or `plan`; `unlimited` is refused, because an organization cannot exceed its plan. A limit of `0` or less is refused everywhere.

`subscription remove` revokes the organization's live sandbox keys in that region (it refuses unless you pass `force` if a revoke fails), pushes the blocking quota and stops its running sandboxes there. Its sandboxes and usage history stay; you or the customer can delete them.

Only instance admins change a subscription or an organization's status. Owners and admins still rename their organization and manage members and keys. A `pending` or `suspended` organization is readable by its members but cannot be managed: no new keys, members or settings until it is active.

An organization also has a **team** (`lbx-…`) for AerolVM, derived from its ID and never reused. Only instance admins see the team.

Removing a member, changing their role to viewer, disabling their account or deleting it revokes every sandbox key they created (in that workspace for member changes, everywhere for account changes). If a key cannot be revoked, the change is refused with `502` until it succeeds. The workspace's `lba_` keys and other members' keys are untouched.

## Regions

Customers place sandboxes in their workspace's **region**: a cluster that is ready or degraded. Instance admins see every one; a customer sees only the region its workspace is subscribed to, with the plan and limits there. A signed-in customer with no workspace chosen sees none. The route carries no admin URL, token, node or version.

```bash
lewbox region list --org acme
```

A sandbox key defaults to the workspace's region and can only be created there; another region is `404 region … not found`, and a workspace with no region is `412`. A subscription's quota is pushed before the workspace is created or a plan change returns; a key created in that region before the push succeeds gets `412` ("still being set up") until it does.

## How limits are enforced

Each region's AerolVM **team quota** is exactly that subscription's limits; there is no cross-region headroom. On each sync pass (every 15 seconds) Lewbox pushes the quota:

- an active, subscribed organization gets its subscription's limits;
- a workspace that is not active, or has no subscription in that region, gets a **blocking quota**;
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
