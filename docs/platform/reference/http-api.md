---
title: HTTP API
description: The orchestrator's /v0 API — authentication, every route, and the objects it returns.
---

Everything the CLI does goes through this API. The CLI's `--json` output is exactly these response bodies.

- Base URL: your orchestrator, for example `https://lewbox.example.com/v0`.
- JSON in and out. Unknown fields in a request body are rejected.
- Errors: `{"error":{"code","message","details"}}`, with the codes in [Output and errors](output-and-errors.md#errors). A bad field value is `422` with `details.field`; `400` means the request couldn't be read at all; a dependency that is down is `503` with `Retry-After`.
- Every response carries `X-Request-ID`; a `500` repeats it in `details.request_id`. An unknown path is JSON `404`, and a known path with the wrong method is `405` with `Allow`.

## Lists

Every list that grows is paged, searched, sorted and filtered by the API. The answer carries the page beside the data:

```json
{"data":[…],"pages":{"current":2,"total":7,"per_page":20,"items":131}}
```

`pages.total` is at least 1, so an empty list is "page 1 of 1". A page past the end is `200` with `data: []` and the real totals.

| Parameter | Rule | Default |
|---|---|---|
| `page` | Plain digits, 1–10 000 | 1 |
| `per_page` | Plain digits, 1–100 | 20 |
| `q` | Trimmed, at most 100 characters, matched case-insensitively against the list's search columns. `%`, `_` and `\` match literally | none |
| `sort` | One of the list's sort keys; a `-` prefix reverses it | the list's first key |
| filters | One comma-separated value per parameter, such as `status=started,stopped`. Duplicates are ignored; at most 50 values | none (all) |

An empty item, an unknown value, an unknown sort key, a malformed page or a repeated parameter is `400` with `details.field` naming the parameter. A route that doesn't list `q` or a filter below rejects it.

| List | Sort keys (default first) | `q` matches | Filters |
|---|---|---|---|
| `GET /v0/organizations` | `name`, `-created`, `status` | name, slug | `status` (pending, active, suspended), `region`, `plan`, `user` (single: an ID or email) |
| `GET /v0/users` | `email`, `-created`, `status` | email | `status` (active, disabled), `role` (admin, user) |
| `GET /v0/sandboxes` | `-created`, `status`, `region`, `workspace` | sandbox ID | `status` (creating, started, stopped, destroyed, error), `region`, `organization` (admins) |
| `GET /v0/snapshots` | `-created`, `name`, `size` | name | `kind` (snapshot, archive), `region`, `organization` (admins), `status` (pending, pushing, active, error) |
| `GET /v0/nodes` | `cluster`, `name` | name, URL | `cluster`, `role` (server, worker, ingress, mixed), `alive` (true or false) |
| `GET /v0/members` | `email`, `role`, `-joined` | email | `role` (owner, admin, member, viewer) |
| `GET /v0/sandbox-keys`, `GET /v0/admin-keys`, `GET /v0/access-tokens` | `-created`, `name`, `expires` | name (and prefix for admin keys and access tokens) | `status` (active, expired, revoked) |
| `GET /v0/setup-links` | `-created`, `expires` | note | `status` (unused, claimed, used, expired), `plan`, `region` |
| `GET /v0/events` | fixed: newest first | none | `actor`, `organization` and `action` are comma lists; `from`, `to`, `limit` and the `before` cursor are unchanged |

`region`, `plan`, `organization`, `cluster` and `user` accept an ID or slug where the object has one. A customer's `region` is checked against their workspace's subscriptions; `organization` is for instance admins only (`403` otherwise).

## Authentication

Sign in to get a session token, then send it as a bearer token:

```bash
TOKEN=$(curl -fsS -X POST https://lewbox.example.com/v0/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"…"}' | jq -r .token)

curl -fsS -H "Authorization: Bearer $TOKEN" https://lewbox.example.com/v0/clusters
```

Tokens look like `lbs_` followed by 64 hex characters and last 30 days. An [access token](../accounts/access-tokens.md) (`lbp_…`, see [below](#access-tokens)) works the same way and acts as its account; use one for scripts and CI. An organization's API key (`lba_…`, see [below](#organizations)) works the same way, for that organization's routes only. Every route except `GET /v0/health`, `GET /v0/instance`, `POST /v0/auth/signup`, `POST /v0/setup-links/check`, the public docs routes (`GET /v0/docs…` and `GET /v0/platform…`), `POST /v0/auth/login` and `POST /v0/auth/session` needs one. A missing token gets `401`. A regular account calling an admin-only route gets `403`.

Browsers use `POST /v0/auth/session` instead. It takes the same body and sets an `HttpOnly` session cookie, so the token never reaches JavaScript:

```
POST /v0/auth/session
{"email":"you@example.com","password":"…"}

200 OK
Set-Cookie: __Host-lewbox_session=lbs_…; Path=/; HttpOnly; Secure; SameSite=Strict; Max-Age=604800
{"expires_at":"2026-11-18T15:23:01.2Z","user":{…},"csrf_token":"…"}
```

Over TLS the cookie is named `__Host-lewbox_session` and is `Secure`; on plain HTTP it is `lewbox_session`. A browser session lasts at most 7 days and ends after 12 hours without a request. Send the cookie back and omit `Authorization`; if both are present, `Authorization` wins.

### Fresh proof

Changing a password, creating an access token and creating an organization's admin key also take the caller's own `current_password`. A missing or wrong value is an error whose `details.field` is `current_password`. Wrong guesses share the account's sign-in failure budget, so too many answer `rate_limited`.

### CSRF and request bodies

Browser cookie requests are protected against cross-site request forgery. A state-changing request that the browser marks as cross-site (`Sec-Fetch-Site: cross-site`, or a mismatched `Origin`) gets `403`:

```json
{"error":{"code":"forbidden","message":"cross-origin request rejected"}}
```

A cookie-authenticated `POST`, `PUT`, `PATCH` or `DELETE` must also send the session's CSRF token in an `X-CSRF-Token` header; without it, or with another session's, the answer is `403` (`missing or invalid CSRF token`) and nothing changes. The token comes in the `csrf_token` field of the sign-in (`POST /v0/auth/session`) and sign-up answers, and `GET /v0/profile` returns it again for a cookie session, so a reloaded page can pick it up. It is derived from the session (nothing extra is stored) and changes when the session does. Signing in and signing up need no token.

A cookie-authenticated `POST`, `PUT`, `PATCH` or `DELETE` with a body must also send `Content-Type: application/json` (parameters such as `charset` are allowed); anything else gets `415` with code `bad_request`. Bearer requests are not affected: the CLI sends no `Sec-Fetch-Site` or `Origin` header and needs no CSRF token or special content type; `GET /v0/profile` never returns a token to a bearer caller.

API responses carry `Cache-Control: no-store`.

### Rate limits

Every authenticated request draws on a per-caller budget of 1,800 requests per minute. A signed-in account's sessions and access tokens share one budget, so switching credentials does not buy a fresh one; each organization API key has its own. Over the limit the response is `429` with a `Retry-After` header and:

```json
{"error":{"code":"rate_limited","message":"too many requests; try again in 52 seconds","details":{"retry_after_seconds":52}}}
```

`POST /v0/auth/logout` and `PATCH /v0/users/{user}/password` are exempt, so a stolen session cannot spend the owner's budget and stop them recovering the account. Changing a password still needs fresh proof, which shares the sign-in failure budget.

`POST /v0/auth/login`, `POST /v0/auth/session` and `POST /v0/auth/signup` share two budgets: 20 attempts per client IP per minute, and 5 failed attempts per normalized email per minute (sign-up only uses the per-IP budget). When one is exhausted the response is `429` with a `Retry-After` header and:

```json
{"error":{"code":"rate_limited","message":"too many sign-in attempts; try again in 42 seconds","details":{"retry_after_seconds":42}}}
```

Windows are aligned to their length (the top of the minute for the per-minute budgets, the top of the hour for sign-up), so `Retry-After` says how long is left in the current window, not how long since the first attempt. A per-client budget counts an IPv6 address by its /64, so rotating addresses inside one network does not reset it.

Creating something has its own aligned hourly budget, checked before anything is written or sent to a region: 5 workspaces per account, 100 sandbox keys per workspace, and 100 management (admin) keys per workspace. Instance admins and local mode are exempt from the workspace budget, not from the key budgets. Over one of them the response is `429` with the same body and a message naming what was limited:

```json
{"error":{"code":"rate_limited","message":"too many sandbox keys created; try again in 3233 seconds","details":{"retry_after_seconds":3233}}}
```

An attempt that reaches the limit check counts, even if the write then fails (a bad request and a wrong `current_password` never get that far); a refusal creates nothing. The limits are `--limit-workspaces-per-hour`, `--limit-sandbox-keys-per-hour` and `--limit-admin-keys-per-hour` (or the matching `LEWBOX_LIMIT_*` variables); `0` turns one off.

## Routes

A path parameter takes an ID or a name, and is named after what it holds: `{cluster}` is a cluster slug or ID, `{node}` a node name or ID, `{user}` an account's email or ID, `{dns_profile}` and `{storage_profile}` a profile name or ID, `{member}` a membership ID, an account ID or an email, and `{region}` a cluster slug or ID.

### Auth

| Route | Who | |
|---|---|---|
| `GET /v0/health` | anyone | `{"ok": true}`. `503` while the database can't be reached, so container health checks and the deploy gate see it as not ready |
| `GET /v0/instance` | anyone | `{name, signup}`: the instance's name and sign-up mode, for the sign-in page |
| `POST /v0/auth/login` | anyone | `{email, password}` → `{token, expires_at, user}` |
| `POST /v0/auth/session` | anyone | Same body, sets the browser session cookie → `{expires_at, user}`. The token is only in the cookie |
| `POST /v0/auth/signup` | anyone | `{email, password, setup_token}` → the same body and cookie as `POST /v0/auth/session`. It creates only the account, with no workspace. A valid, unused `setup_token` allows sign-up in any mode and claims the link; without one, `403` when sign-up is closed. An invalid or used token is `422`; a taken email is `409` |
| `POST /v0/auth/logout` | signed in | Ends this session and clears the session cookie → `{"ok": true}`. `400` for an access token or API key, which have no session |
| `GET /v0/profile` | signed in | `{user, organizations, via}` for an account, plus `access_token` when `via` is `access_token`; `{organization, admin_key, via}` for an API key |

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

### Audit trail

Instance admins only. One row per state change, newest first, cursor-paged.

| Route | Who | |
|---|---|---|
| `GET /v0/events` | admin | `{data: [Event], next}`. Cursor-paged and always newest first. `actor` (comma-separated account IDs or emails), `organization` (workspace IDs or slugs) and `action` (event names) each take a comma list; `from` and `to` are RFC 3339 (`to` exclusive); `limit` is 50 by default and at most 200; `before` is the `next` value of the previous page. An unknown filter value matches nothing; a cursor from another scope, or a pruned one, is `400` |

An Event has `id`, `name`, `actor_kind` (`user`, `access_token`, `admin_key`, `local`, `system` or `anonymous`), `actor_id`, `actor_label`, `target_type`, `target_id`, `target_name`, `organization_id`, `organization_slug` (the workspace's slug, when it still exists), `changes` (`{field: {from, to}}` for an update), `ip`, `request_id` and `created_at`.

### Setup links

Instance admins create and revoke them; only the token's hash is stored, so `token` appears only in the create response. The public page's lookup takes the token in the body, so it never reaches a URL or a proxy log.

| Route | Who | |
|---|---|---|
| `POST /v0/setup-links/check` | anyone | `{token}` → what the public setup page shows for an unused, unexpired link: `{instance_name, plan: {name}, region: {name}, expires_at}`. Never a slug or who claimed it. `404` (`this setup link is invalid or has expired`) for an unknown, used or expired link |
| `GET /v0/setup-links` | admin | `{data, pages}`, newest first, never a token. Filters: `status` (unused, claimed, used, expired), `plan`, `region` |
| `POST /v0/setup-links` | admin | `{plan, region, expires_in_hours, note}` → `201` SetupLink plus `token`. `plan` and `region` are a slug or ID; `expires_in_hours` defaults to 168 and is 1–720. Unknown plan or region is `404` |
| `DELETE /v0/setup-links/{setup_link}` | admin | Revoke an unused link → the SetupLink. `409` when it has been used |

### Plans

Instance admins only. A limit is a positive integer; `null` is unlimited and `0` is refused.

| Route | Who | |
|---|---|---|
| `GET /v0/plans` | admin | `{data: [Plan]}`, ordered by slug |
| `POST /v0/plans` | admin | `{slug, name, max_sandboxes, max_vcpus, max_memory_mb}` → `201` Plan |
| `GET /v0/plans/{plan}` | admin | Plan, by ID or slug |
| `PATCH /v0/plans/{plan}` | admin | Any of `slug`, `name` and the three limits. For a limit, absent means no change, `null` means unlimited |
| `DELETE /v0/plans/{plan}` | admin | → the Plan. `409` while a subscription uses it or it is the default plan |

### DNS profiles

Instance admins only. A DNS profile is a saved Cloudflare token, kept sealed and shown only by its last four characters. Creating one, or changing its zone or token, tests it first and saves nothing when the test fails. See [DNS profiles](../clusters/dns-profiles.md).

| Route | Who | |
|---|---|---|
| `GET /v0/dns-profiles` | admin | `{data: [DNSProfile]}`, ordered by name |
| `POST /v0/dns-profiles` | admin | `{name, provider, zone, api_token, acme_email}` → `201` DNSProfile. A failed test → `422` when Cloudflare refuses the values, or `503` when it can't be reached; `details.step` is `zone` or `write` |
| `GET /v0/dns-profiles/{dns_profile}` | admin | DNSProfile, by ID or name |
| `PATCH /v0/dns-profiles/{dns_profile}` | admin | Any of `name`, `zone`, `api_token` and `acme_email`. A new zone or token is tested before it is saved and bumps `version` |
| `DELETE /v0/dns-profiles/{dns_profile}` | admin | → the DNSProfile. `409` while a cluster uses it |
| `POST /v0/dns-profiles/{dns_profile}/check` | admin | Test now → DNSProfile. A failure is recorded in `test_error`, not returned as an error |
| `POST /v0/dns-profiles/{dns_profile}/credentials` | admin | `{name, provider, zone, zone_id, api_token, acme_email, version}`. The only response with the token |

### Storage profiles

Instance admins only. A storage profile is a saved S3-compatible bucket and access key, kept sealed and shown only by its last four characters. Creating one, or changing anything but its name, tests it first and saves nothing when the test fails. See [Storage profiles](../clusters/storage-profiles.md).

| Route | Who | |
|---|---|---|
| `GET /v0/storage-profiles` | admin | `{data: [StorageProfile]}`, ordered by name |
| `POST /v0/storage-profiles` | admin | `{name, provider, endpoint, region, bucket, prefix, path_style, access_key_id, secret_access_key}` → `201` StorageProfile. A failed test → `422` when the bucket refuses the values, or `503` when it can't be reached; `details.step` is `write`, `read`, `list` or `delete` |
| `GET /v0/storage-profiles/{storage_profile}` | admin | StorageProfile, by ID or name |
| `PATCH /v0/storage-profiles/{storage_profile}` | admin | Any of `name`, `endpoint`, `region`, `bucket`, `prefix`, `path_style`, `access_key_id` and `secret_access_key`. A change other than the name is tested before it is saved and bumps `version` |
| `DELETE /v0/storage-profiles/{storage_profile}` | admin | → the StorageProfile. `409` while a cluster uses it |
| `POST /v0/storage-profiles/{storage_profile}/check` | admin | Test now → StorageProfile. A failure is recorded in `test_error`, not returned as an error |
| `POST /v0/storage-profiles/{storage_profile}/credentials` | admin | `{name, provider, endpoint, region, bucket, prefix, path_style, access_key_id, secret_access_key, version}`. The only response with the secret |

### Clusters

| Route | Who | |
|---|---|---|
| `GET /v0/clusters` | admin | `{data: [Cluster]}` |
| `POST /v0/clusters` | admin | Register: `{slug, name, admin_url, endpoint_url, runtime, domain, dns_profile, dns_records, storage_profile, stop_policy, archive_local_retention, archive_snapshot_name, docker_timeout, max_sandbox_cpu, max_sandbox_memory_mb, max_sandbox_disk_gb, idle_stop_minutes, destroy_if_idle_for, max_age, firecracker_vm_slots, admin_token, admin_ip}` → `201` ClusterDetail. `domain`, `dns_profile` and `storage_profile` (IDs or names) and `dns_records` are optional; `admin_ip` (optional) is an IP at which `admin_url`'s host answers, used for the registration checks only when that host doesn't resolve yet (TLS is still verified against the host name; `422` if `admin_url` already names an IP). With a domain and a DNS profile, registering creates the domain's records; `stop_policy` is `keep` or `archive`; `archive_local_retention` is `instant` (default), `1h`, `1d`, `7d` or `never`; `archive_snapshot_name` is `id` (default) or `name`. The advanced settings are optional and described under `PATCH` below. A failed required check → `422` with the checks in `details`, or `503` when the cluster can't be reached; `archive` without a storage profile, or a bad policy, retention, name style or advanced setting, → `422` with the field in `details` |
| `PATCH /v0/clusters/{cluster}` | admin | `{domain, dns_profile, dns_records, storage_profile, stop_policy, archive_local_retention, archive_snapshot_name, docker_timeout, max_sandbox_cpu, max_sandbox_memory_mb, max_sandbox_disk_gb, idle_stop_minutes, destroy_if_idle_for, max_age, firecracker_vm_slots}`: change the settings; a profile `""` or `"none"` clears it, `dns_records` is `auto` or `off`, `stop_policy` is `keep` or `archive`. For an advanced setting, absent leaves it alone, `null` clears it (and `docker_timeout: null` restores `15m`). `docker_timeout` is 1m–2h; the size caps are at least 1 CPU, 1024 MB and 10 GB; `idle_stop_minutes` is a whole number; `destroy_if_idle_for` and `max_age` are durations of at most 720h; `firecracker_vm_slots` is 256–1024, Firecracker only, and at least the current value. An empty body, a domain outside the profile's zone, a bad record mode, policy, retention, name style or advanced setting, `archive` without a profile, or clearing the profile while `archive` is set → `422`. → ClusterDetail |
| `GET /v0/clusters/{cluster}` | admin | ClusterDetail, from the last check |
| `DELETE /v0/clusters/{cluster}?force=true` | admin | Stop managing it → the Cluster. Refuses with `503` if its sandbox keys cannot be revoked, unless `force` is true |
| `POST /v0/clusters/{cluster}/check` | admin | Run the checks now → ClusterDetail |
| `GET /v0/clusters/{cluster}/node-settings` | admin | What every node must apply, with secrets: `{cluster_id, cluster, runtime, stop_policy, archive_local_retention, docker_timeout, max_sandbox_cpu, max_sandbox_memory_mb, max_sandbox_disk_gb, idle_stop_minutes, destroy_if_idle_for, max_age, firecracker_vm_slots, storage, dns}`. `runtime` is the create policy the node writes (`docker` or `firecracker`). The advanced settings are the values the node writes; `null` is off. `storage` is `null` or `{profile, version, endpoint, region, bucket, path_style, root_directory, access_key_id, secret_access_key}`; `dns` is `null` or `{profile, version, zone, domain, api_token, acme_email}` |
| `GET /v0/clusters/{cluster}/health?days=30` | admin | `{cluster: {uptime, days}, nodes: [{name, uptime, days}]}`. A day has `date`, `uptime` (`null` with no samples), `samples` and `down_minutes`; `days` is 1–31 |
| `GET /v0/clusters/{cluster}/metrics?range=24h` | admin | `{range, bucket_seconds, nodes: [{name, data}]}`. A bucket has `start`, averaged `sandboxes`, `cpu_cores`, `cpu_reserved`, `memory_total_mb`, `memory_used_mb`, `disk_free_gb` and the `alive` fraction. `range` is `24h`, `7d` or `30d` |
| `GET /v0/clusters/{cluster}/upgrade` | admin | The current rolling upgrade: `{cluster, cluster_id, id, phase, direction, target_version, previous_version, source, hold, batch, pause_after, allow_downtime, error, created_at, updated_at, nodes: [{node, role, from_version, to_version, state, order, batch, error, migrated, updated_at}]}`. `phase` is `staging`, `running`, `paused`, `done`, `failed`, `stopped`, `finalizing`, `finalized`, `rolling_back`, `rolled_back` or `unknown`; a node's `state` is `pending`, `staged`, `upgrading`, `done`, `failed`, `manual`, `skipped` or `unknown`. A cluster that never upgraded → `404 not_found` |
| `POST /v0/clusters/{cluster}/upgrade` | admin | `{version, batch, pause_after, hold, allow_downtime}` → `201` the rollout. `version` defaults to the AerolVM release this Lewbox release was tested with. `latest` is refused with `422`; a version jump below the release's `upgrade_from_min`, or one older than a node, is refused with `422`; an unreadable release is `503`. With one or two Raft voters AerolVM refuses unless `allow_downtime` is true → `409` with its reason |
| `GET /v0/clusters/{cluster}/upgrade/plan?version=&batch=&pause_after=` | admin | What an upgrade would do, and nothing else: `{cluster, cluster_id, target_version, tested, source, allowed, refusals, warnings, needs_allow_downtime, skew: {upgrade_from_min, below}, one_way_migrations, rollback_after_finalize, batch, batch_size, pause_after, nodes: [{node, role, version, target_version, alive, voter, leader, order, batch, action}], metadata}`. `action` is `upgrade`, `skip_target` or `skip_down`. The plan is refused (`allowed: false`) rather than hidden when the jump is not allowed; an unreadable release is `503` |
| `POST /v0/clusters/{cluster}/upgrade/stop` \| `resume` \| `rollback` \| `finalize` | admin | No body → `200` the rollout after the command. A command the current phase refuses → `409` |
| `GET /v0/overview` | admin | `{customers, sandboxes, capacity, clusters, running, top_customers}` for the console home |
| `POST /v0/clusters/{cluster}/join-tokens` | admin | `{role, ttl}` → `201` JoinToken |
| `GET /v0/clusters/resolve?cluster=REF` | admin | The Cluster for a slug, ID, or any of its nodes' names. Accepts `cluster/x` and `node/x` |

### Nodes

| Route | Who | |
|---|---|---|
| `GET /v0/nodes` | admin | `{data, pages}`. `cluster` and `role` (server, worker, ingress, mixed) take comma lists; `alive=true|false` keeps nodes with that liveness |
| `GET /v0/nodes/{node}` | admin | Node |
| `DELETE /v0/nodes/{node}?force=true` | admin | Drain, remove from the cluster, forget → the Node |
| `POST /v0/nodes` | admin | `{name, url}`: record a node by URL only → `201` Node |
| `PATCH /v0/nodes/{node}/settings` | admin | `{storage_profile, storage_version, dns_profile, dns_version, archive_retention, policy_runtime, advanced}`: the versions a node applied, a name or `null` for each profile, the `archive_local_retention` it applied with its storage (`null` counts as `instant`), the runtime its create policy allows (`docker`, `firecracker` or `null`), and `advanced` (`null` or the advanced settings it applied). A version below 1 with a profile, an unknown `policy_runtime`, or a bad advanced setting, → `422`; a node in no cluster → `409`. → the Node |

### Access tokens

An account's tokens for the API. `?user=` (an ID or email) names another account: instance admins only, and required in local mode. Without it, the routes act on the caller's own account.

| Route | Who | |
|---|---|---|
| `GET /v0/access-tokens` | signed in (not an API key) | `{data, pages}`, newest first, revoked ones included. Filter `status` (active, expired, revoked) |
| `POST /v0/access-tokens` | signed in, not with a token | `{name, expires_in, user, current_password}` → `201` AccessToken plus `token`, shown only here. `expires_in` is like `90d` or `720h`; empty means never. `user` is optional, as `?user=`. `current_password` is the caller's own. A token can't create another (`403`); a disabled account gets `409` |
| `DELETE /v0/access-tokens/{access_token}` | signed in (not an API key) | Revoke by ID or live name → the AccessToken. A token may revoke itself |

### Accounts

| Route | Who | |
|---|---|---|
| `GET /v0/users` | admin | `{data, pages}`. Filters: `status` (active, disabled), `role` (admin, user) |
| `POST /v0/users` | admin | `{email, password, instance_admin}` → `201` User |
| `GET /v0/users/{user}` | admin, or themselves | User |
| `DELETE /v0/users/{user}` | admin | → the User |
| `POST /v0/users/{user}/suspension` | admin | Ends their sessions and revokes their access tokens → User |
| `DELETE /v0/users/{user}/suspension` | admin | → User |
| `PATCH /v0/users/{user}/password` | admin, or themselves | `{password, current_password}` → User. `current_password` is the caller's own, even when an admin changes someone else's |

### Organizations

The member and API key routes act on the organization in the `Lewbox-Organization` header (its slug or ID). An API key acts for its own organization, so it can leave the header out; naming another organization gets `404`. Without either, the request gets `400`.

| Route | Who | |
|---|---|---|
| `GET /v0/organizations` | signed in, or a key | `{data, pages}`: every one for admins, your own otherwise, its own for a key. `user` (admins, or yourself) lists that account's, with its `role` in each. Filters: `status` (pending, active, suspended), `region`, `plan` |
| `POST /v0/organizations` | signed in | `{slug, name, owner, setup_token}` → `201` Organization. `slug` is generated from `name` when it is empty. An instance admin may set `owner` (an email or ID, default you) and gets no default subscription unless it uses a setup token. Anyone else becomes the owner (`owner` is `403`), gets the default plan in the default region when both exist, and must have sign-up `open` (active workspace) or `approval` (pending); `closed` is `403`. With `setup_token`, the workspace is active in any mode, subscribed to the link's plan in its region, and the link is consumed. At most 10 workspaces may be owned by one account (`422`, a quota) |
| `GET /v0/organizations/{organization}` | its members, admins, its keys | Organization |
| `PATCH /v0/organizations/{organization}` | owner or admin; status: admin | `{name}` renames. Instance admins can also set `{status}`; `status` is `active` or `suspended` → Organization |
| `GET /v0/organization` | as for the header's, or the key's own | Organization |
| `GET /v0/members` | its members, admins, its keys | `{data, pages}`. Filter `role` (owner, admin, member, viewer) |
| `POST /v0/members` | owner or admin | `{email, role}` → `201` Member. Only owners add owners |
| `PATCH /v0/members/{member}` | owner or admin | `{role}` → Member. `{member}` is an email, account ID or member ID. The last owner stays (`409`) |
| `DELETE /v0/members/{member}` | owner or admin, or themselves | → the Member |
| `GET /v0/admin-keys` | owner, admin, admins, its keys | `{data, pages}`, newest first, revoked ones included. Filter `status` (active, expired, revoked) |
| `POST /v0/admin-keys` | owner or admin | `{name, expires_in, current_password}` → `201` AdminKey plus `token`, shown only here. `expires_in` is like `90d` or `720h`; empty means never. `current_password` is the caller's own |
| `DELETE /v0/admin-keys/{admin_key}` | owner or admin | Revoke by ID or live name → the AdminKey |
| `GET /v0/sandbox-keys` | its members, admins, its keys | `{data, pages}`, newest first, revoked ones included. Filter `status` (active, expired, revoked) |
| `POST /v0/sandbox-keys` | owner, admin or member, admins, its keys | `{name, region, expires_in}` → `201` NewSandboxKey, with the `avm_…` token shown only here. `region` defaults to the workspace's region; `expires_in` is `90d` (the default), `never` or a duration such as `30d`. `403` if the organization is pending or suspended, `409` if the region is not ready or the new quota has not been pushed successfully yet ("still being set up"); an unsubscribed region is `404`. A cluster failure is `503` and stores nothing |
| `DELETE /v0/sandbox-keys/{sandbox_key}` | owner, admin or member (members only their own), admins, its keys | Revoke the upstream token, then mark it → SandboxKey. A cluster failure is `503` and the key stays live |

"Admin" in the **Who** column is an instance admin, who can do everything in every organization. An API key can never change members or admin keys, but it can list, create and revoke sandbox keys for its own organization. It gets `403` on every route outside that organization (clusters, nodes, accounts, plans, settings). A suspended or pending organization's keys get `403`; its members can only read, and it cannot mint sandbox keys (`403`).

### Sandboxes and usage

These routes take the organization as `?org={slug}` when present, otherwise in the `Lewbox-Organization` header. An instance admin with neither sees the whole fleet. Another organization's sandbox is always `404`, never `403`.

| Route | Who | |
|---|---|---|
| `GET /v0/sandboxes` | its members, admins, its keys | `{data, pages}`, not-gone only, newest first. `region`, `status` and `organization` (admins) take comma lists; `org` is the workspace scope |
| `POST /v0/sandboxes/{sandbox}/stop?region={slug}` | owner, admin or member, admins, its keys | Stop on the cluster, then update the mirror → Sandbox. `region` picks the cluster when the ID is not unique |
| `DELETE /v0/sandboxes/{sandbox}?region={slug}` | same | Delete on the cluster, then mark it gone → Sandbox |
| `GET /v0/snapshots` | its members, admins, its keys | `{data, pages, summary?, partial_nodes: [node]}`. One list of the workspace's snapshots and archived sandboxes, newest first, each with its `kind` (`snapshot` or `archive`). Filters: `kind`, `region`, `organization` (admins) and `status` (pending, pushing, active, error). `summary=include` adds totals over the whole filtered set, before paging. `partial_nodes` names nodes that did not answer and covers only the snapshot half; archives come from the database and are never partial. Without `org`, an instance admin gets every workspace's. Cached for 30 s |
| `DELETE /v0/snapshots/{snapshot}?region={slug}` | owner, admin or member, admins, its keys | Delete on the cluster with a short-lived team token, then revoke it → Snapshot. An `archive-…` name is `422`: delete it through the archive routes |
| `GET /v0/archives/{archive}` | its members, admins, its keys | The archive as a Snapshot of `kind: "archive"`. Another workspace's is `404`, like a missing one; a non-UUID ID is `404` too |
| `POST /v0/archives/{archive}/restore` | owner, admin or member, admins, its keys | Create a new sandbox from the archive's registry ref, with its name, tags and size, and the tag `lewbox.restored_from=<old sandbox id>` → `{archive, sandbox}` where `archive` is a Snapshot. The archive is kept. `403` if the workspace is suspended; `409` if it has no subscription in that region or the archive is not `active`; `422` if the region's plan limit is reached |
| `DELETE /v0/archives/{archive}` | same | Delete the snapshot and its keys in the bucket, then mark the row deleted → the Snapshot as it was before the delete, still with its `active`, `error` or `pending` status. The stored `deleted` state is never returned |
| `GET /v0/usage?from={date}&to={date}&granularity=hour\|day&org={slug}` | its members, admins, its keys | `Usage`: one bucket per hour or day, zero-filled. `to` defaults to now, `from` to 30 days before; at most 400 days |
| `GET /v0/limits` | its members, admins, its keys | `LimitsReport`: one entry per subscribed region, with its plan, limits, live use, quota and last push |

Suspending an organization pushes a blocking quota where it has used compute and stops its running sandboxes on the next sync pass; the sync also stops sandboxes in a region it is no longer subscribed to. Stopped sandboxes still count against limits, as AerolVM counts them.

Snapshots and archived sandboxes share one list and one status vocabulary. The snapshot half is read with short-lived read-only tokens bound to the workspace's AerolVM team, kept in memory only; archives come from the database. Snapshot names are unique per team, so `region` names the workspace's region; without it Lewbox asks that region. A viewer may list both kinds but not delete either.

### Snapshot summary

`GET /v0/snapshots?summary=include` adds a `summary` object over every snapshot the filters kept, before paging:

```json
{
  "count": 131,
  "disk_gb": 640,
  "memory_mb": 262144,
  "failed": 2,
  "workspaces": 3,
  "by_node": [
    {"region": "eu-west", "node": "node-a", "kind": "snapshot", "count": 90, "disk_gb": 400},
    {"region": "eu-west", "node": null, "kind": "archive", "count": 41, "disk_gb": 240}
  ],
  "by_workspace": [
    {"slug": "acme", "name": "Acme Inc", "count": 100, "disk_gb": 500},
    {"slug": "globex", "name": "Globex", "count": 31, "disk_gb": 140}
  ]
}
```

`node` is `null` for an archive (and for a caller who may not see nodes). `failed` counts status `error`; `workspaces` counts distinct owners. Both groups are sorted by `disk_gb` descending, then `count` descending, then label (region, node and kind; or slug). Any other `summary` value is `400`.

### Subscriptions

The organization comes from the `Lewbox-Organization` header. Only instance admins change them; members may list them.

| Route | Who | |
|---|---|---|
| `GET /v0/subscriptions` | its members, admins, its keys | `{data: [Subscription]}`, by region slug |
| `PATCH /v0/subscriptions/{region}` | admin | `{plan, max_sandboxes, max_vcpus, max_memory_mb}` → Subscription. `{region}` is a cluster slug or ID; `plan` is a slug or ID. A workspace is in at most one region, so a different region is `409` (remove the current one first); the same region changes the plan. An absent override keeps the current one (`null` on a create), `null` clears it to the plan's; `0` is refused. Pushes the quota at once (best effort) |
| `DELETE /v0/subscriptions/{region}?force=true` | admin | Unsubscribe → the Subscription. Revokes the organization's live keys there (refusing with `503` unless `force`), pushes the blocking quota and stops its running sandboxes |

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
  "archive_local_retention": "1d",
  "archive_snapshot_name": "id",
  "docker_timeout": "15m",
  "max_sandbox_cpu": 4,
  "max_sandbox_memory_mb": 8192,
  "max_sandbox_disk_gb": 50,
  "idle_stop_minutes": 30,
  "destroy_if_idle_for": "24h",
  "max_age": "168h",
  "firecracker_vm_slots": null,
  "admin_token_last4": "9f3a",
  "status": "ready",
  "version": "v0.7.0",
  "nodes": 2,
  "checked_at": "2026-11-11T15:23:01.2Z",
  "created_at": "2026-11-11T15:22:35.8Z"
}
```

`status` is `ready`, `degraded` or `unreachable`; `status_detail` explains anything but `ready`. The admin token itself is never returned. `domain` is `""` when the cluster has none, `dns_profile` and `storage_profile` are the profiles' **names** or `null`, `dns_records` is `auto` or `off`, `stop_policy` is `keep` or `archive`, `archive_local_retention` is how long nodes keep local copies of archived sandboxes (`instant`, `1h`, `1d`, `7d` or `never`), and `archive_snapshot_name` is `id` (`archive-<sandbox id>`) or `name` (`archive-<sandbox name>-<sandbox id>`). See [Storage profiles](../clusters/storage-profiles.md#local-copies-of-archives).

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

`settings` is `null` for a node in no cluster. Otherwise `applied_at` is the last report (`null` when it never reported), `outdated` says its applied create policy or profile versions are behind its cluster's, and `reasons` (never `null`) lists why, including a missing or different policy runtime.

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
  "running": 2,
  "role": "owner",
  "created_at": "2026-11-11T15:22:35.8Z"
}
```

`status` is `pending`, `active` or `suspended`; `status_reason` is `""` or `admin`. `region` is the workspace's single subscribed region, or `null` when it has none. `running` is the number of the workspace's sandboxes with status `started` in the mirror, across its regions. `team` is only present for instance admins. `role` is your role (or the listed account's) when it is a member.

**Region**: `{slug, name, endpoint_url, runtime, status, stop_policy}` for a cluster customers can use. `stop_policy` is `keep` or `archive`. For a subscribed region it also has `plan` (`{slug, name}`) and `limits`.

**Subscription**: `{region, plan, limits, overrides, created_at}`. `region` is a Region; `limits` are the effective limits (the plan's, with the override); `overrides` (`{sandboxes, vcpus, memory_mb}`) is only present for instance admins. A `null` limit is unlimited or, on an override, the plan's value.

**Plan**: `{id, slug, name, max_sandboxes, max_vcpus, max_memory_mb, subscriptions, default, created_at}`. A `null` limit is unlimited; `subscriptions` is how many subscriptions use it.

**DNSProfile**: `{id, name, provider, zone, zone_id, token_last4, acme_email, version, clusters, tested_at, test_error, created_at, updated_at}`. `provider` is `cloudflare`; `version` goes up when the zone or token changes, so nodes can tell they are out of date. The token itself is never in it.

**StorageProfile**: `{id, name, provider, endpoint, region, bucket, prefix, path_style, access_key_id, secret_last4, version, clusters, tested_at, test_error, created_at, updated_at}`. `provider` is `s3` and an empty `endpoint` means AWS S3; `version` goes up when anything but the name changes, so nodes can tell they are out of date. The secret access key itself is never in it.

**Settings**: `{instance_name, signup, default_plan, default_region}`. `signup` is `open`, `approval` or `closed`; `default_plan` and `default_region` are a plan and cluster slug, or `null`.

**SetupLink**: `{id, plan, region, note, status, claimed_by, workspace, created_at, expires_at, used_at}` plus `token` only in the create response. `plan` is `{slug, name}`, `region` is `{slug, name}`, `status` is `unused`, `claimed`, `used` or `expired`; `claimed_by` is an email or `null` and `workspace` is `{slug, name}` or `null`.

**LimitsReport**: `{regions: [{slug, name, plan, limits, used, quota, blocked, pushed_at, error}]}`, one entry per subscribed region. `used` is `{sandboxes, vcpus, memory_mb}`; `quota` is `null` when none was pushed; `error` is the last push error.

**Instance**: `{name, signup}`; public.

**Profile** (`GET /v0/profile`) for an account is `{user, organizations, via}` where each organization is `{id, slug, name, role, status}`. `via` is `session`, or `access_token` with the token in `access_token`. For an API key it is `{organization, admin_key, via}` instead.

**AccessToken**: `{id, user_id, name, prefix, last4, status, created_at, last_used_at, expires_at, revoked_at}`. `status` is `active`, `expired` or `revoked`. The token is only in the create response.

**Member**: `{id, organization_id, user_id, email, role, created_at}`.

**AdminKey**: `{id, organization_id, name, prefix, last4, status, created_by, created_at, last_used_at, expires_at, revoked_at}`. `status` is `active`, `expired` or `revoked`; `created_by` is an email. The token is only in the create response.

**SandboxKey**: `{id, name, region, token_hint, created_by, created_at, expires_at, revoked_at, expired}`. `region` is `{slug, name, endpoint_url, runtime}`; `token_hint` is `avm_…` and the token's last four characters; `expires_at` and `revoked_at` are `null` when they do not apply. **NewSandboxKey** adds the `token` (shown only in the create response), `env` (`SB_API_URL`, `SB_API_TOKEN`) and, for a `docker` region, `e2b_env` (`E2B_API_URL`, `E2B_SANDBOX_URL`, `E2B_API_KEY`).

**JoinToken**: `{cluster_id, cluster, runtime, version, role, token_role, token, ca_cert_hash, join_urls, expires_at, command}`. `token` is single-use and is shown only in this response.

**Snapshot**: `{id, kind, name, region, organization?, node?, sandbox_name, source_sandbox_id, image, runtime, created_at, archived_at, cpu, memory_mb, disk_gb, tags, status, error, registry_ref, last_restored_sandbox_id, last_restored_at}`. `kind` is `snapshot` or `archive`: an archive is a snapshot of a stopped sandbox kept in the cluster's bucket. `name` is the snapshot name, which for an archive starts with `archive-`; `sandbox_name` and `source_sandbox_id` name the sandbox it was taken from and are empty for a plain snapshot. `organization` (`{slug, name}`) is included for instance admins for both kinds; `node` (the member that holds it) is only on a plain snapshot, since an archive lives in the bucket. `status` is `pending`, `pushing`, `active` or `error`, AerolVM's vocabulary: a plain snapshot passes its push state through unchanged (empty when the cluster has no registry) and an archive maps its stored state (`archived` to `active`, `failed` to `error`). `error` is the push or archive error. `tags` is always an object; `id` (the archive's UUIDv7), `archived_at`, `registry_ref` and `last_restored_*` are empty or `null` for a plain snapshot. AerolVM reports no owner and no size, so a snapshot is attributed to its workspace's team and is not billed.

**RestoredArchive**: `{archive, sandbox}`. `archive` is a Snapshot of kind `archive`. `sandbox` is `{id, name, status, region}`: a **new** sandbox, with the archive's name (plus a short suffix when that name is taken) and `region` (the cluster slug).
