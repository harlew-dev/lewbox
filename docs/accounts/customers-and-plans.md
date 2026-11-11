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

A plan is a set of limits. A limit that is not set is unlimited.

```bash
lewbox plan list
lewbox plan create pro --name "Pro" --max-sandboxes 20 --max-vcpus 16 --max-memory 32GB
lewbox plan show pro
lewbox plan update pro --max-sandboxes 50 --max-memory unlimited
lewbox plan delete old
```

`--max-memory` takes `4096`, `4096MB` or `4GB`; `0` and `unlimited` clear a limit. The `free` plan is seeded. A plan can't be deleted while organizations use it or while it is the instance's default plan.

## Organizations as customers

Each organization has one plan and may have its own overrides. The **limits** a customer sees are the effective ones: an override if it is set, otherwise the plan's value; `null` means unlimited.

```bash
lewbox org create acme --name "Acme Inc" --owner owner@acme.dev --plan free
lewbox org update acme --plan pro --max-sandboxes 10
lewbox org update acme --max-vcpus unlimited   # back to the plan's value
lewbox org list --status pending
lewbox org approve acme                        # status: active
lewbox org suspend acme                        # status: suspended, reason "admin"
lewbox org restore acme
```

Only instance admins change a plan, limits or status. Owners and admins still rename their organization and manage members and keys. A `pending` or `suspended` organization is readable by its members but cannot be managed: no new keys, members or settings until it is active.

An organization also has a **team** (`lbx-…`) for AerolVM, derived from its ID and never reused. Only instance admins see the team and the raw overrides.

## Regions

Customers place sandboxes in **regions**: the clusters that are ready or degraded. The route carries no admin URL, token, node or version.

```bash
lewbox region list
```

Any signed-in account, and any organization API key, can list regions. Cluster, node, account, plan and settings routes are instance-admin only.

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
