# Sandbox keys

> Status: Draft · Milestone: Alpha (M2) · Updated: 2026-10-29

A sandbox key (`lbs_…`) reaches **one sandbox** for **15 minutes**, with the permissions it was minted with. It's how a browser works inside a sandbox (a terminal, a file browser, an agent chat, a private-port link) without ever holding a profile or admin key. A project's backend mints one and hands it to the page; the dashboard mints them for its signed-in users. This plan covers what they reach, who mints them, their lifetime and revocation, and how they're checked.

## Today

- **The table** is `sandbox_keys` (`lewbox-legacy/database/migrations/2026_10_19_195558_create_sandbox_keys_table.php`, `app/Models/SandboxKey.php`):
  - public id `sbk_`, token `lbs_` plus 64 hex, stored as a SHA-256 hash with its last four characters;
  - `permissions` (JSON);
  - `created_by` (a user) or `profile_api_key_id` (the profile key that minted it);
  - `expires_at`.

  Expired keys are pruned hourly.
- **Minted three ways** (`lewbox-legacy/docs/api.md` § Sandbox keys):
  1. The dashboard, for a signed-in user (`Tenant\Sandbox\KeyController@store`). The key is kept in the session and reused until 2 minutes before it expires (`reuse_margin_minutes`).
  2. The app's own jobs, which minted keys to call the gateway: sign-ins, token refreshes, pushes (all gone: Lewbox's own jobs call the toolbox directly, and pushes left Lewbox, [git](../git.md)).
  3. A consumer with a profile key: `POST /v1/sandboxes/{sbx}/keys`, with `permissions` at most the profile key's own.
- **What it reaches.** Reading its sandbox, and the inside routes (commands, sessions, files, ports, SSH keys, conversations, activity), within its permissions.
  - Another sandbox is `404`.
  - Anything else (lists, create, start, stop, delete, `/v1/profile`, templates) is `403 wrong_key_kind`.
  - It can't mint another key.
  - It stops working when the profile key that minted it is revoked or expires.
- **Checked by the gateway** through the app's `whoami/{sha256}`, cached for 60 s.
- **Lewagent** mints one per chat with `['sandboxes.agents', 'sandboxes.exec']`. Because "the gateway still refuses `sandboxes.agents` asked for by name", it falls back to a key with no scopes, which means every permission the profile key has (`lewagent-legacy/app/Http/Controllers/Computer/AgentKeyController.php`). That's a bug that widened every browser key.
- **Activity** records a key's actions as the actor who minted it.

## Goals

- A browser gets exactly the access it needs, to one sandbox, for minutes, and nothing else.
- Every permission a sandbox key can have can be asked for by name.
- Revoking the key that minted it ends it at once (no 60-second cache).
- Lewbox's own code never mints keys to call itself.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Prefix and format. One sandbox. 15 minutes by default. Permissions a subset of the minter's. Ends with the minting key. Never listed. `404` for other sandboxes, `403 wrong_key_kind` for the rest. Works on any origin (CORS), and on WebSockets via the `lewbox.bearer` subprotocol |
| **Change** | Minted by **admin keys** too (for any sandbox in the project). Optional `ttl` (1 to 60 minutes). Optional `conversation` scope (open question). Checked by a direct database lookup, cached at most 5 s, and revocation of the minter is instant. Every permission validated by one list, which fixes Lewagent's `sandboxes.agents` bug. `DELETE` to end one early |
| **Drop** | Keys minted by Lewbox for itself. The 60 s `whoami` cache |

## Design

### What a sandbox key can have

| Permission | Allows, in its sandbox |
|---|---|
| `sandboxes.exec` | Commands, sessions, files, directories, SSH keys |
| `sandboxes.ports` | Exposing and closing ports |
| `sandboxes.ports.view` | Making links to private ports |
| `sandboxes.agents` | Prompting agents, following turns, replying, cancelling, reading and deleting the sandbox's conversations |
| `sandboxes.activity.view` | Reading the sandbox's activity |
| `sandboxes.activity` | Reading activity and adding app events |
| `sandboxes.git` | Changing the sandbox's Git setting, up to the project's cap ([git/tokens](../git/tokens.md#the-sandboxs-git-setting)) |

Touching the sandbox (`POST /v0/sandboxes/{sbx}/touch`) needs no permission of its own: any of `sandboxes.exec`, `sandboxes.ports.view` or `sandboxes.agents` allows it ([api/permissions](../api/permissions.md#sandbox-permissions)).

Every key can read its sandbox (`GET /v0/sandboxes/{sbx}`), its ports and its operations, as any key that sees the sandbox can.

Asking for a permission the minter doesn't have is `403 permission_denied` naming it. Asking for none gives every permission above that the minter has (as now). Unknown names are `422 validation_failed`.

### Minting

`POST /v0/sandboxes/{sbx}/keys`

| Field | Type | |
|---|---|---|
| `permissions` | array | Optional. Names from the table above |
| `ttl` | integer | **New.** Seconds, 60 to 3600. Default 900 |
| `label` | string | **New.** Optional, up to 64 chars, recorded in activity (e.g. `chat`, `terminal`) |

Answers `201`:

```json
{
  "data": {
    "id": "key_…",
    "token": "lbs_…",
    "sandbox": "sbx_…",
    "permissions": ["sandboxes.agents", "sandboxes.exec"],
    "expires_at": "2026-12-01T10:15:00Z",
    "api_url": "https://api.lewbox.com/v0"
  }
}
```

`token` is shown only here. `api_url` is the URL the request came in on, so a page on a custom domain gets that domain.

| Minter | May mint for | Permissions up to | Recorded as |
|---|---|---|---|
| Profile key | Its profile's sandboxes on clusters it reaches | Its own | `minted_by_key_id` |
| Admin key | Any sandbox in its project | All of the table | `minted_by_key_id` |
| Dashboard user | Any sandbox in the project their role allows ([projects/members-and-roles](../projects/members-and-roles.md)) | Their role's | `minted_by_user_id` |
| Sandbox key | Never: `403 wrong_key_kind` | | |

The sandbox may be in any status; a key for a stopped sandbox works when it starts, if it hasn't expired.

### The dashboard

The React dashboard asks its own session endpoint for a key per sandbox (`POST /v0/sandboxes/{sbx}/keys` with the session cookie and a CSRF token). It keeps the key in memory, and asks again 2 minutes before expiry or on a `401`. There's one key per sandbox per tab; the server doesn't cache them in the session, as legacy did. Simpler: a mint is one insert.

### Checking a key

On every request with an `lbs_` bearer (in `internal/auth`; [api/authentication](../api/authentication.md)):

1. Check the shape: `lbs_` plus 64 lowercase hex.
2. Look up `sandbox_keys` by `sha256(token)`, joined to its sandbox and its minter, in one query. Results are cached in memory for up to `LEWBOX_AUTH_CACHE_TTL` (5 s), keyed by hash.
3. Refuse with `401 invalid_key` if:
   - it isn't found;
   - it's expired (checked on every request, even when cached);
   - it was ended with `DELETE`;
   - its minter is revoked or expired, or the minting user lost access to the project;
   - its profile is deleted.
4. Refuse with `403 profile_suspended` if its profile is suspended.
5. Route check:
   - the path's `{sbx}` isn't the key's: `404 not_found`;
   - the route isn't one of the inside-a-sandbox routes, or `GET` of the sandbox, its ports and its operations: `403 wrong_key_kind`.
6. Permission check as for any key.

Revoking a profile or admin key bumps a `keys_revoked` notification (`NOTIFY auth_revoked`), and every replica drops its cache at once. So revocation is instant, not after up to 60 s.

### Ending a key early

**New:** `DELETE /v0/sandboxes/{sbx}/keys/{key}`, by the minter or an admin key, answers `204`. For a page that's closed, or a user signed out of a project's app. The hash row gets `revoked_at`.

Keys aren't listed: they're short-lived, and listing would invite keeping them.

### Pruning

A daily job deletes rows expired for more than 7 days. Activity events keep the key's public id, so they stay attributable.

## Data model

```sql
CREATE TABLE sandbox_keys (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id         text NOT NULL UNIQUE,               -- key_…
  project_id        bigint NOT NULL REFERENCES projects,
  sandbox_id        bigint NOT NULL REFERENCES sandboxes ON DELETE CASCADE,
  token_hash        bytea NOT NULL UNIQUE,               -- sha256
  last_four         text NOT NULL,
  permissions       text[] NOT NULL,
  label             text,
  minted_by_key_id  bigint REFERENCES api_keys,          -- profile or admin key
  minted_by_user_id bigint REFERENCES users,
  expires_at        timestamptz NOT NULL,
  revoked_at        timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CHECK ((minted_by_key_id IS NULL) <> (minted_by_user_id IS NULL))
);
CREATE INDEX ON sandbox_keys (expires_at);
```

The public id becomes `key_` like every other key, rather than `sbk_` ([foundation/ids-and-keys](../foundation/ids-and-keys.md)). The token prefix stays `lbs_`.

## API

| Endpoint | Does | Who |
|---|---|---|
| `POST /v0/sandboxes/{sbx}/keys` | Mint | Profile key, admin key, dashboard |
| `DELETE /v0/sandboxes/{sbx}/keys/{key}` | End early | The minter, or an admin key |

| Status | Code | When |
|---|---|---|
| `401` | `invalid_key` | Unknown, expired, ended, or its minter is revoked |
| `403` | `wrong_key_kind` | A sandbox key used outside its sandbox's routes, or to mint |
| `403` | `permission_denied` | Asking for a permission the minter lacks |
| `404` | `not_found` | Another sandbox, or one the minter can't see |
| `422` | `validation_failed` | Unknown permission, `ttl` out of range |

## Security

- **Bounded:** one sandbox, at most an hour, and a subset of the minter's permissions.
- **Never in cookies,** so CORS can allow every origin without CSRF risk. The docs tell projects to mint server-side, and never to expose profile or admin keys to browsers.
- **Instant revocation** when the minter is revoked.
- **Attribution:** activity records the sandbox key's id, its label, and the actor it was minted for (the user or the minting key), so "who ran this command" has an answer.
- **Rate limits:** minting is limited per minting key ([api/rate-limits](../api/rate-limits.md)), default 600 a minute.

## Observability

- **Metrics:** `lewbox_sandbox_keys_minted_total{minter=profile_key|admin_key|user}` and `lewbox_sandbox_key_auth_total{outcome}`.
- **Activity:** `key.created` per mint (as legacy), with permissions, `ttl` and label. It's grouped under `keys`, and the dashboard hides it by default as noise.

## Configuration

| Variable | Default |
|---|---|
| `LEWBOX_SANDBOX_KEY_TTL` | `15m` |
| `LEWBOX_SANDBOX_KEY_MAX_TTL` | `60m` |
| `LEWBOX_AUTH_CACHE_TTL` | `5s` |

## Testing

- Each minter's limits: a subset of the minter's permissions; every permission minted by name, including `sandboxes.agents` (a regression test for Lewagent's fallback).
- Route matrix: every route in the API with a sandbox key, for its own sandbox and another, asserting `200`/`404`/`403`. It's generated from the router, so a new route can't forget the rule.
- Revoking the minting key: the next request is `401`, on another replica too (two servers in the test).
- Expiry with a cached key; `DELETE`; WebSocket auth via subprotocol.

## Build plan

1. Migration and `internal/auth` support for `lbs_` (with [api/authentication](../api/authentication.md)).
2. Mint and end endpoints; the dashboard's use of them.
3. The route classifier (sandbox-scoped or not), generated from route metadata.
4. Revocation notifications and cache.
5. The route-matrix test.

## Open questions

1. **Conversation-scoped keys.** Lewagent's chat page needs `sandboxes.agents` for one conversation, not every conversation in the sandbox. Add an optional `conversation` to mint, limiting agent routes to it? Cheap, and shrinks the blast radius.
2. **`sandboxes.exec` is broad.** A terminal needs it, and a file browser needs only files. Split `sandboxes.exec` into `sandboxes.commands` and `sandboxes.files` (with `exec` implying both)? Would need a matching change on profile keys ([api/permissions](../api/permissions.md)). Decided for the alpha: keep `sandboxes.exec`; the split stays open for after it.
3. **Longer keys for long pages.** 60 minutes max, with re-minting. Is that enough for a desktop view open all day? Private-port *sessions* already last 12 hours independently ([inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md)), so probably.
