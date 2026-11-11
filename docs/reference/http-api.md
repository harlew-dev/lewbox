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

Tokens look like `lbs_` followed by 64 hex characters and last 30 days. An organization's API key (`lba_…`, see [below](#organizations)) works the same way, for that organization's routes only. Every route except `GET /v0/health`, the public docs routes (`GET /v0/docs…`), `POST /v0/auth/login` and `POST /v0/auth/session` needs one. A missing token gets `401`. A regular account calling an admin-only route gets `403`.

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

`POST /v0/auth/login` and `POST /v0/auth/session` share two budgets: 20 attempts per client IP per minute, and 5 failed attempts per normalized email per minute. When one is exhausted the response is `429` with a `Retry-After` header and:

```json
{"error":{"code":"rate_limited","message":"too many sign-in attempts; try again in 42 seconds","details":{"retry_after_seconds":42}}}
```

## Routes

`{ref}` takes an ID or a name: the slug for clusters, the name for nodes, the email for accounts.

### Auth

| Route | Who | |
|---|---|---|
| `GET /v0/health` | anyone | `{"ok": true}` |
| `POST /v0/auth/login` | anyone | `{email, password}` → `{token, expires_at, user}` |
| `POST /v0/auth/session` | anyone | Same body, sets the browser session cookie → `{expires_at, user}`. The token is only in the cookie |
| `POST /v0/auth/logout` | signed in | Ends this session and clears the session cookie → `{"ok": true}` |
| `GET /v0/me` | signed in | `{user, via}` |

### Clusters

| Route | Who | |
|---|---|---|
| `GET /v0/clusters` | signed in | `{data: [Cluster]}` |
| `POST /v0/clusters` | admin | Register: `{slug, name, admin_url, endpoint_url, runtime, admin_token}` → `201` ClusterDetail. A failed required check → `412` with the checks in `details` |
| `GET /v0/clusters/{ref}` | signed in | ClusterDetail, from the last check |
| `DELETE /v0/clusters/{ref}` | admin | Stop managing it → the Cluster |
| `POST /v0/clusters/{ref}/check` | admin | Run the checks now → ClusterDetail |
| `POST /v0/clusters/{ref}/join-tokens` | admin | `{role, ttl}` → `201` JoinToken |
| `GET /v0/clusters/resolve/{ref}` | signed in | The Cluster for a slug, ID, or any of its nodes' names. Accepts `cluster/x` and `node/x` |

### Nodes

| Route | Who | |
|---|---|---|
| `GET /v0/nodes?cluster={ref}` | signed in | `{data: [Node]}`, optionally for one cluster |
| `GET /v0/nodes/{ref}` | signed in | Node |
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
| `GET /v0/organizations?user={ref}` | signed in, or a key | `{data: [Organization]}`: every one for admins, your own otherwise, its own for a key. `user` (admins, or yourself) lists that account's, with its `role` in each |
| `POST /v0/organizations` | admin | `{slug, name, owner}` → `201` Organization. `owner` (an email or ID) defaults to you |
| `GET /v0/organizations/{ref}` | its members, admins, its keys | Organization |
| `PATCH /v0/organizations/{ref}` | owner or admin; `status`: admin | `{name}` renames; `{status: "suspended"\|"active"}` suspends or restores → Organization |
| `GET /v0/organization` | as for the header's, or the key's own | Organization |
| `GET /v0/members` | its members, admins, its keys | `{data: [Member]}` |
| `POST /v0/members` | owner or admin | `{email, role}` → `201` Member. Only owners add owners |
| `PATCH /v0/members/{ref}` | owner or admin | `{role}` → Member. `{ref}` is an email, account ID or member ID. The last owner stays (`409`) |
| `DELETE /v0/members/{ref}` | owner or admin, or themselves | → the Member |
| `GET /v0/admin-keys` | owner, admin, admins, its keys | `{data: [AdminKey]}`, newest first, revoked ones included |
| `POST /v0/admin-keys` | owner or admin | `{name, expires_in}` → `201` AdminKey plus `token`, shown only here. `expires_in` is like `90d` or `720h`; empty means never |
| `DELETE /v0/admin-keys/{ref}` | owner or admin | Revoke by ID or live name → the AdminKey |

"Admin" in the **Who** column is an instance admin, who can do everything in every organization. An API key can never change members or keys, and gets `403` on every route outside its organization (clusters, nodes, accounts). A suspended organization's keys get `403`, and its members can only read.

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

**Organization**: `{id, slug, name, status, members, role, created_at}`. `status` is `active` or `suspended`; `role` is your role (or the listed account's) when it's a member.

**Member**: `{id, organization_id, user_id, email, role, created_at}`.

**AdminKey**: `{id, organization_id, name, prefix, last4, status, created_by, created_at, last_used_at, expires_at, revoked_at}`. `status` is `active`, `expired` or `revoked`; `created_by` is an email. The token is only in the create response.

**JoinToken**: `{cluster_id, cluster, runtime, version, role, token_role, token, ca_cert_hash, join_urls, expires_at, command}`. `token` is single-use and is shown only in this response.
