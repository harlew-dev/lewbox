---
title: Management API
description: The customer-facing /v0 routes, and the lba_ keys that let your backend call them.
---

The portal uses the same HTTP API your backend can. It sends and returns JSON, and unknown fields in a request body are rejected.

- Base URL: your provider's Lewbox address, for example `https://lewbox.example.com/v0`.
- Errors: `{"error":{"code","message","details"}}`. `400` means the request could not be read; `422` means a value breaks a rule (`details.field` names it) or a quota is reached; `409` means the workspace's current state forbids it; `503` means a region or dependency is down (`Retry-After` says how long to wait).
- Every response carries `X-Request-ID`; a `500` repeats it in `details.request_id`.
- Responses carry `Cache-Control: no-store`.

## Authentication

Sign in to get a session token, then send it as a bearer token:

```bash
TOKEN=$(curl -fsS -X POST https://lewbox.example.com/v0/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"…"}' | jq -r .token)

curl -fsS -H "Authorization: Bearer $TOKEN" https://lewbox.example.com/v0/profile
```

A browser signs in with `POST /v0/auth/session`, which sets an `HttpOnly` cookie instead of returning the token.

For a backend, create a **management API key** (`lba_…`) instead of storing a person's password. Owners and admins can create them in **Workspace settings → API keys**, or over HTTP with `POST /v0/admin-keys` and `{"name": "…", "expires_in": "90d", "current_password": "…"}`. The token is shown once; Lewbox keeps only its prefix and last characters. A key lasts 90 days by default, or with no expiry if you leave `expires_in` empty. Revoke it with `DELETE /v0/admin-keys/{admin_key}`.

For your own scripts across every workspace you belong to, create a personal **access token** (`lbp_…`) in **Account settings → Access tokens**, or with `POST /v0/access-tokens` and `{"name": "…", "expires_in": "90d", "current_password": "…"}`. It acts as you, with your role in each workspace, so send `Lewbox-Organization` to pick one. List yours with `GET /v0/access-tokens`, and revoke one with `DELETE /v0/access-tokens/{access_token}`. A token can't create another token.

Both create calls also take your own `current_password`, to confirm it's you. A missing or wrong value is an error whose `details.field` is `current_password`; too many wrong guesses answer `rate_limited`, with the same budget as signing in.

Every signed-in request draws on a per-caller budget of 1,800 requests per minute. Your sessions and personal access tokens share it, so switching credentials does not reset it; each management key has its own. Over the limit answers `429` with a `Retry-After` header and:

```json
{"error":{"code":"rate_limited","message":"too many requests; try again in 52 seconds","details":{"retry_after_seconds":52}}}
```

Signing out and changing your password are exempt, so a stolen session cannot lock you out of recovering your account. Windows are aligned to the minute, and client IPs are counted by IPv6 /64.

Creating workspaces and keys has its own hourly budget, checked before anything is created or sent to a region: 5 workspaces per account per hour, 100 sandbox keys per workspace per hour and 100 management keys per workspace per hour. Over one of them the response is `429` with a `Retry-After` header and a message naming what was limited:

```json
{"error":{"code":"rate_limited","message":"too many sandbox keys created; try again in 3233 seconds","details":{"retry_after_seconds":3233}}}
```

Windows are aligned to the hour, so `Retry-After` is the time left in the current hour. Instance admins are exempt from the workspace limit but not the key limits. Your provider can turn a limit off or change it on the orchestrator.

Routes that act on a workspace take it from the `Lewbox-Organization` header (its slug or ID). A management key acts for its own workspace, so it can leave the header out; naming a different workspace gets `404`.

## Workspace and members

| Route | Who | |
|---|---|---|
| `GET /v0/profile` | signed in, or a key | `{user, organizations, via}`, or `{organization, admin_key, via}` for a key |
| `POST /v0/organizations` | signed in | `{name, slug?, setup_token?}` → `201` Organization. With a setup link, the workspace starts with the link's plan and region |
| `GET /v0/organizations` | signed in, or a key | `{data: [Organization]}`: yours, or the key's own |
| `GET /v0/organizations/{organization}` | its members, its keys | Organization |
| `PATCH /v0/organizations/{organization}` | owner or admin | `{name}` renames → Organization |
| `GET /v0/organization` | its members, its keys | Organization, from the header or the key |
| `GET /v0/members` | its members, its keys | `{data: [Member]}` |
| `POST /v0/members` | owner or admin | `{email, role}` → `201` Member. Only owners add owners |
| `PATCH /v0/members/{member}` | owner or admin | `{role}` → Member. The last owner stays (`409`) |
| `DELETE /v0/members/{member}` | owner or admin, or themselves | → the Member |

Roles are `owner`, `admin`, `member` and `viewer`.

## Sandbox keys

| Route | Who | |
|---|---|---|
| `GET /v0/sandbox-keys` | its members, its keys | `{data: [SandboxKey]}`, live first, revoked ones included |
| `POST /v0/sandbox-keys` | owner, admin or member, its keys | `{name, region?, expires_in?}` → `201` NewSandboxKey, with the `avm_…` token shown only here. `region` defaults to the workspace's region; `expires_in` is `90d`, `never` or a duration such as `30d` |
| `DELETE /v0/sandbox-keys/{sandbox_key}` | owner, admin or member (members only their own), its keys | Revoke the key on the region, then mark it → SandboxKey |

A key is refused with `403` while the workspace is pending or suspended, and with `409` while its region or quota is still being set up. A region failure is `503` and stores nothing.

## Sandboxes, snapshots, usage and limits

| Route | Who | |
|---|---|---|
| `GET /v0/sandboxes?region={slug}&status={status}` | its members, its keys | `{data: [Sandbox]}`, not-gone only, newest first, at most 500 |
| `POST /v0/sandboxes/{sandbox}/stop` | owner, admin or member, its keys | Stop on the region, then update the mirror → Sandbox |
| `DELETE /v0/sandboxes/{sandbox}` | same | Delete on the region, then mark it gone → Sandbox |
| `GET /v0/snapshots?region={slug}` | its members, its keys | `{data: [Snapshot], partial_nodes: [name]}`. One list of the workspace's snapshots and archived sandboxes, newest first, each with its `kind` (`snapshot` or `archive`) and `status`. Partial nodes are the machines that did not answer; archives are never partial |
| `DELETE /v0/snapshots/{snapshot}?region={slug}` | owner, admin or member, its keys | Delete on the region → Snapshot. An `archive-…` name is `422`: use the archive routes |
| `GET /v0/archives/{archive}` | its members, its keys | The archive as a Snapshot of `kind: "archive"`. Another workspace's is `404` |
| `POST /v0/archives/{archive}/restore` | owner, admin or member, its keys | Create a new sandbox from the archive → `{archive, sandbox}`, where `archive` is a Snapshot. The archive is kept |
| `DELETE /v0/archives/{archive}` | same | Delete the archive's snapshot and bucket data → the Snapshot as it was before the delete, with its `active`, `error` or `pending` status |
| `GET /v0/usage?from={date}&to={date}&granularity=hour\|day` | its members, its keys | Usage: one bucket per hour or day, zero-filled. `to` defaults to now, `from` to 30 days before; at most 400 days |
| `GET /v0/limits` | its members, its keys | LimitsReport: the workspace's region, its plan, effective limits, live use and quota |
| `GET /v0/regions` | signed in, or a key | `{data: [Region]}`: the regions the workspace can use, with its `plan` and `limits` |
| `GET /v0/subscriptions` | its members, its keys | `{data: [Subscription]}`. A workspace has at most one |

Another workspace's sandbox is always `404`, never `403`.

## Objects

**Organization**

```json
{
  "id": "01a1cb34-29a5-74c3-9691-5394d021b0f2",
  "slug": "acme",
  "name": "Acme Inc",
  "status": "active",
  "status_reason": "",
  "region": {"slug": "eu-west", "name": "EU West", "plan": {"slug": "pro", "name": "Pro"}},
  "members": 3,
  "role": "owner",
  "created_at": "2026-11-11T15:22:35.8Z"
}
```

`status` is `pending`, `active` or `suspended`; `status_reason` is `""` or `admin`. `region` is `null` until the workspace has a plan.

**Region**: `{slug, name, endpoint_url, runtime, status, stop_policy}` for a region the workspace can use. `stop_policy` is `keep` or `archive`: on an `archive` region a stopped sandbox is archived to storage. A subscribed region also has `plan` (`{slug, name}`) and `limits`.

**Snapshot**: `{id, kind, name, region, sandbox_name, source_sandbox_id, image, runtime, created_at, archived_at, cpu, memory_mb, disk_gb, tags, status, error, registry_ref, last_restored_sandbox_id, last_restored_at}`. `kind` is `snapshot` or `archive`; an archive is a snapshot of a stopped sandbox kept in the region's storage. `name` is the snapshot name: for an archive it starts with `archive-` and ends with the sandbox ID, and depending on the region may include the sandbox's name too, as `archive-<name>-<id>`. `sandbox_name` and `source_sandbox_id` name the sandbox it came from. `status` is `pending`, `pushing`, `active` or `error` for both kinds; a plain snapshot's is empty when its region has no registry. `error` explains an error. `registry_ref` is the image to restore an archive by. `tags` is always an object; `id`, `archived_at`, `registry_ref` and `last_restored_*` are empty or `null` for a plain snapshot. The restore response's `sandbox` is `{id, name, status, region}`, a new sandbox.

**SandboxKey**: `{id, name, region, token_hint, created_by, created_at, expires_at, revoked_at, expired}`. `token_hint` is `avm_…` and the token's last four characters. **NewSandboxKey** adds the `token`, `env` (`SB_API_URL`, `SB_API_TOKEN`) and, for a Docker region, `e2b_env` (`E2B_API_URL`, `E2B_SANDBOX_URL`, `E2B_API_KEY`).

**Member**: `{id, organization_id, user_id, email, role, created_at}`.

**AdminKey**: `{id, organization_id, name, prefix, last4, status, created_by, created_at, last_used_at, expires_at, revoked_at}`. `status` is `active`, `expired` or `revoked`. The token is only in the create response.

**Subscription**: `{region, plan, limits, overrides, created_at}`. `limits` are the effective ones; `overrides` is only present for instance admins.

**LimitsReport**: `{regions: [{slug, name, plan, limits, used, quota, blocked, pushed_at, error}]}`. `used` is `{sandboxes, vcpus, memory_mb}`.
