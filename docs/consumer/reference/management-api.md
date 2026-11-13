---
title: Management API
description: The customer-facing /v0 routes, and the lba_ keys that let your backend call them.
---

The portal uses the same HTTP API your backend can. It sends and returns JSON, and unknown fields in a request body are rejected.

- Base URL: your provider's Lewbox address, for example `https://lewbox.example.com/v0`.
- Errors: `{"error":{"code","message","details"}}`.
- Responses carry `Cache-Control: no-store`.

## Authentication

Sign in to get a session token, then send it as a bearer token:

```bash
TOKEN=$(curl -fsS -X POST https://lewbox.example.com/v0/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"…"}' | jq -r .token)

curl -fsS -H "Authorization: Bearer $TOKEN" https://lewbox.example.com/v0/me
```

A browser signs in with `POST /v0/auth/session`, which sets an `HttpOnly` cookie instead of returning the token.

For a backend, create a **management API key** (`lba_…`) instead of storing a person's password. Owners and admins can create them in **Workspace settings → API keys**, or over HTTP with `POST /v0/admin-keys`. The token is shown once; Lewbox keeps only its prefix and last characters. A key lasts 90 days by default, or with no expiry if you leave `expires_in` empty. Revoke it with `DELETE /v0/admin-keys/{id or name}`.

Routes that act on a workspace take it from the `Lewbox-Organization` header (its slug or ID). A management key acts for its own workspace, so it can leave the header out; naming a different workspace gets `404`.

## Workspace and members

| Route | Who | |
|---|---|---|
| `GET /v0/me` | signed in, or a key | `{user, organizations, via}`, or `{organization, admin_key, via}` for a key |
| `POST /v0/organizations` | signed in | `{name, slug?, setup_token?}` → `201` Organization. With a setup link, the workspace starts with the link's plan and region |
| `GET /v0/organizations` | signed in, or a key | `{data: [Organization]}`: yours, or the key's own |
| `GET /v0/organizations/{ref}` | its members, its keys | Organization |
| `PATCH /v0/organizations/{ref}` | owner or admin | `{name}` renames → Organization |
| `GET /v0/organization` | its members, its keys | Organization, from the header or the key |
| `GET /v0/members` | its members, its keys | `{data: [Member]}` |
| `POST /v0/members` | owner or admin | `{email, role}` → `201` Member. Only owners add owners |
| `PATCH /v0/members/{ref}` | owner or admin | `{role}` → Member. The last owner stays (`409`) |
| `DELETE /v0/members/{ref}` | owner or admin, or themselves | → the Member |

Roles are `owner`, `admin`, `member` and `viewer`.

## Sandbox keys

| Route | Who | |
|---|---|---|
| `GET /v0/sandbox-keys` | its members, its keys | `{data: [SandboxKey]}`, live first, revoked ones included |
| `POST /v0/sandbox-keys` | owner, admin or member, its keys | `{name, region?, expires_in?}` → `201` NewSandboxKey, with the `avm_…` token shown only here. `region` defaults to the workspace's region; `expires_in` is `90d`, `never` or a duration such as `30d` |
| `DELETE /v0/sandbox-keys/{ref}` | owner, admin or member (members only their own), its keys | Revoke the key on the region, then mark it → SandboxKey |

A key is refused while the workspace is pending or suspended. A region failure is `502` and stores nothing.

## Sandboxes, snapshots, usage and limits

| Route | Who | |
|---|---|---|
| `GET /v0/sandboxes?region={slug}&status={status}` | its members, its keys | `{data: [Sandbox]}`, not-gone only, newest first, at most 500 |
| `POST /v0/sandboxes/{id}/stop` | owner, admin or member, its keys | Stop on the region, then update the mirror → Sandbox |
| `DELETE /v0/sandboxes/{id}` | same | Delete on the region, then mark it gone → Sandbox |
| `GET /v0/snapshots?region={slug}` | its members, its keys | `{data: [Snapshot], partial_nodes: [name]}`. Partial nodes are the machines that did not answer |
| `DELETE /v0/snapshots/{name}?region={slug}` | owner, admin or member, its keys | Delete on the region → Snapshot. An `archive-…` name is `400`: use the archive routes |
| `GET /v0/archives?region={slug}` | its members, its keys | `{data: [Archive]}`, newest first: the sandboxes stopped on an archive region. Cancelled and deleted rows are not listed |
| `GET /v0/archives/{id}` | its members, its keys | Archive. Another workspace's is `404` |
| `POST /v0/archives/{id}/restore` | owner, admin or member, its keys | Create a new sandbox from the archive → `{archive, sandbox}`. The archive is kept |
| `DELETE /v0/archives/{id}` | same | Delete the archive's snapshot and bucket data → Archive, with `status: "deleted"` |
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

**Archive**: `{id, region, sandbox_id, sandbox_name, image, runtime, cpu, memory_mb, disk_gb, tags, snapshot_name, registry_ref, status, error, created_at, archived_at, last_restored_sandbox_id, last_restored_at}`. `status` is `pending`, `pushing`, `archived` or `failed`; `registry_ref` is the image to restore it by. The restore response's `sandbox` is `{id, name, status, region}`, a new sandbox.

**SandboxKey**: `{id, name, region, token_hint, created_by, created_at, expires_at, revoked_at, expired}`. `token_hint` is `avm_…` and the token's last four characters. **NewSandboxKey** adds the `token`, `env` (`SB_API_URL`, `SB_API_TOKEN`) and, for a Docker region, `e2b_env` (`E2B_API_URL`, `E2B_SANDBOX_URL`, `E2B_API_KEY`).

**Member**: `{id, organization_id, user_id, email, role, created_at}`.

**AdminKey**: `{id, organization_id, name, prefix, last4, status, created_by, created_at, last_used_at, expires_at, revoked_at}`. `status` is `active`, `expired` or `revoked`. The token is only in the create response.

**Subscription**: `{region, plan, limits, overrides, created_at}`. `limits` are the effective ones; `overrides` is only present for instance admins.

**LimitsReport**: `{regions: [{slug, name, plan, limits, used, quota, blocked, pushed_at, error}]}`. `used` is `{sandboxes, vcpus, memory_mb}`.
