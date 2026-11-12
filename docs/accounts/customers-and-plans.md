---
title: Customers, plans and limits
description: Let people sign up, give each customer an organization on a plan, and control approvals, suspension and limits.
---

An operator can resell an instance. When sign-up is open, anyone can create an account and gets an organization of their own on a plan you choose. You approve, suspend and re-plan customers from the CLI or the API.

## Choose how people sign up

```bash
lewbox settings                                   # instance_name, signup, default_plan
lewbox settings set --signup approval
lewbox settings set --name "Acme Sandboxes" --default-plan free
```

`signup` is one of:

| | |
|---|---|
| `open` | A new account is active at once and can use its organization |
| `approval` | The organization is `pending` until you approve it |
| `closed` | No sign-up; you create accounts yourself (the default) |

The sign-in page reads the instance name and mode from the public `GET /v0/instance` route. The sign-up form posts to `POST /v0/auth/signup`, gets a browser session, and lands on its new organization. An account always signs up without instance-admin rights.

## Plans

A plan is a set of limits. A limit is a positive whole number, or not set, which means unlimited.

```bash
lewbox plan list
lewbox plan create pro --name "Pro" --max-sandboxes 20 --max-vcpus 16 --max-memory 32GB
lewbox plan show pro
lewbox plan update pro --max-sandboxes 50 --max-memory unlimited
lewbox plan delete old
```

`--max-memory` takes `4096`, `4096MB` or `4GB`; `unlimited` clears a limit. `0` and negative numbers are refused. The `free` plan is seeded. A plan can't be deleted while organizations use it or while it is the instance's default plan.

Sign-up is rate limited to 5 attempts per client IP per hour, separately from the sign-in budget.

## Organizations as customers

Each organization has one plan and may have its own overrides. The **limits** a customer sees are the effective ones: an override if it is set, otherwise the plan's value; `null` means unlimited. On an override, `null` means "use the plan's value".

```bash
lewbox org create acme --name "Acme Inc" --owner owner@acme.dev --plan free
lewbox org update acme --plan pro --max-sandboxes 10
lewbox org update acme --max-vcpus plan   # back to the plan's value
lewbox org list --status pending
lewbox org approve acme                        # status: active
lewbox org suspend acme                        # status: suspended, reason "admin"
lewbox org restore acme
```

Organization limits take a positive whole number or `plan`; `unlimited` is refused, because an organization cannot exceed its plan. A limit of `0` or less is refused everywhere: use `null` on the API, `plan` on the CLI.

Only instance admins change a plan, limits or status. Owners and admins still rename their organization and manage members and keys. A `pending` or `suspended` organization is readable by its members but cannot be managed: no new keys, members or settings until it is active.

An organization also has a **team** (`lbx-…`) for AerolVM, derived from its ID and never reused. Only instance admins see the team and the raw overrides.

## Regions

Customers place sandboxes in **regions**: the clusters that are ready or degraded. The route carries no admin URL, token, node or version.

```bash
lewbox region list
```

Any signed-in account, and any organization API key, can list regions. Cluster, node, account, plan and settings routes are instance-admin only.

## How limits are enforced

A plan's limits are shared across every region. On each sync pass (every 15 seconds) Lewbox counts the sandboxes the organization holds on each cluster — stopped ones included, as AerolVM counts them — and works out the headroom:

```
headroom = max(0, limit − use in all regions)
quota in a region = use in that region + headroom
```

It then sets that region's AerolVM **team quota** for the organization's team. AerolVM enforces it atomically, so a create that would exceed the quota fails with `429 team_quota_exceeded`; lowering a limit never removes a running sandbox, it only refuses growth. With one region this is a plain limit; with several, the free headroom follows the region that needs it.

AerolVM reads a quota of `0` as "no limit", so a suspended or pending organization gets a **blocking quota** (`max_cpu: 0.001`, `max_memory_mb: 1`) instead. When every limit is unlimited, Lewbox deletes the quota.

Suspending an organization pushes the blocking quota to every region at once, and the next sync stops its running sandboxes so they stop accruing compute. Pending organizations have the same block.

Quotas are only pushed when they change, when the last push failed, or after ten minutes; what Lewbox last pushed and any error is stored per organization and region. A changed plan, limit or status is pushed immediately. `lewbox limits` shows the current state and every push error.

Limits apply on top of what is already running: a suspended or over-limit organization can read its data but not grow.

## What customers see

| | Instance admin | Org owner/admin | Org member/viewer | Org API key |
|---|---|---|---|---|
| Clusters, nodes, accounts, plans, settings | yes | no | no | no |
| Regions | yes | yes | yes | yes |
| Own organization's plan and effective limits | yes | yes | yes | yes |
| Change plan, limits or status | yes | no | no | no |

## Next

- [Organizations and API keys](organizations-and-api-keys.md)
- [HTTP API](../reference/http-api.md#plans)
