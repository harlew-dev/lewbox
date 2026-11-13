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

Tokens look like `lbs_` followed by 64 hex characters and last 30 days. An organization's API key (`lba_…`, see [below](#organizations)) works the same way, for that organization's routes only. Every route except `GET /v0/health`, `GET /v0/instance`, `POST /v0/auth/signup`, the public docs routes (`GET /v0/docs…` and `GET /v0/platform…`), `POST /v0/auth/login` and `POST /v0/auth/session` needs one. A missing token gets `401`. A regular account calling an admin-only route gets `403`.

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
| `POST /v0/auth/signup` | anyone | `{email, password, setup_token}` → the same body and cookie as `POST /v0/auth/session`. It creates only the account, with no workspace. A valid, unused `setup_token` allows sign-up in any mode and claims the link; without one, `403` when sign-up is closed. An invalid or used token is `400`; a taken email is `409` |
| `POST /v0/auth/logout` | signed in | Ends this session and clears the session cookie → `{"ok": true}` |
| `GET /v0/me` | signed in | `{user, organizations, via}` for an account; `{organization, admin_key, via}` for an API key |
| `GET /v0/setup/{token}` | anyone | The public setup page for an unused, unexpired link: `{instance_name, plan: {name}, region: {name}, expires_at}`. Never a slug or who claimed it. `404` (`this setup link is invalid or has expired`) for an unknown, used or expired link |

### Regions

| Route | Who | |
|---|---|---|
| `GET /v0/regions` | signed in, or a key | `{data: [Region]}`: clusters that are `ready` or `degraded`, ordered by slug. Admins get every one; anyone else gets the workspace's region, with `plan` and `limits`. No admin fields |

### Settings

Instance admins only.

| Route | Who | |
|---|---|---|
| `GET /v0/settings` | admin | `{instance_name, signup, default_plan, default_region}` |
| `PATCH /v0/settings` | admin | `{instance_name, signup, default_plan, default_region}`; only the fields set change. `signup` is `open`, `approval` or `closed`; `default_plan` and `default_region` are a plan and cluster slug, or `none` to clear them |

### Setup links

Instance admins create and revoke them; only the token's hash is stored, so `token` appears only in the create response. See `GET /v0/setup/{token}` above for the public page.

| Route | Who | |
|---|---|---|
| `GET /v0/setup-links` | admin | `{data: [SetupLink]}`, newest first, never a token |
| `POST /v0/setup-links` | admin | `{plan, region, expires_in_hours, note}` → `201` SetupLink plus `token`. `plan` and `region` are a slug or ID; `expires_in_hours` defaults to 168 and is 1–720. Unknown plan or region is `404` |
| `DELETE /v0/setup-links/{id}` | admin | Revoke an unused link → the SetupLink. `409` when it has been used |

### Plans

Instance admins only. A limit is a positive integer; `null` is unlimited and `0` is refused.

| Route | Who | |
|---|---|---|
| `GET /v0/plans` | admin | `{data: [Plan]}`, ordered by slug |
| `POST /v0/plans` | admin | `{slug, name, max_sandboxes, max_vcpus, max_memory_mb}` → `201` Plan |
| `GET /v0/plans/{ref}` | admin | Plan, by ID or slug |
| `PATCH /v0/plans/{ref}` | admin | Any of `slug`, `name` and the three limits. For a limit, absent means no change, `null` means unlimited |
| `DELETE /v0/plans/{ref}` | admin | → the Plan. `409` while a subscription uses it or it is the default plan |

### DNS profiles

Instance admins only. A DNS profile is a saved Cloudflare token, kept sealed and shown only by its last four characters. Creating one, or changing its zone or token, tests it first and saves nothing when the test fails. See [DNS profiles](../clusters/dns-profiles.md).

| Route | Who | |
|---|---|---|
| `GET /v0/dns-profiles` | admin | `{data: [DNSProfile]}`, ordered by name |
| `POST /v0/dns-profiles` | admin | `{name, provider, zone, api_token, acme_email}` → `201` DNSProfile. A failed test → `412`, with `details.step` set to `zone` or `write` |
| `GET /v0/dns-profiles/{ref}` | admin | DNSProfile, by ID or name |
| `PATCH /v0/dns-profiles/{ref}` | admin | Any of `name`, `zone`, `api_token` and `acme_email`. A new zone or token is tested before it is saved and bumps `version` |
| `DELETE /v0/dns-profiles/{ref}` | admin | → the DNSProfile. `409` while a cluster uses it |
| `POST /v0/dns-profiles/{ref}/test` | admin | Test now → DNSProfile. A failure is recorded in `test_error`, not returned as an error |
| `POST /v0/dns-profiles/{ref}/credentials` | admin | `{name, provider, zone, zone_id, api_token, acme_email, version}`. The only response with the token |

### Storage profiles

Instance admins only. A storage profile is a saved S3-compatible bucket and access key, kept sealed and shown only by its last four characters. Creating one, or changing anything but its name, tests it first and saves nothing when the test fails. See [Storage profiles](../clusters/storage-profiles.md).

| Route | Who | |
|---|---|---|
| `GET /v0/storage-profiles` | admin | `{data: [StorageProfile]}`, ordered by name |
| `POST /v0/storage-profiles` | admin | `{name, provider, endpoint, region, bucket, prefix, path_style, access_key_id, secret_access_key}` → `201` StorageProfile. A failed test → `412`, with `details.step` set to `write`, `read`, `list` or `delete` |
| `GET /v0/storage-profiles/{ref}` | admin | StorageProfile, by ID or name |
| `PATCH /v0/storage-profiles/{ref}` | admin | Any of `name`, `endpoint`, `region`, `bucket`, `prefix`, `path_style`, `access_key_id` and `secret_access_key`. A change other than the name is tested before it is saved and bumps `version` |
| `DELETE /v0/storage-profiles/{ref}` | admin | → the StorageProfile. `409` while a cluster uses it |
| `POST /v0/storage-profiles/{ref}/test` | admin | Test now → StorageProfile. A failure is recorded in `test_error`, not returned as an error |
| `POST /v0/storage-profiles/{ref}/credentials` | admin | `{name, provider, endpoint, region, bucket, prefix, path_style, access_key_id, secret_access_key, version}`. The only response with the secret |

### Clusters

| Route | Who | |
|---|---|---|
| `GET /v0/clusters` | admin | `{data: [Cluster]}` |
| `POST /v0/clusters` | admin | Register: `{slug, name, admin_url, endpoint_url, runtime, domain, dns_profile, dns_records, storage_profile, stop_policy, admin_token}` → `201` ClusterDetail. `domain`, `dns_profile` and `storage_profile` (IDs or names) and `dns_records` are optional; `stop_policy` is `keep` or `archive`. A failed required check → `412` with the checks in `details`; `archive` without a storage profile or on Firecracker, or a bad policy, → `400` |
| `PATCH /v0/clusters/{ref}` | admin | `{domain, dns_profile, dns_records, storage_profile, stop_policy}`: change the settings; a profile `""` or `"none"` clears it, `dns_records` is `auto` or `off`, `stop_policy` is `keep` or `archive`. An empty body, a domain outside the profile's zone, a bad record mode or policy, `archive` without a profile, or clearing the profile while `archive` is set → `400`. → ClusterDetail |
| `GET /v0/clusters/{ref}` | admin | ClusterDetail, from the last check |
| `DELETE /v0/clusters/{ref}?force=true` | admin | Stop managing it → the Cluster. Refuses with `502` if its sandbox keys cannot be revoked, unless `force` is true |
| `POST /v0/clusters/{ref}/check` | admin | Run the checks now → ClusterDetail |
| `GET /v0/clusters/{ref}/node-settings` | admin | What every node must apply, with secrets: `{cluster_id, cluster, stop_policy, storage, dns}`. `storage` is `null` or `{profile, version, endpoint, region, bucket, path_style, root_directory, access_key_id, secret_access_key}`; `dns` is `null` or `{profile, version, zone, domain, api_token, acme_email}` |
| `GET /v0/clusters/{ref}/health?days=30` | admin | `{cluster: {uptime, days}, nodes: [{name, uptime, days}]}`. A day has `date`, `uptime` (`null` with no samples), `samples` and `down_minutes`; `days` is 1–31 |
| `GET /v0/clusters/{ref}/metrics?range=24h` | admin | `{range, bucket_seconds, nodes: [{name, data}]}`. A bucket has `start`, averaged `sandboxes`, `cpu_cores`, `cpu_reserved`, `memory_total_mb`, `memory_used_mb`, `disk_free_gb` and the `alive` fraction. `range` is `24h`, `7d` or `30d` |
| `GET /v0/overview` | admin | `{customers, sandboxes, capacity, clusters, running, top_customers}` for the console home |
| `POST /v0/clusters/{ref}/join-tokens` | admin | `{role, ttl}` → `201` JoinToken |
| `GET /v0/clusters/resolve?ref=REF` | admin | The Cluster for a slug, ID, or any of its nodes' names. Accepts `cluster/x` and `node/x` |

### Nodes

| Route | Who | |
|---|---|---|
| `GET /v0/nodes?cluster={ref}` | admin | `{data: [Node]}`, optionally for one cluster |
| `GET /v0/nodes/{ref}` | admin | Node |
| `DELETE /v0/nodes/{ref}?force=true` | admin | Drain, remove from the cluster, forget → the Node |
| `POST /v0/nodes` | admin | `{name, url}`: record a node by URL only → `201` Node |
| `PUT /v0/nodes/{ref}/settings` | admin | `{storage_profile, storage_version, dns_profile, dns_version}`: the versions a node applied, a name or `null` for each profile. A version below 1 with a profile → `400`; a node in no cluster → `412`. → the Node |

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
| `POST /v0/organizations` | signed in | `{slug, name, owner, setup_token}` → `201` Organization. `slug` is generated from `name` when it is empty. An instance admin may set `owner` (an email or ID, default you) and gets no default subscription unless it uses a setup token. Anyone else becomes the owner (`owner` is `403`), gets the default plan in the default region when both exist, and must have sign-up `open` (active workspace) or `approval` (pending); `closed` is `403`. With `setup_token`, the workspace is active in any mode, subscribed to the link's plan in its region, and the link is consumed. At most 10 workspaces may be owned by one account (`412`) |
| `GET /v0/organizations/{ref}` | its members, admins, its keys | Organization |
| `PATCH /v0/organizations/{ref}` | owner or admin; status: admin | `{name}` renames. Instance admins can also set `{status}`; `status` is `active` or `suspended` → Organization |
| `GET /v0/organization` | as for the header's, or the key's own | Organization |
| `GET /v0/members` | its members, admins, its keys | `{data: [Member]}` |
| `POST /v0/members` | owner or admin | `{email, role}` → `201` Member. Only owners add owners |
| `PATCH /v0/members/{ref}` | owner or admin | `{role}` → Member. `{ref}` is an email, account ID or member ID. The last owner stays (`409`) |
| `DELETE /v0/members/{ref}` | owner or admin, or themselves | → the Member |
| `GET /v0/admin-keys` | owner, admin, admins, its keys | `{data: [AdminKey]}`, newest first, revoked ones included |
| `POST /v0/admin-keys` | owner or admin | `{name, expires_in}` → `201` AdminKey plus `token`, shown only here. `expires_in` is like `90d` or `720h`; empty means never |
| `DELETE /v0/admin-keys/{ref}` | owner or admin | Revoke by ID or live name → the AdminKey |
| `GET /v0/sandbox-keys` | its members, admins, its keys | `{data: [SandboxKey]}`, live ones first, revoked ones included, newest first |
| `POST /v0/sandbox-keys` | owner, admin or member, admins, its keys | `{name, region, expires_in}` → `201` NewSandboxKey, with the `avm_…` token shown only here. `region` defaults to the workspace's region; `expires_in` is `90d` (the default), `never` or a duration such as `30d`. `412` if the organization is not active, the region is not ready, or the new quota has not been pushed successfully yet ("still being set up"); an unsubscribed region is `404`. A cluster failure is `502` and stores nothing |
| `DELETE /v0/sandbox-keys/{ref}` | owner, admin or member (members only their own), admins, its keys | Revoke the upstream token, then mark it → SandboxKey. A cluster failure is `502` and the key stays live |

"Admin" in the **Who** column is an instance admin, who can do everything in every organization. An API key can never change members or admin keys, but it can list, create and revoke sandbox keys for its own organization. It gets `403` on every route outside that organization (clusters, nodes, accounts, plans, settings). A suspended or pending organization's keys get `403`; its members can only read, and it cannot mint sandbox keys (`412`).

### Sandboxes and usage

These routes take the organization as `?org={slug}` when present, otherwise in the `Lewbox-Organization` header. An instance admin with neither sees the whole fleet. Another organization's sandbox is always `404`, never `403`.

| Route | Who | |
|---|---|---|
| `GET /v0/sandboxes?region={slug}&status={status}&org={slug}` | its members, admins, its keys | `{data: [Sandbox]}`, not-gone only, newest first, at most 500. Filters by region and status |
| `POST /v0/sandboxes/{id}/stop?region={slug}` | owner, admin or member, admins, its keys | Stop on the cluster, then update the mirror → Sandbox. `region` picks the cluster when the ID is not unique |
| `DELETE /v0/sandboxes/{id}?region={slug}` | same | Delete on the cluster, then mark it gone → Sandbox |
| `GET /v0/snapshots?region={slug}&org={slug}` | its members, admins, its keys | `{data: [Snapshot], partial_nodes: [node]}`. Lists the workspace's snapshots on every node of each subscribed region; `partial_nodes` names nodes that did not answer. Without `org`, an instance admin gets every workspace's. Cached for 30 s |
| `DELETE /v0/snapshots/{name}?region={slug}` | owner, admin or member, admins, its keys | Delete on the cluster with a short-lived team token, then revoke it → Snapshot |
| `GET /v0/usage?from={date}&to={date}&granularity=hour\|day&org={slug}` | its members, admins, its keys | `Usage`: one bucket per hour or day, zero-filled. `to` defaults to now, `from` to 30 days before; at most 400 days |
| `GET /v0/limits` | its members, admins, its keys | `LimitsReport`: one entry per subscribed region, with its plan, limits, live use, quota and last push |

Suspending an organization pushes a blocking quota where it has used compute and stops its running sandboxes on the next sync pass; the sync also stops sandboxes in a region it is no longer subscribed to. Stopped sandboxes still count against limits, as AerolVM counts them.

Snapshots are listed with short-lived read-only tokens bound to the workspace's AerolVM team, kept in memory only. Names are unique per team, so `region` names the workspace's region; without it Lewbox asks that region. A viewer may list snapshots but not delete one.

### Subscriptions

The organization comes from the `Lewbox-Organization` header. Only instance admins change them; members may list them.

| Route | Who | |
|---|---|---|
| `GET /v0/subscriptions` | its members, admins, its keys | `{data: [Subscription]}`, by region slug |
| `PUT /v0/subscriptions/{region}` | admin | `{plan, max_sandboxes, max_vcpus, max_memory_mb}` → Subscription. `{region}` is a cluster slug or ID; `plan` is a slug or ID. A workspace is in at most one region, so a different region is `409` (remove the current one first); the same region changes the plan. An absent override keeps the current one (`null` on a create), `null` clears it to the plan's; `0` is refused. Pushes the quota at once (best effort) |
| `DELETE /v0/subscriptions/{region}?force=true` | admin | Unsubscribe → the Subscription. Revokes the organization's live keys there (refusing with `502` unless `force`), pushes the blocking quota and stops its running sandboxes |

### Docs

Public and static: no token and no cookie. There are two books: the customer book at `/v0/docs`, rendered in the console at `/docs/...`, and the platform book at `/v0/platform`, rendered at `/platform/...`.

| Route | Who | |
|---|---|---|
| `GET /v0/docs` | anyone | The customer book's nav from `docs/consumer/README.md`: `{groups: [{title, items: [{title, path, description, external}]}]}` |
| `GET /v0/docs/{path}` | anyone | One page: `{path, title, description, markdown, prev, next}`. `GET /v0/docs/` is the home page. An unknown page gets `404` with code `not_found` |
| `GET /v0/platform` | anyone | The platform book's nav from `docs/platform/README.md`, same shape |
| `GET /v0/platform/{path}` | anyone | One platform page, same shape. An unknown page gets `404` |

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
  "domain": "eu.api.example.com",
  "dns_profile": "cf-prod",
  "dns_records": "auto",
  "storage_profile": "s3-main",
  "stop_policy": "archive",
  "admin_token_last4": "9f3a",
  "status": "ready",
  "version": "v0.7.0",
  "nodes": 2,
  "checked_at": "2026-11-11T15:23:01.2Z",
  "created_at": "2026-11-11T15:22:35.8Z"
}
```

`status` is `ready`, `degraded` or `unreachable`; `status_detail` explains anything but `ready`. The admin token itself is never returned. `domain` is `""` when the cluster has none, `dns_profile` and `storage_profile` are the profiles' **names** or `null`, `dns_records` is `auto` or `off`, and `stop_policy` is `keep` or `archive`.

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
  "created_at": "2026-11-11T15:22:51.1Z",
  "settings": {
    "applied_at": "2026-11-13T09:15:04.2Z",
    "outdated": true,
    "reasons": ["storage profile s3-main is at version 3; this node has version 2"]
  }
}
```

`settings` is `null` for a node in no cluster. Otherwise `applied_at` is the last report (`null` when it never reported), `outdated` says its applied profile versions are behind its cluster's, and `reasons` (never `null`) lists why.

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
  "region": {"slug": "eu-west", "name": "EU West", "plan": {"slug": "free", "name": "Free"}},
  "members": 1,
  "role": "owner",
  "created_at": "2026-11-11T15:22:35.8Z"
}
```

`status` is `pending`, `active` or `suspended`; `status_reason` is `""` or `admin`. `region` is the workspace's single subscribed region, or `null` when it has none. `team` is only present for instance admins. `role` is your role (or the listed account's) when it is a member.

**Region**: `{slug, name, endpoint_url, runtime, status}` for a cluster customers can use. For a subscribed region it also has `plan` (`{slug, name}`) and `limits`.

**Subscription**: `{region, plan, limits, overrides, created_at}`. `region` is a Region; `limits` are the effective limits (the plan's, with the override); `overrides` (`{sandboxes, vcpus, memory_mb}`) is only present for instance admins. A `null` limit is unlimited or, on an override, the plan's value.

**Plan**: `{id, slug, name, max_sandboxes, max_vcpus, max_memory_mb, subscriptions, default, created_at}`. A `null` limit is unlimited; `subscriptions` is how many subscriptions use it.

**DNSProfile**: `{id, name, provider, zone, zone_id, token_last4, acme_email, version, clusters, tested_at, test_error, created_at, updated_at}`. `provider` is `cloudflare`; `version` goes up when the zone or token changes, so nodes can tell they are out of date. The token itself is never in it.

**StorageProfile**: `{id, name, provider, endpoint, region, bucket, prefix, path_style, access_key_id, secret_last4, version, clusters, tested_at, test_error, created_at, updated_at}`. `provider` is `s3` and an empty `endpoint` means AWS S3; `version` goes up when anything but the name changes, so nodes can tell they are out of date. The secret access key itself is never in it.

**Settings**: `{instance_name, signup, default_plan, default_region}`. `signup` is `open`, `approval` or `closed`; `default_plan` and `default_region` are a plan and cluster slug, or `null`.

**SetupLink**: `{id, plan, region, note, status, claimed_by, workspace, created_at, expires_at, used_at}` plus `token` only in the create response. `plan` is `{slug, name}`, `region` is `{slug, name}`, `status` is `unused`, `claimed`, `used` or `expired`; `claimed_by` is an email or `null` and `workspace` is `{slug, name}` or `null`.

**LimitsReport**: `{regions: [{slug, name, plan, limits, used, quota, blocked, pushed_at, error}]}`, one entry per subscribed region. `used` is `{sandboxes, vcpus, memory_mb}`; `quota` is `null` when none was pushed; `error` is the last push error.

**Instance**: `{name, signup}`; public.

**Me** for an account is `{user, organizations, via}` where each organization is `{id, slug, name, role, status}`. For an API key it is `{organization, admin_key, via}` instead.

**Member**: `{id, organization_id, user_id, email, role, created_at}`.

**AdminKey**: `{id, organization_id, name, prefix, last4, status, created_by, created_at, last_used_at, expires_at, revoked_at}`. `status` is `active`, `expired` or `revoked`; `created_by` is an email. The token is only in the create response.

**SandboxKey**: `{id, name, region, token_hint, created_by, created_at, expires_at, revoked_at, expired}`. `region` is `{slug, name, endpoint_url, runtime}`; `token_hint` is `avm_…` and the token's last four characters; `expires_at` and `revoked_at` are `null` when they do not apply. **NewSandboxKey** adds the `token` (shown only in the create response), `env` (`SB_API_URL`, `SB_API_TOKEN`) and, for a `docker` region, `e2b_env` (`E2B_API_URL`, `E2B_SANDBOX_URL`, `E2B_API_KEY`).

**JoinToken**: `{cluster_id, cluster, runtime, version, role, token_role, token, ca_cert_hash, join_urls, expires_at, command}`. `token` is single-use and is shown only in this response.

**Snapshot**: `{name, region, organization?, node?, source_sandbox_id, image, created_at, cpu, memory_mb, disk_gb, push_state}`. `organization` (`{slug, name}`) and `node` (the member that holds it) are only included for instance admins. `push_state` describes a push to an image registry. AerolVM reports no owner and no size, so a snapshot is attributed to its workspace's team and is not billed.
