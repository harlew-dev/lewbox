# Profile keys

> Status: Draft · Milestone: Alpha (M1) · Core · Updated: 2026-10-29

A **profile key** (`lbp_…`) is how a project's customer's software, or the project's own backend acting for a profile, uses Lewbox's consumer API: creating sandboxes, running commands, prompting agents. Each key belongs to one profile, reaches all of the profile's clusters or only some, and has full access or a chosen set of permissions. Keys are shown once and kept only as hashes.

## Today

- `profile_api_keys` (`ProfileApiKey`, `key_`): `name`, `cluster_access` (`all`, `restricted` with `profile_api_key_cluster`), `permissions` (JSON list or null for full access), `token_hash`, `last_four`, `created_by`, `last_used_at` (at most once a minute), `expires_at`, soft-deleted when revoked. Design in `lewbox-legacy/docs/profiles.md` § *API keys are hashed, never stored*.
- Made in the dashboard (`Tenant\Profile\ApiKeyController`) or the Admin API (`POST /api/v1/profiles/{prf}/api-keys`).
- Checked by the gateway: hash, `GET /api/gateway/v1/whoami/{sha256}`, cached 60 s with singleflight; the app re-checks visibility per call with the key's id. A revoked key works for up to a minute.
- Public docs: [lewbox.com/docs/getting-started/authentication](https://lewbox.com/docs/getting-started/authentication) (permissions table, sandbox keys, WebSocket subprotocol, errors).
- Permissions (`App\Enums\ApiKey\Permission`): `sandboxes.create`, `sandboxes.update`, `sandboxes.start`, `sandboxes.stop`, `sandboxes.delete`, `sandboxes.exec`, `sandboxes.ports`, `sandboxes.ports.view`, `sandboxes.agents`, `sandboxes.activity`, `sandboxes.activity.view`, `templates.create`, `templates.delete`.
- Lewagent hit a bug where asking for `sandboxes.agents` by name on a sandbox key was refused, and falls back to a key with every permission (`AgentKeyController`).

## Goals

- Same semantics as legacy and the public docs: cluster access, fixed permissions, expiry, hashed storage, last-used tracking.
- Revocation and suspension effective at once on every replica.
- One permission list, one check function, shared with sandbox keys and documented in the OpenAPI spec.

## Design

### Format and storage

`lbp_` + 58 lowercase hex + a 6-character base62 checksum ([foundation/ids-and-keys](../foundation/ids-and-keys.md), decided 2026-10-29). Rows in the shared `api_keys` table with `kind = 'profile'` and `profile_id` set ([projects/admin-keys](../projects/admin-keys.md#data-model)); restricted clusters in `api_key_clusters`. Only the SHA-256 and last four characters are stored; 32 random bytes need no slow hash.

### Cluster access

- **`all`**: every cluster of the project, including ones added later. Most keys.
- **`restricted`**: only the clusters listed when it was made. Fixed afterwards: make a new key to change it.

A cluster a key doesn't reach doesn't exist for it: its sandboxes answer `404 not_found` and are never listed; naming it on create is `422 validation_failed` on `cluster`; the profile's default cluster reads as `null` if the key doesn't reach it. Deleting a cluster removes it from every key's list; a restricted key left with no clusters still authenticates but can create nothing (`422 cluster_required`).

Lewbox picks a cluster for a create without one as the docs say: the client's own default (sent as `cluster`), else the profile's default if the key reaches it, else the only cluster the key reaches, else `422 cluster_required` ([sandboxes/lifecycle](../sandboxes/lifecycle.md)).

### Permissions

A key has **full access** (`permissions` null), including permissions added later, or exactly the ones chosen when it was made. Fixed afterwards.

Every key can read its profile, list and read its sandboxes, their operations, ports and SSH keys, list templates, and list and read conversations. Anything else needs:

| Permission | Allows |
|---|---|
| `sandboxes.create` | Creating sandboxes |
| `sandboxes.update` | Changing a sandbox's metadata, idle timeout and lifetime |
| `sandboxes.start` | Starting a stopped sandbox. Restart needs this and `sandboxes.stop` |
| `sandboxes.stop` | Stopping a running sandbox |
| `sandboxes.delete` | Deleting sandboxes |
| `sandboxes.exec` | Commands, sessions, files and SSH keys: everything inside a sandbox |
| `sandboxes.ports` | Exposing and closing ports |
| `sandboxes.ports.view` | Making links to private ports |
| `sandboxes.agents` | Prompting agents, following turns, replying, cancelling, reading messages, deleting conversations |
| `sandboxes.activity.view` | Reading activity logs |
| `sandboxes.activity` | Reading activity logs and adding app events |
| `sandboxes.keys` | Making sandbox keys (new: today any profile key can) |
| `templates.create` | Creating and rebuilding the profile's own templates |
| `templates.delete` | Deleting them |

`sandboxes.keys` is new. Making a sandbox key hands its permissions to a browser, so a narrow server-side key (say, only `sandboxes.activity.view`) shouldn't be able to mint a key that can do more than it can. Rule: a sandbox key's permissions are a subset of its parent key's, and asking for one the parent lacks is `403 permission_denied` naming it; asking for none gives the parent's (within the sandbox-key set). That fixes Lewagent's fallback bug by design ([sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md)).

Core permissions are constants in `internal/auth/permissions.go`, and plugins register theirs (`git.tokens` by `git/github`, `profiles.credits` by `credits`) through the registry ([plugins/extension-points](../plugins/extension-points.md)); both are generated into the OpenAPI spec's `x-lewbox-permission` on each operation and into the docs table, so the three never disagree ([api/permissions](../api/permissions.md)).

### Authentication path

1. Parse `Authorization: Bearer lbp_…` (or the `lewbox.bearer` WebSocket subprotocol). Wrong shape: `401 invalid_key`. An admin key on a consumer route: `401 wrong_key_kind`.
2. SHA-256 the token, and look it up joined to its profile and project: one indexed query. A small per-replica cache (5 s TTL, singleflight) avoids a query per request on hot paths like file streams; every revoke, expiry, suspension and permission change `NOTIFY`s `api_keys` with the key or profile id, and each replica drops matching entries. So a revoked key fails on the next request, not a minute later.
3. Refuse revoked or expired keys (`401 invalid_key`), a suspended profile (`403 profile_suspended`), a suspended or deleting project (`403 project_suspended` / `401 invalid_key`).
4. Put the principal (project, profile, key id, kind, permissions, reachable cluster ids) in the request context. Handlers call `auth.Require(ctx, perm)` and scope queries by `principal.ProfileID` and `principal.ClusterIDs`.
5. Touch `last_used_at` at most once a minute per key.

### Expiry

Optional `expires_at`, from an hour to 2 years ahead (legacy allowed any). Keys made in the dashboard default to 90 days, changeable (including to none); keys made through the API default to none (decided 2026-10-29). The management API and the dashboard show expiring keys; a project notification fires a week ahead for keys made by the dashboard (keys made by a project's backend for its customers are the backend's to rotate, so no notifications for those unless the project turns them on).

### Rate limits

Per key, 1,200 requests a minute by default for the consumer API, configurable per instance and per project; WebSocket and SSE connections count once when they open. Separate, tighter limits for creates (60 sandboxes a minute per profile) and app activity events (60 a minute per key, as documented). See [api/rate-limits](../api/rate-limits.md).

## API

Management (`/v0/projects/{prj}/profiles/{prf}/keys`; session owner/admin or admin key with `profiles.keys`):

| Method | Path | What |
|---|---|---|
| `GET` | `…/keys` | List: `id`, `name`, `cluster_access`, `clusters`, `permissions`, `last_four`, `created_by`, `last_used_at`, `expires_at`, `created_at` |
| `POST` | `…/keys` | `{ name, cluster_access?, clusters?, permissions?, expires_at? }`; answers with `token` once. Takes `Idempotency-Key`: a replay answers the same key **without** its token (`token: null`) and `Idempotent-Replayed: true` |
| `DELETE` | `…/keys/{key}` | Revoke, `204`; also revokes every sandbox key it made |

Consumer: `GET /v0/profile` includes `key` (the calling key's id, permissions, cluster access, expiry). A key can't list or revoke keys, including itself.

```json
{
  "data": {
    "id": "key_3nT8vQ2mR5xLpY",
    "name": "Default",
    "cluster_access": "restricted",
    "clusters": [{ "id": "clu_dOMNHWyaFGfMBf", "name": "eu-west" }],
    "permissions": ["sandboxes.create", "sandboxes.exec", "sandboxes.delete"],
    "last_four": "9f3a",
    "token": "lbp_4b1d…9f3a",
    "created_by": { "type": "admin_key", "id": "key_…" },
    "last_used_at": null,
    "expires_at": "2027-06-01T00:00:00Z",
    "created_at": "2026-11-10T09:31:00Z"
  }
}
```

### Errors

| Status | Code | When |
|---|---|---|
| `401` | `unauthenticated` | No `Authorization: Bearer` |
| `401` | `invalid_key` | Malformed, unknown, revoked, expired, or its profile or project deleted |
| `401` | `wrong_key_kind` | An admin key on the consumer API |
| `403` | `wrong_key_kind` | A sandbox key outside its sandbox |
| `403` | `profile_suspended`, `project_suspended` | |
| `403` | `permission_denied` | Missing a permission; the message names it |
| `422` | `validation_failed` | Unknown permission, a cluster that isn't the project's, `restricted` with no clusters, `expires_at` out of range |

All `401`s carry `WWW-Authenticate: Bearer realm="lewbox"`.

## Security

- Keys never leave the server but in the create response. They're never logged: the request logger redacts `Authorization` and the `lewbox.bearer` subprotocol's second value.
- The docs warn never to put a profile key in a browser; sandbox keys exist for that.
- A key can't widen itself or mint wider keys.
- Secret scanning: register `lbp_` with GitHub's partner program (Later), and add a revocation endpoint scanners can call.

## Observability

- Audit: `profile_key.created`, `profile_key.revoked` (with who).
- Activity: actions taken with a key carry `actor: { type: "profile_key", id, name }` ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md)).
- Metrics: `lewbox_auth_failures_total{code}`, `lewbox_key_cache_hits_total`.

## Testing

- Each permission against its routes; full access including a permission added after the key.
- Cluster access: restricted keys can't see, list, create on, or filter by other clusters; default cluster reads `null`.
- Revocation, suspension and expiry take effect on a second replica within a second (two servers, one database).
- Sandbox keys can't exceed their parent's permissions.

## Build plan

In `internal/auth`: the shared key table, lookup with NOTIFY-invalidated cache, principal, permission checks; endpoints in `internal/profiles`.

1. Key creation, listing, revoke (with child sandbox keys).
2. Authentication middleware for the consumer API.
3. Permission constants and `x-lewbox-permission` generation.
4. Cluster access scoping helpers used by every sandbox query.
5. Cache invalidation and the two-replica test.

## Open questions

1. ~~**`sandboxes.keys` as a new permission.**~~ Decided (2026-10-29): yes; the API is new, and it's in the [registry](../api/permissions.md). A key with full access has it.
2. ~~**Default expiry.**~~ Decided (2026-10-29): keys made in the dashboard default to 90 days (changeable, including to none); API-made keys default to none.
3. ~~**Changing a key.**~~ Decided (2026-10-29): fixed once made, as the docs say; narrowing in place is Later if asked.
