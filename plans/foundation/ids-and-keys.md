# Ids, keys and tokens

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

Every resource Lewbox shows has a public id; every secret it issues has a recognisable format. This plan defines the id format and every prefix, the token formats and how they're stored, the signed tokens, and request ids. It's the single list: other plans refer to it rather than invent prefixes.

## Today

- **Public ids** (`lewbox-legacy/app/Concerns/HasPublicId.php`): a prefix and 14 characters from `0-9A-Za-z`, chosen with `random_int`, stored in `public_id`, used as the route key. Prefixes per model: `usr_ tnt_ prf_ key_ clu_ cin_ node_ sbx_ op_ ses_ sbk_ evt_ bld_ cnv_ msg_ asi_ psl_ ssh_ psh_ cbk_ ctx_ ntf_ whk_ whe_ whd_`. Both profile and tenant API keys use `key_`.
- **Tokens** (`app/Concerns/IssuesApiTokens.php`): a prefix and 64 lowercase hex characters, stored as `hash('sha256', $token)` with the last four for display. `lbt_` tenant keys, `lbp_` profile keys, `lbs_` sandbox keys. Node tokens and install tokens are unprefixed; install tokens are also stored encrypted so the command can be shown again.
- **The gateway** checks a key's prefix and shape before looking it up (`lbp_` or `lbs_` + 64 hex), hashes it, and asks the app `GET whoami/{sha256}` (cached 60 s).
- **Signed tokens** (`internal/portlink`, `internal/agents`): Ed25519, `base64url(claims).base64url(signature)`. Port links: `{sbx, port, path, exp, sess}`. Agent upload tokens: `{aud: "agentd", sbx, cnv, exp}`. Both signed with `GATEWAY_PORT_LINK_KEY`.
- **Request ids**: `req_` + 24 hex (`internal/requestid`), in `Lewbox-Request-Id`. The app's Admin API doesn't send one yet.

## Goals

- Keep the formats that worked: they're compact, URL-safe, and say what they are at a glance.
- One prefix per kind of thing, never shared. (Both key kinds being `key_` made activity's `actor` ambiguous.)
- Every token's prefix says which API takes it, so a wrong one is refused with a helpful message (`wrong_key_kind`) before any lookup.
- Tokens that secret scanners can find.

## Design

### Public ids

```
sbx_Qm4pL0sX2bN8cV
└┬┘ └─────┬──────┘
prefix   14 × [0-9A-Za-z], from crypto/rand
```

- **Entropy**: 62^14 ≈ 1.2 × 10^25 (≈ 83 bits). Collisions are not a practical concern; the unique index catches the impossible case, and the insert retries.
- **Generated in Go** (`internal/ids`), rejection sampling over `crypto/rand` bytes so each character is uniform.
- **Opaque**: no time, order or count. Lists are ordered by `created_at` and internal `id`, never by public id.
- **Parsed strictly** at the edge: the prefix must match the route's resource (`GET /v0/sandboxes/op_…` is `404 not_found`, not a database lookup), then exactly 14 base62 characters.
- **Typed in Go**: `ids.Sandbox`, `ids.Operation`, … are distinct string types, so an operation id can't be passed where a sandbox id is wanted.

### Prefixes

| Prefix | Resource | Plan | Notes |
|---|---|---|---|
| `usr_` | User | [projects/users-and-login](../projects/users-and-login.md) | |
| `prj_` | Project | [projects](../projects.md) | Was `tnt_` |
| `mem_` | Project membership | [projects/members-and-roles](../projects/members-and-roles.md) | New |
| `inv_` | Project invitation | [projects/members-and-roles](../projects/members-and-roles.md) | New |
| `key_` | Admin key or profile key | [projects/admin-keys](../projects/admin-keys.md), [profiles/profile-keys](../profiles/profile-keys.md) | Shared, as both are long-lived API keys listed side by side in activity actors; the actor's `type` says which. See [open questions](#open-questions) |
| `sbk_` | Sandbox key | [sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md) | |
| `ntf_` | Notification | [projects](../projects.md) | |
| `clu_` | Cluster | [clusters](../clusters.md) | Clusters also have a `name`, e.g. `eu-west`, unique in their project; the API takes either |
| `cin_` | Cluster installation | [clusters/installation](../clusters/installation.md) | |
| `node_` | Node | [clusters/node-agent](../clusters/node-agent.md) | Also AerolVM's `--node-id`, and in DNS names (`node-<id>.<domain>`), so it must stay DNS-safe once lowercased. Kept from legacy |
| `prf_` | Profile | [profiles](../profiles.md) | |
| `ssh_` | SSH key | [profiles/ssh-keys](../profiles/ssh-keys.md) | |
| `psl_` | Setup link | [profiles/setup-links](../profiles/setup-links.md) | |
| `asi_` | Agent sign-in | [profiles/agent-sign-ins](../profiles/agent-sign-ins.md) | |
| `sbx_` | Sandbox | [sandboxes](../sandboxes.md) | Unique across all clusters |
| `op_` | Operation | [sandboxes/operations](../sandboxes/operations.md) | |
| `ses_` | Session | [inside-a-sandbox/sessions](../inside-a-sandbox/sessions.md) | Also the toolbox session's name |
| (slug) | Template | [templates](../templates.md) | e.g. `claude-code`, chosen by its maker |
| `bld_` | Template build | [templates/builds](../templates/builds.md) | |
| `cnv_` | Conversation | [agents/conversations](../agents/conversations.md) | |
| `msg_` | Message (prompt or turn) | [agents/turns-and-events](../agents/turns-and-events.md) | |
| `tsk_` | Background task | [agents/background-tasks](../agents/background-tasks.md) | New; legacy used the agent's own task ids |
| `wtr_` | Worktree | [agents/worktrees](../agents/worktrees.md) | Later |
| `evt_` | Activity event | [activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md) | |
| `whk_` | Webhook endpoint | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) | |
| `whe_` | Webhook event | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) | Also the Standard Webhooks `webhook-id` |
| `whd_` | Webhook delivery | [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md) | |
| `gha_` | GitHub App | [git/github-app](../git/github-app.md) | New: was a polymorphic credential |
| `ght_` | GitHub token issued by the token API (the record, never the token) | [git/tokens](../git/tokens.md#tables) | New |
| `cbk_` | Credit bucket | [credits/balances](../credits/balances.md) (the `credits` plugin) | Beta |
| `txn_` | Credit transaction | [credits/balances](../credits/balances.md) (the `credits` plugin) | Beta. Was `ctx_`, renamed: `ctx` reads as Go's context everywhere in the code |
| `dom_` | Custom domain | | Later |
| `req_` | Request id | [observability](observability.md#request-ids) | 24 hex, not base62 (kept) |

Not public, so no prefix: memberships' internal rows, chunks (agents give their own `external_id`), node metrics, encryption keys, job rows, idempotency keys.

**Plugins' prefixes.** A plugin that has public ids registers its prefixes (`r.IDPrefixes("cbk", "txn")`), and the registry fails start-up on a clash with the core's or another plugin's, so this table stays the one list for first-party plugins and the registry guards third-party ones ([plugins/extension-points](../plugins/extension-points.md)). The ids above owned by plugins: `gha_`, `ght_` (`git/github`), `cbk_`, `txn_` (`credits`).

### Secret tokens

```
lbp_8c1f…(64 lowercase hex)…e09a
└┬┘ └──────────┬──────────┘
kind      32 bytes from crypto/rand
```

| Prefix | Token | Reaches | Lifetime | Plan |
|---|---|---|---|---|
| `lba_` | Admin key | One project's management API, and every sandbox in it | Until revoked or its expiry | [projects/admin-keys](../projects/admin-keys.md) |
| `lbp_` | Profile key | One profile's sandboxes, on its clusters, with its permissions | Until revoked or its expiry | [profiles/profile-keys](../profiles/profile-keys.md) |
| `lbs_` | Sandbox key | One sandbox, with chosen permissions | 15 minutes | [sandboxes/sandbox-keys](../sandboxes/sandbox-keys.md) |
| `lbn_` | Node token | One node's node API | Until the node is removed; rotated on renewal | [clusters/node-agent](../clusters/node-agent.md) |
| `lbi_` | Install token | One cluster installation | 1 hour, once | [clusters/installation](../clusters/installation.md) |
| `lbl_` | Setup link token | One profile's setup page, chosen sections | 7 days, then a 1 h session once used | [profiles/setup-links](../profiles/setup-links.md) |
| (none) | Dashboard session | One user's session | 30 days sliding | [projects/users-and-login](../projects/users-and-login.md) |
| (none) | Login code | One sign-in | 10 minutes, 5 attempts | [projects/users-and-login](../projects/users-and-login.md) |

Legacy `lbt_` tenant keys become `lba_`. Since the API is new (D4), no legacy token works against it. Lewagent and others make new keys ([migration](../migration.md)).

#### Storage

- `token_hash bytea` = SHA-256 of the whole token string (prefix included), unique index; `token_last_four text` for display ("`lbp_…e09a`").
- SHA-256 rather than a slow hash (bcrypt, argon2) because tokens carry 256 bits of randomness: there is nothing to brute-force, and lookups must be fast.
- Lookups by hash, then a constant-time compare isn't needed (the hash is the index key, and knowing a hash prefix gives nothing).
- **Shown once**, at creation. Never retrievable. Install tokens are the exception: they're also stored encrypted so the install command can be shown again until it's used ([secrets-and-encryption](secrets-and-encryption.md#what-is-encrypted)).
- **Last used** is recorded on keys (`last_used_at`), at most once a minute per key, to avoid a write per request.

#### Checking

At the edge, before any lookup:

1. Missing `Authorization: Bearer` → `401 unauthenticated`.
2. Not `<known prefix>_` + 64 lowercase hex → `401 invalid_key`.
3. A known prefix for another API (e.g. a profile key on the management API) → `401 wrong_key_kind`, with a message naming what the route takes.
4. Look up by hash. Unknown, revoked, expired, or its project or profile deleted → `401 invalid_key`. Suspended → `403 profile_suspended` / `project_suspended`.

Full rules: [api/authentication](../api/authentication.md).

#### Lookup cache

Legacy cached lookups for 60 s in the gateway, so a revoked key kept working for a minute. Now the database is local:

- No cache by default. A key lookup is one indexed query.
- If profiling shows it matters, a per-process cache of a few seconds, invalidated on revoke by `NOTIFY lewbox_cache` ([jobs-and-scheduling](jobs-and-scheduling.md#wake-ups)). Revocation is then effectively immediate.

#### Checksums

Now that the format can change, consider GitHub's approach: append a 6-character base62 CRC32 of the random part, e.g. `lbp_<64 hex>_<6 base62>`. Benefits:

- Secret scanners (GitHub's partner programme, gitleaks, trufflehog) can tell a real token from a random string, with few false positives, and report leaks to us.
- The edge can reject typos without a database lookup.

Costs: a slightly longer token, and nothing else. **Recommended**, decided in M0. Then register `lba_`, `lbp_` and `lbs_` with GitHub secret scanning, with a revocation endpoint that revokes reported keys and notifies the project.

### Signed tokens

Some tokens are checked where there's no database: inside a sandbox (portd), or must be verifiable without a lookup on a hot path. These are signed, not stored.

Format, kept from legacy:

```
base64url(json claims) "." base64url(Ed25519 signature over the first part)
```

| Token | Claims | Checked by | Lifetime | Plan |
|---|---|---|---|---|
| Port link | `{kid, sbx, port, path, exp, sess}` | portd in the sandbox, with public keys passed when it starts | 60 s to open; the session it starts lasts 12 h | [inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md) |
| Agent channel token | `{kid, aud: "agentd", sbx, iat, exp}` | The control plane, on `GET /agentd/v0/channel` | 24 h; refreshed over the channel before it expires | [agents/storage-and-uploads](../agents/storage-and-uploads.md) |

- New: `kid`, the signing key's id, so keys can rotate ([secrets-and-encryption](secrets-and-encryption.md#signing-keys)).
- `aud` is required on every kind, and checked, so one kind of token can never be used as another.
- Expiry checked with 30 s of leeway for clock skew.
- Not JWTs: no algorithm negotiation, no header to forge. A fixed, single algorithm.

### Request ids

- `req_` + 24 lowercase hex (12 random bytes), kept from legacy.
- Generated for every request (API, dashboard, node API, hooks), returned as `Lewbox-Request-Id`, logged on every line, stored on activity events and in the metadata of jobs the request enqueues.
- A client may send its own `Lewbox-Request-Id`? **No**: a client-chosen id could collide with or impersonate another. Clients can send `Lewbox-Client-Request-Id`, which is logged beside ours.

## Security

- Every random value from `crypto/rand`. A test asserts `math/rand` isn't imported outside tests.
- Tokens never logged: the access log records the key's `key_` id, never the token. A `slog` handler redacts anything matching a token pattern as a backstop.
- Error messages never echo a token back.
- Revoking a profile key revokes the sandbox keys it made at once (sandbox keys record `created_by_key_id`).

## Observability

- `lewbox_auth_attempts_total{kind,result}`: kind `admin|profile|sandbox|node|session|setup_link`, result `ok|unauthenticated|invalid_key|wrong_key_kind|suspended|expired`.
- Activity records refused requests (`request.refused`) with the key's id when known ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md)).

## Testing

- Public ids: format, uniform distribution (chi-squared over many), strict parsing, wrong-prefix routing.
- Tokens: generation, hashing, last four; every edge refusal; checksum validation (if adopted).
- Signed tokens: round trip; expired; wrong `aud`; unknown `kid`; tampered claims; tampered signature; a port link for another sandbox or port.
- Lint: no `math/rand` in non-test code.

## Build plan

1. `internal/ids`: generation, parsing, typed ids, the prefix registry (this table as code; a test checks no prefix is used twice).
2. `internal/tokens`: generate, hash, last four, parse, checksum.
3. `internal/signing`: Ed25519 claims tokens with `kid` and `aud`.
4. `internal/requestid`: middleware and context helpers (port from the gateway).

## Open questions

1. **Checksummed tokens**? Recommended.
2. **Separate prefixes for admin and profile key ids** (`akey_` / `pkey_`), or keep `key_` for both with the actor's `type` disambiguating? Separate is clearer in logs; `key_` matches the docs.
3. **Project slugs** for dashboard URLs (`/projects/acme`) as well as `prj_` ids?
