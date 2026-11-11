---
title: HTTP API
description: The orchestrator's /v0 API — authentication, every route, and the objects it returns.
---

Everything the CLI does goes through this API. The CLI's `--json` output is exactly these response bodies.

- Base URL: your orchestrator, for example `https://lewbox.example.com/v0`.
- JSON in and out. Unknown fields in a request body are rejected.
- Errors: `{"error":{"code","message","details"}}`, with the codes in [Output and errors](output-and-errors.md#errors).

## Authentication

Sign in to get a session token, then send it as a bearer token:

```bash
TOKEN=$(curl -fsS -X POST https://lewbox.example.com/v0/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"…"}' | jq -r .token)

curl -fsS -H "Authorization: Bearer $TOKEN" https://lewbox.example.com/v0/clusters
```

Tokens look like `lbs_` followed by 64 hex characters and last 30 days. An organization's API key (`lba_…`, see [below](#organizations)) works the same way, for that organization's routes only. Every route except `GET /v0/health`, `GET /v0/instance`, `POST /v0/auth/signup`, the public docs routes (`GET /v0/docs…`), `POST /v0/auth/login` and `POST /v0/auth/session` needs one. A missing token gets `401`. A regular account calling an admin-only route gets `403`.

Browsers use `POST /v0/auth/session` instead. It takes the same body and sets an `HttpOnly` session cookie, so the token never reaches JavaScript:

```
POST /v0/auth/session
{"email":"you@example.com","password":"…"}

200 OK
Set-Cookie: __Host-lewbox_session=lbs_…; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=604800
{"expires_at":"2026-11-18T15:23:01.2Z","user":{…}}
```

Over TLS the cookie is named `__Host-lewbox_session` and is `Secure`; on plain HTTP it is `lewbox_session`. A browser session lasts at most 7 days and ends after 12 hours without a request. Send the cookie back and omit `Authorization`; if both are present, `Authorization` wins.

### CSRF and request bodies

Browser cookie requests are protected against cross-site request forgery. A state-changing request that the browser marks as cross-site (`Sec-Fetch-Site: cross-site`, or a mismatched `Origin`) gets `403`:

```json
{"error":{"code":"forbidden","message":"cross-origin request rejected"}}
```

A cookie-authenticated `POST`, `PUT`, `PATCH` or `DELETE` with a body must also send `Content-Type: application/json` (parameters such as `charset` are allowed); anything else gets `415` with code `invalid_request`. Bearer requests are not affected: the CLI sends no `Sec-Fetch-Site` or `Origin` header and needs no special content type.

API responses carry `Cache-Control: no-store`.

### Rate limits

`POST /v0/auth/login`, `POST /v0/auth/session` and `POST /v0/auth/signup` share two budgets: 20 attempts per client IP per minute, and 5 failed attempts per normalized email per minute (sign-up only uses the per-IP budget). When one is exhausted the response is `429` with a `Retry-After` header and:

```json
{"error":{"code":"rate_limited","message":"too many sign-in attempts; try again in 42 seconds","details":{"retry_after_seconds":42}}}
```

## Routes

`{ref}` takes an ID or a name: the slug for clusters, the name for nodes, the email for accounts.

### Auth

| Route | Who | |
|---|---|---|
| `GET /v0/health` | anyone | `{"ok": true}` |
| `GET /v0/instance` | anyone | `{name, signup}`: the instance's name and sign-up mode, for the sign-in page |
| `POST /v0/auth/login` | anyone | `{email, password}` → `{token, expires_at, user}` |
| `POST /v0/auth/session` | anyone | Same body, sets the browser session cookie → `{expires_at, user}`. The token is only in the cookie |
| `POST /v0/auth/signup` | anyone | `{email, password, organization}` → the same body and cookie as `POST /v0/auth/session`. `403` when sign-up is closed, `409` when the email is taken |
| `POST /v0/auth/logout` | signed in | Ends this session and clears the session cookie → `{"ok": true}` |
| `GET /v0/me` | signed in | `{user, organizations, via}` for an account; `{organization, admin_key, via}` for an API key |

### Regions

| Route | Who | |
|---|---|---|
| `GET /v0/regions` | signed in, or a key | `{data: [Region]}`: clusters that are `ready` or `degraded`, ordered by slug. No admin fields |

### Settings

Instance admins only.

| Route | Who | |
|---|---|---|
| `GET /v0/settings` | admin | `{instance_name, signup, default_plan}` |
| `PATCH /v0/settings` | admin | `{instance_name, signup, default_plan}`; only the fields set change. `signup` is `open`, `approval` or `closed`; `default_plan` is a plan slug |

### Plans

Instance admins only. A `null` limit is unlimited.

| Route | Who | |
|---|---|---|
| `GET /v0/plans` | admin | `{data: [Plan]}`, ordered by slug |
| `POST /v0/plans` | admin | `{slug, name, max_sandboxes, max_vcpus, max_memory_mb}` → `201` Plan |
| `GET /v0/plans/{ref}` | admin | Plan, by ID or slug |
| `PATCH /v0/plans/{ref}` | admin | Any of `slug`, `name` and the three limits. For a limit, absent means no change, `null` means unlimited |
| `DELETE /v0/plans/{ref}` | admin | → the Plan. `409` while organizations use it or it is the default plan |

### Clusters

| Route | Who | |
|---|---|---|
| `GET /v0/clusters` | admin | `{data: [Cluster]}` |
| `POST /v0/clusters` | admin | Register: `{slug, name, admin_url, endpoint_url, runtime, admin_token}` → `201` ClusterDetail. A failed required check → `412` with the checks in `details` |
| `GET /v0/clusters/{ref}` | admin | ClusterDetail, from the last check |
| `DELETE /v0/clusters/{ref}` | admin | Stop managing it → the Cluster |
| `POST /v0/clusters/{ref}/check` | admin | Run the checks now → ClusterDetail |
| `POST /v0/clusters/{ref}/join-tokens` | admin | `{role, ttl}` → `201` JoinToken |
| `GET /v0/clusters/resolve/{ref}` | admin | The Cluster for a slug, ID, or any of its nodes' names. Accepts `cluster/x` and `node/x` |

### Nodes

| Route | Who | |
|---|---|---|
| `GET /v0/nodes?cluster={ref}` | admin | `{data: [Node]}`, optionally for one cluster |
| `GET /v0/nodes/{ref}` | admin | Node |
| `DELETE /v0/nodes/{ref}?force=true` | admin | Drain, remove from the cluster, forget → the Node |
| `POST /v0/nodes` | admin | `{name, url}`: record a node by URL only → `201` Node |

### Accounts

| Route | Who | |
|---|---|---|
| `GET /v0/users` | admin | `{data: [User]}` |
| `POST /v0/users` | admin | `{email, password, instance_admin}` → `201` User |
| `GET /v0/users/{ref}` | admin, or themselves | User |
| `DELETE /v0/users/{ref}` | admin | → the User |
| `POST /v0/users/{ref}/disable` | admin | Ends their sessions → User |
| `POST /v0/users/{ref}/enable` | admin | → User |
| `PUT /v0/users/{ref}/password` | admin, or themselves | `{password}` → User |

### Organizations

The member and API key routes act on the organization in the `Lewbox-Organization` header (its slug or ID). An API key acts for its own organization, so it can leave the header out; naming another organization gets `404`. Without either, the request gets `400`.

| Route | Who | |
|---|---|---|
| `GET /v0/organizations?user={ref}&status={status}` | signed in, or a key | `{data: [Organization]}`: every one for admins, your own otherwise, its own for a key. `user` (admins, or yourself) lists that account's, with its `role` in each; `status` keeps `pending`, `active` or `suspended` ones |
| `POST /v0/organizations` | admin | `{slug, name, owner, plan}` → `201` Organization. `owner` (an email or ID) defaults to you; `plan` (a slug) defaults to the instance's default plan |
| `GET /v0/organizations/{ref}` | its members, admins, its keys | Organization |
| `PATCH /v0/organizations/{ref}` | owner or admin; plan, limits and status: admin | `{name}` renames. Instance admins can also set `{plan, max_sandboxes, max_vcpus, max_memory_mb, status}`. A `null` limit falls back to the plan's; `status` is `active` or `suspended` → Organization |
| `GET /v0/organization` | as for the header's, or the key's own | Organization |
| `GET /v0/members` | its members, admins, its keys | `{data: [Member]}` |
| `POST /v0/members` | owner or admin | `{email, role}` → `201` Member. Only owners add owners |
| `PATCH /v0/members/{ref}` | owner or admin | `{role}` → Member. `{ref}` is an email, account ID or member ID. The last owner stays (`409`) |
| `DELETE /v0/members/{ref}` | owner or admin, or themselves | → the Member |
| `GET /v0/admin-keys` | owner, admin, admins, its keys | `{data: [AdminKey]}`, newest first, revoked ones included |
| `POST /v0/admin-keys` | owner or admin | `{name, expires_in}` → `201` AdminKey plus `token`, shown only here. `expires_in` is like `90d` or `720h`; empty means never |
| `DELETE /v0/admin-keys/{ref}` | owner or admin | Revoke by ID or live name → the AdminKey |
| `GET /v0/sandbox-keys` | its members, admins, its keys | `{data: [SandboxKey]}`, live ones first, revoked ones included, newest first |
| `POST /v0/sandbox-keys` | owner, admin or member, admins, its keys | `{name, region, expires_in}` → `201` NewSandboxKey, with the `avm_…` token shown only here. `region` defaults to the only ready region; `expires_in` is `90d` (the default), `never` or a duration such as `30d`. `412` if the organization is not active or the region is not ready; a cluster failure is `502` and stores nothing |
| `DELETE /v0/sandbox-keys/{ref}` | owner, admin or member (members only their own), admins, its keys | Revoke the upstream token, then mark it → SandboxKey. A cluster failure is `502` and the key stays live |

"Admin" in the **Who** column is an instance admin, who can do everything in every organization. An API key can never change members or admin keys, but it can list, create and revoke sandbox keys for its own organization. It gets `403` on every route outside that organization (clusters, nodes, accounts, plans, settings). A suspended or pending organization's keys get `403`; its members can only read, and it cannot mint sandbox keys (`412`).

### Docs

Public and static: no token and no cookie. The web console renders them at `/docs/...`.

| Route | Who | |
|---|---|---|
| `GET /v0/docs` | anyone | The nav from `docs/README.md`: `{groups: [{title, items: [{title, path, description, external}]}]}` |
| `GET /v0/docs/{path}` | anyone | One page: `{path, title, description, markdown, prev, next}`. `GET /v0/docs/` is the home page. An unknown page gets `404` with code `not_found` |

The API lives only under `/v0`. `GET /` (and any other page route) serves the web console, a single-page app that loads these docs itself.

## Objects

**Cluster**

```json
{
  "id": "01a1cb34-29a5-74c3-9691-5394d021b0f2",
  "slug": "eu-west",
  "name": "eu-west",
  "admin_url": "http://10.0.0.5:21212",
  "endpoint_url": "http://10.0.0.5:21212",
  "runtime": "docker",
  "admin_token_last4": "9f3a",
  "status": "ready",
  "version": "v0.7.0",
  "nodes": 2,
  "checked_at": "2026-11-11T15:23:01.2Z",
  "created_at": "2026-11-11T15:22:35.8Z"
}
```

`status` is `ready`, `degraded` or `unreachable`; `status_detail` explains anything but `ready`. The admin token itself is never returned.

**ClusterDetail** is a Cluster plus `checks` (a list of `{name, ok, fatal, detail}`) and `node_list` (a list of Nodes).

**Node**

```json
{
  "id": "01a1cb34-659b-74a8-983d-085c0383e0a6",
  "name": "node-2",
  "url": "http://10.0.0.6:21212",
  "cluster_id": "01a1cb34-29a5-74c3-9691-5394d021b0f2",
  "cluster": "eu-west",
  "role": "mixed",
  "version": "v0.7.0",
  "alive": true,
  "last_seen_at": "2026-11-11T15:22:51.1Z",
  "created_at": "2026-11-11T15:22:51.1Z"
}
```

**User**: `{id, email, instance_admin, disabled, created_at}`. Passwords and hashes are never returned.

**Organization**

```json
{
  "id": "01a1cb34-29a5-74c3-9691-5394d021b0f2",
  "slug": "acme",
  "name": "Acme Inc",
  "status": "active",
  "status_reason": "",
  "team": "lbx-01a1cb3429a574c396915394d021b0f2",
  "plan": {"slug": "free", "name": "Free"},
  "limits": {"sandboxes": 2, "vcpus": 2, "memory_mb": 4096},
  "overrides": {"sandboxes": null, "vcpus": null, "memory_mb": null},
  "members": 1,
  "role": "owner",
  "created_at": "2026-11-11T15:22:35.8Z"
}
```

`status` is `pending`, `active` or `suspended`; `status_reason` is `""`, `admin` or `credits`. `limits` are the effective limits: an override if set, otherwise the plan's; `null` is unlimited. `team` and `overrides` are only present for instance admins. `role` is your role (or the listed account's) when it is a member.

**Region**: `{slug, name, endpoint_url, runtime, status}` for a cluster customers can use.

**Plan**: `{id, slug, name, max_sandboxes, max_vcpus, max_memory_mb, organizations, default, created_at}`. A `null` limit is unlimited; `organizations` is how many organizations use it.

**Settings**: `{instance_name, signup, default_plan}`. `signup` is `open`, `approval` or `closed`.

**Instance**: `{name, signup}`; public.

**Me** for an account is `{user, organizations, via}` where each organization is `{id, slug, name, role, status}`. For an API key it is `{organization, admin_key, via}` instead.

**Member**: `{id, organization_id, user_id, email, role, created_at}`.

**AdminKey**: `{id, organization_id, name, prefix, last4, status, created_by, created_at, last_used_at, expires_at, revoked_at}`. `status` is `active`, `expired` or `revoked`; `created_by` is an email. The token is only in the create response.

**SandboxKey**: `{id, name, region, token_hint, created_by, created_at, expires_at, revoked_at, expired}`. `region` is `{slug, name, endpoint_url, runtime}`; `token_hint` is `avm_…` and the token's last four characters; `expires_at` and `revoked_at` are `null` when they do not apply. **NewSandboxKey** adds the `token` (shown only in the create response), `env` (`SB_API_URL`, `SB_API_TOKEN`) and, for a `docker` region, `e2b_env` (`E2B_API_URL`, `E2B_SANDBOX_URL`, `E2B_API_KEY`).

**JoinToken**: `{cluster_id, cluster, runtime, version, role, token_role, token, ca_cert_hash, join_urls, expires_at, command}`. `token` is single-use and is shown only in this response.
