# Phase 2: sandbox API keys

**Goal:** a customer creates an API key for a region in one click and gets the env vars to paste. The key is an AerolVM `runner` token bound to the organization's team on that cluster, so it can only see and touch that organization's sandboxes. Lewbox never stores the token.

## AerolVM facts (v0.7.0, verified in source; see DECISIONS D7)

- `POST /v1/tokens {"name","role":"runner","team":"<org team>","expires_in":"90d"|"36500d"}` with the cluster admin token → `201` with the token record and `"token":"avm_…"` (shown once). `name`: 1–64 of `[A-Za-z0-9._-]`, unique per team (`409` if taken). `expires_in` up to `36500d`; Lewbox nodes set `SB_TOKEN_MAX_TTL=876000h`, so `36500d` ("never") works on clusters Lewbox installed. A cluster with the default cap (365d) answers `400`; report that as an `invalid` error telling the operator to raise `SB_TOKEN_MAX_TTL`.
- `503` (with `Retry-After`) means the write may or may not have happened. The existing client already retries `503`; if a retry then gets `409` (name taken), list tokens (`GET /v1/tokens` → `{"tokens":[…]}`, no filter), revoke the one with our name and team, and mint once more.
- `DELETE /v1/tokens/{id}` → `204`, also when it's already gone. Revocation takes effect on servers at once and on workers within about 5 s.
- The team is the organization's `team` column (Phase 1). Lewbox never mints `*`, `admin`, `auditor` or `viewer` tokens for customers.

## Schema (`00007_sandbox_keys.sql`)

```sql
CREATE TABLE sandbox_keys (
    id           TEXT PRIMARY KEY,                    -- UUIDv7
    org_id       TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id   TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    upstream_id  TEXT NOT NULL,                       -- AerolVM tok_…
    last4        TEXT NOT NULL,
    created_by   TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at   DATETIME NOT NULL,
    expires_at   DATETIME,                            -- NULL = never
    revoked_at   DATETIME
);
CREATE INDEX sandbox_keys_org ON sandbox_keys(org_id);
CREATE UNIQUE INDEX sandbox_keys_live_name ON sandbox_keys(org_id, name) WHERE revoked_at IS NULL;
```

The upstream token name is `lbx.<key id>` (40 characters), so Lewbox can always find its own tokens.

## API

All act on the organization named by the `Lewbox-Organization` header (or the API key's own), like members and admin keys.

| Route | Who | What |
|---|---|---|
| `GET /v0/sandbox-keys` | any member, the org's API key, instance admins | `{"data":[SandboxKey]}`, live keys first then revoked, newest first |
| `POST /v0/sandbox-keys {"name","region","expires_in"}` | owner/admin/member, the org's API key, instance admins | Mints the token; returns `NewSandboxKey` |
| `DELETE /v0/sandbox-keys/{ref}` | owner/admin/member (members only their own keys), the org's API key, instance admins | Revokes upstream, then marks revoked. `ref` is the ID or the live key's name |

```json
// SandboxKey
{"id":"…","name":"laptop","region":{"slug":"eu-west","name":"EU West","endpoint_url":"https://eu-west.api.acme.dev","runtime":"docker"},
 "token_hint":"avm_…k3Xz","created_by":"you@example.com","created_at":"…","expires_at":"…"|null,"revoked_at":null,"expired":false}

// NewSandboxKey = SandboxKey + the secret and ready-to-paste settings
{"…":"…","token":"avm_…",
 "env":{"SB_API_URL":"https://eu-west.api.acme.dev","SB_API_TOKEN":"avm_…"},
 "e2b_env":{"E2B_API_URL":"https://eu-west.api.acme.dev/e2b","E2B_SANDBOX_URL":"https://eu-west.api.acme.dev/e2b/runtime","E2B_API_KEY":"avm_…"}}
```

- `region` defaults to the only region when there is exactly one; otherwise it's required.
- `expires_in`: `90d` default, `never`, or a duration like the admin keys' `--expires-in`.
- `e2b_env` is present only for `docker` regions (the E2B facade can't choose a runtime).
- Refused (`412 precondition_failed`) when the organization isn't `active` ("your account is waiting for approval" / "suspended"), or the region isn't `ready`/`degraded`.
- Upstream failure → `502 unavailable` with the cluster's message; nothing is stored.
- Revoke: if the cluster can't be reached, return `502` and keep the key live (fail closed: the token still works, so don't claim it's revoked). If the cluster no longer exists, the row is already gone (cascade).

`RemoveCluster` revokes every live sandbox key on that cluster upstream (best effort) before it deletes the cluster.

## CLI

```
lewbox key list [--org acme]
lewbox key create laptop --region eu-west [--expires-in 30d | --never-expires]
lewbox key revoke laptop
```

`create` prints the token and the `SB_API_URL`/`SB_API_TOKEN` lines (text mode), and the full JSON body with `--json`.

## Done when

- `aerolvmtest` fake supports token create/list/revoke bound to a team (it may already; extend if not) and tests cover: mint with the right team/role/name/expiry, never-expiring, region default, pending/suspended refusal, viewer refusal, member revoking someone else's key refused, upstream failure stores nothing, revoke failure keeps the key live, 503-then-409 recovery, cluster removal revokes keys, another org can't see or revoke the key (404).
- Parity tests for the three CLI commands; docs: new `docs/customers/api-keys.md` (customer-facing, short) and the CLI/API references.
- The four Go checks pass.
