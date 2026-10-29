# Authentication

> Status: Draft · Milestone: Alpha (M0 keys and sessions; M1 node and install tokens; M3 setup sessions and agentd tokens) · Updated: 2026-10-29

How Lewbox knows who is calling: the five principals of the API (user sessions, admin keys, profile keys, sandbox keys, setup sessions), the internal ones (node, install and agentd tokens), how each is presented, checked, cached, revoked and expired, and how the API refuses a credential on a route it can't use. [Permissions](permissions.md) says what each may do once it's known.

## Today

- **Two services authenticate differently.** The gateway checks `lbp_` and `lbs_` keys by asking the app `GET /api/gateway/v1/whoami/{sha256}`, caching answers (found or not) for 60 s per replica with singleflight (`lewbox-gateway-legacy/internal/auth/auth.go`). The app checks `lbt_` keys itself (`app/Http/Middleware/AuthenticateTenantApiKey.php`), node and installation tokens (`AuthenticateNode`, `AuthenticateClusterInstallation`), the gateway's shared token (`AuthenticateGateway`), and dashboard sessions (Laravel's database sessions, passwordless email codes via `harrisonclewis/laravel-passwordless`).
- **The 60-second cache means a revoked key works for up to a minute** on each replica, as does a suspended profile's key. The gateway also passed the consumer's key id to the app on every call so the app could re-check visibility, because the cached answer might be stale.
- **`last_used_at`** was written by the app on each whoami, at most once a minute.
- **Sandbox keys** (`lbs_`, public id `sbk_`) were minted by the dashboard for users, by the app's own jobs (to drive sign-ins, pushes, token refreshes through the public API), and by consumers with a profile key. They show up in activity as actors.
- **One Ed25519 key, `GATEWAY_PORT_LINK_KEY`**, signs both private-port links and agentd's upload tokens; without it, both features are off.
- **No CSRF concerns** on the gateway (no cookies); Laravel's CSRF for the dashboard.

## Goals

- One authentication middleware for every principal, reading straight from Postgres.
- Revocation that takes effect across replicas in about a second, not a minute.
- No credential reaches more than it must; no secret is stored in a form that could be replayed.
- Every principal is visible in logs and activity as who it is, not as a key the system minted for itself.

## Keep, change, drop

| | |
|---|---|
| **Change** | Prefixed tokens, now `<prefix>_<58 lowercase hex><6 base62 checksum>` (was 64 hex), stored as SHA-256 with their last four; shown once. Bearer header; `lewbox.bearer` WebSocket subprotocol. Sandbox keys for browsers, 15 minutes, a subset of their maker's permissions. Expiry on keys. Passwordless sign-in by emailed code |
| **Change** | `lbt_` tenant keys → `lba_` admin keys, which also reach sandboxes ([api](../api.md#decisions)) |
| **Change** | Key lookup from the database, cached a few seconds and invalidated by `NOTIFY`, instead of 60 s over HTTP |
| **Change** | Separate signing keys for port links and agentd tokens, each with a key id for rotation |
| **Change** | Setup links become setup *sessions* the SPA uses against `/v0/profile` |
| **Drop** | The gateway's shared service token. The app's self-minted sandbox keys: Lewbox's own jobs call services directly, and their activity actor is `lewbox` |

---

## Design

### Credentials

| Principal | Credential | Format | Stored as | Lifetime | Made by |
|---|---|---|---|---|---|
| User session | Cookie `__Host-lewbox_session` | 32 random bytes, base64url | SHA-256, in `user_sessions` | 30 days, sliding; 7 days idle | Signing in ([projects/users-and-login](../projects/users-and-login.md)) |
| Admin key | `Authorization: Bearer lba_…` | `lba_` + 58 hex + 6-character checksum | SHA-256 + last four, in `api_keys` | Until revoked, or its `expires_at` | A project owner or admin, in the dashboard only |
| Profile key | `Authorization: Bearer lbp_…` | `lbp_` + 58 hex + 6-character checksum | as above | as above | Sessions and admin keys, `POST …/profiles/{prf}/keys` |
| Sandbox key | `Authorization: Bearer lbs_…` | `lbs_` + 58 hex + 6-character checksum | as above | 15 minutes (`LEWBOX_SANDBOX_KEY_TTL`) | Sessions, admin keys, profile keys: `POST /v0/sandboxes/{sbx}/keys` |
| Setup link | In the URL: `https://<instance>/setup#lbl_…` | `lbl_` + 58 hex + 6-character checksum | SHA-256, in `profile_setup_links` | 7 days, until first used | Sessions, admin keys |
| Setup session | Cookie `__Host-lewbox_setup` | 32 random bytes | SHA-256 | 1 hour from the link's use, not sliding | Exchanging a setup link, `POST /v0/setup-sessions` |
| Install token | `Authorization: Bearer lbi_…` on `/node/v0` | `lbi_` + 58 hex + 6-character checksum | SHA-256, and encrypted so the command can be shown again | Until the installation finishes, is cancelled, or 7 days | Creating a cluster installation |
| Node token | `Authorization: Bearer lbn_…` on `/node/v0` | `lbn_` + 58 hex + 6-character checksum | SHA-256 | Until the node is removed; rotatable | Enrollment |
| agentd token | `Authorization: Bearer <token>` on `/agentd/v0` | `v1.<base64url claims>.<base64url Ed25519 sig>` | Not stored: verified by signature | 24 hours, renewed with each prompt | The control plane, when it prompts agentd |
| Port link | In the link URL | as agentd tokens, audience `portd` | Not stored | 60 seconds to open; its browser session 12 hours | `POST …/ports/{port}/links` ([inside-a-sandbox](../inside-a-sandbox.md)) |

All key-like tokens share one table, `api_keys`, with a `kind` (`admin`, `profile`, `sandbox`), so lookup, revocation, `last_used_at` and auditing are one code path. Their public ids are `key_…`, whatever the kind (legacy's `sbk_` becomes `key_` too; the token's prefix still tells kinds apart).

```sql
api_keys (
  id bigint primary key,
  public_id text unique,          -- key_…
  kind text not null,             -- admin | profile | sandbox
  token_hash bytea unique not null,
  last_four text not null,
  name text,                      -- null for sandbox keys
  project_id bigint not null,
  profile_id bigint,              -- profile and sandbox keys
  sandbox_id bigint,              -- sandbox keys
  parent_key_id bigint,           -- the key that minted a sandbox key
  created_by_user_id bigint,      -- a session that made it
  permissions text[],             -- null = full access (admin and profile keys)
  cluster_access text not null default 'all',  -- all | restricted (profile keys)
  expires_at timestamptz,
  last_used_at timestamptz,
  revoked_at timestamptz,
  created_at timestamptz not null
)
api_key_clusters (api_key_id, cluster_id)      -- restricted cluster access
```

Tokens are 32 bytes from `crypto/rand`, hex-encoded. Their prefixes let secret scanners and logs recognise them; the access log never writes one, only the key's `key_` id.

### Presenting credentials

- **Bearer**, `Authorization: Bearer <token>`, on every surface.
- **WebSockets** may present the token as subprotocols instead, for runtimes (browsers) that can't set headers on a handshake: `Sec-WebSocket-Protocol: lewbox.bearer, <token>`. The server selects only `lewbox.bearer`, so the token is never echoed. A browser on the instance's own origin may also use its session cookie on a WebSocket; the `Origin` header must then be the instance's ([CSRF](#csrf)).
- **Never in a query string.** Tokens in URLs end up in logs and history. The only tokens in URLs are one-use links (setup links in the fragment, port links), and they're exchanged at once for cookies.
- **Cookies** only for sessions and setup sessions, only on the instance's own origin.

If a request carries both a bearer token and a cookie, the bearer token wins and the cookie is ignored (so a page on the dashboard's origin can call the API with a sandbox key, as the dashboard's terminal does, without the session leaking into it).

### How a request is authenticated

`internal/auth.Authenticate(r) (Principal, error)`:

1. **Find the credential**: the `Authorization` header, else the `lewbox.bearer` subprotocol on a WebSocket, else the surface's cookie. None: `401 unauthenticated`.
2. **Check the shape** without touching the database: a known prefix, 58 lowercase hex and a matching 6-character checksum ([foundation/ids-and-keys](../foundation/ids-and-keys.md#checksums)). Anything else: `401 invalid_key`. The surface decides which prefixes it accepts: `/node/v0` only `lbi_`/`lbn_`; `/agentd/v0` only agentd tokens; `/v0` only `lba_`, `lbp_`, `lbs_` and cookies. A node token on `/v0` is `401 invalid_key` (not `wrong_key_kind`; it's not an API credential at all).
3. **Hash** with SHA-256. From here only the hash is used.
4. **Look it up** in the [cache](#the-cache), else in Postgres, with its project, profile, sandbox and their statuses in one query.
5. **Decide**:
   - Not found, revoked, or past `expires_at` (checked against now, even when cached): `401 invalid_key`.
   - A sandbox key whose parent key has been revoked or expired, or whose sandbox is deleted: `401 invalid_key`.
   - Project suspended: `403 project_suspended`. Profile suspended: `403 profile_suspended`. (Found and refused with a reason, rather than "unknown", so the caller knows why.)
6. **Build the principal**: kind, key id, project, profile, sandbox, permissions, the clusters it reaches, the user (for sessions), the setup sections (for setup sessions).
7. **Record use**: `last_used_at` is written at most once a minute per key, by a batched writer, never in the request's path.

The router then checks the principal kind against the route ([wrong principal](#wrong-principal)) and the permission ([permissions](permissions.md)).

### The cache

One gateway-era problem goes away: there's no app to ask. But a database round-trip on every request, including every message of a busy stream's reconnects, is still worth avoiding.

- **In-process, per replica**, keyed by token hash, holding the principal. **TTL 10 s**, and both found and not-found answers are cached (a random unknown key can't become valid; caching "not found" blunts floods of bad keys).
- **Invalidation by `NOTIFY`**: revoking a key, suspending a profile or project, changing a key's clusters, deleting a sandbox, disabling a user or signing a session out sends `NOTIFY auth_changed, '<kind>:<id>'` in the same transaction. Every replica `LISTEN`s and drops matching entries. So revocation takes effect across replicas within about a second; the TTL only bounds the damage if a notification is lost (a replica reconnecting to Postgres flushes its whole cache).
- **Singleflight** per hash, as the gateway did, so a burst of requests with one key makes one query; the query runs detached from the first request's cancellation, bounded by a 2 s timeout.
- **Expiry is checked on every request**, cached or not.
- Size-bounded (LRU, 100k entries); swept each TTL.

### Revocation and expiry

| Event | Effect |
|---|---|
| Revoke an admin or profile key | Refused at once (within ~1 s on every replica). Sandbox keys it minted are refused too. Open streams it authenticated are closed ([streaming](streaming.md#when-a-credential-is-revoked)) |
| A key's `expires_at` passes | Refused from that moment; open streams are closed |
| A sandbox key's 15 minutes pass | Refused from that moment. **Open streams it opened carry on** until they end or the parent key is revoked: a terminal shouldn't die mid-session because its key aged, and the browser mints a new key before it reconnects |
| Suspend a profile | Its keys are refused (`403 profile_suspended`), its streams closed, and its running sandboxes stopped ([profiles](../profiles.md)) |
| Suspend a project | Every key and session of it refused on that project's resources (`403 project_suspended`); members can still sign in and see the suspension |
| Remove a member | Their sessions lose that project at once; sandbox keys they minted for its sandboxes are revoked |
| Disable a user | Their sessions are deleted; sandbox keys they minted are revoked |
| Delete a sandbox | Its sandbox keys are revoked |
| Delete a profile or project | All their keys revoked (rows kept for audit, `revoked_at` set) |

Revoked rows are kept 90 days for audit (who made it, when used last), then pruned. Expired sandbox keys are pruned after a day.

### Sandbox keys

For giving a browser one sandbox: a terminal, a file browser, a chat, a desktop.

- **Minted** with `POST /v0/sandboxes/{sbx}/keys` by any principal that reaches the sandbox, except another sandbox key (a sandbox key can't mint keys). Body: `{"permissions": [...]}`, optional.
- **Permissions**: a subset of the minter's, from the sandbox-key set ([permissions](permissions.md#sandbox-keys)). Left out: every sandbox-key permission the minter has.
- **Answer**: `{"data": {"id": "key_…", "token": "lbs_…", "expires_at": "…", "sandbox": "sbx_…", "permissions": [...], "api_url": "https://<instance>/v0"}}`. `api_url` saves the browser from being configured separately.
- **Reaches** one sandbox: reading it (and its ports, operations, SSH keys), inside it (with its permissions), its conversations, its activity. Another sandbox is `404`; anything else (listing sandboxes, `/v0/profile`, templates, start, stop) is `403 wrong_key_kind`.
- **Not listed, not revocable by id.** They expire. Their parent's revocation revokes them.
- **The dashboard** mints one per user per sandbox, caches it in memory, and mints a new one 2 minutes before expiry or on `401` ([dashboard](../dashboard.md)). Since the dashboard is on the same origin with a session, it *could* call inside-sandbox routes with the session cookie directly; it uses sandbox keys anyway for WebSockets to node-direct connections (Later) and so the same code serves Lewagent-style embedders. (Open question 2.)
- **Lewbox's own jobs never mint them.** Sign-ins and Git token refreshes call the toolbox service directly, and their activity shows actor `lewbox` (`{type: "app"}` in legacy terms).

### Dashboard sessions

- **Signing in**: `POST /v0/auth/codes {email}` sends a 6-digit code (valid 10 minutes, 5 tries, rate-limited by email and by IP); `POST /v0/auth/sessions {email, code}` sets the session cookie and answers the user. Details in [projects/users-and-login](../projects/users-and-login.md), including whether anyone may register.
- **Cookie**: `__Host-lewbox_session`, `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain`. Value is the session's random id; the table stores its hash, user, created, last seen (updated at most every 5 minutes), IP and user agent (for "your sessions"), and expiry.
- **Lifetime**: 30 days, extended on use; ends after 7 days idle; signing out deletes it.
- **A session reaches** every project the user is a member of, with their role there ([permissions](permissions.md#roles)), and `/v0/instance` if the user is an instance admin.
- **Re-authentication** for dangerous actions (deleting a project, creating an admin key): the session must have signed in within the last 15 minutes, or the API answers `403 reauthentication_required` and the dashboard asks for a fresh code. (Open question 3.)

### CSRF

Session-authenticated requests that change something (`POST`, `PUT`, `PATCH`, `DELETE`, and WebSocket handshakes) must pass both:

1. **Origin check**: `Origin` (or, without it, `Referer`) is the instance's own origin. Browsers always send `Origin` on these.
2. **A custom header**: `X-Lewbox-CSRF` equal to the session's CSRF token, which the SPA reads from `GET /v0/me` (and a non-`HttpOnly` cookie `__Host-lewbox_csrf`, double-submit). A cross-origin page can't set custom headers without a CORS preflight, which never allows credentials.

Either failing: `403 csrf_failed`. Bearer-authenticated requests skip CSRF (no ambient credential).

### Setup sessions

For a project's customer to set up their own profile (agent sign-in, Git, SSH keys, telemetry) on a hosted page, without a Lewbox account ([profiles](../profiles.md)).

- A session or admin key makes a link: `POST …/profiles/{prf}/setup-links {sections, return_url}` answers `https://<instance>/setup#lbl_…`. The token is in the fragment, so it never reaches logs or `Referer`.
- The SPA posts it to `POST /v0/setup-sessions {token}`: the link is marked used (one use), and a setup session cookie is set for an hour. A used, expired or unknown link: `401 invalid_key`.
- A setup session reaches only `/v0/profile/…` for its profile, and only the sections its link named (`403 section_forbidden` otherwise). It's CSRF-protected like a dashboard session.
- The page shows the project's name and branding, never Lewbox's ([dashboard](../dashboard.md)).

### Internal tokens

**Install tokens** (`lbi_`) authenticate `/node/v0` while a node installs: enroll, report progress, fetch the installation's settings, send its first CSR. One token per installation; the enrollment endpoint accepts it once; it stops working when the installation is online, failed or cancelled.

**Node tokens** (`lbn_`) authenticate an enrolled node's agent for the rest of its life. Rotation: `PUT /node/v0/node/token` answers a new token; the old one works for 10 minutes more. Optionally (Beta) the node also presents its CA-signed client certificate (mTLS) where the control plane terminates TLS itself ([clusters](../clusters.md)).

**agentd tokens** authenticate agentd's uploads for one conversation:

- `v1.` + base64url(JSON claims) + `.` + base64url(Ed25519 signature over `v1.<claims>`).
- Claims: `{"aud": "agentd", "kid": "…", "sbx": "sbx_…", "cnv": "cnv_…", "exp": <unix>}`.
- Issued with every prompt the control plane hands agentd, valid 24 h, so a long background turn keeps a fresh one.
- Checked by signature, audience, expiry, and that the conversation still exists in that sandbox. `401 invalid_token` otherwise.

**Port links** use the same token format with `aud: portd` and claims `{sbx, port, path, exp, sess}`; portd in the sandbox checks them with the public key it was started with ([inside-a-sandbox](../inside-a-sandbox.md)).

**Signing keys**: two Ed25519 keyrings, `agentd` and `portd`, held encrypted in Postgres ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)), generated on first start (so the old `GATEWAY_PORT_LINK_KEY` setting that turned features off disappears). Each key has a `kid`; tokens name theirs; a rotation adds a key, signs with it, and keeps the old one for verification until the longest-lived token signed by it has expired (24 h for agentd; for portd, until every running portd has been restarted with the new public keys, which happens on each start of the sandbox, or by restarting the proxies).

### Wrong principal

Every route declares the principal kinds it accepts ([api](../api.md#route-map)). A principal of another kind:

| Principal | On a route it can't use | Answer |
|---|---|---|
| Sandbox key | Anything outside its sandbox (lists, profile, templates, start/stop) | `403 wrong_key_kind` |
| Sandbox key | Another sandbox's routes | `404 not_found` |
| Profile key | Project routes (`/v0/projects/…`) | `403 wrong_key_kind` |
| Profile key | Another profile's sandbox | `404 not_found` |
| Admin key | What stays human (admin keys, members, project deletion, GitHub App registration, instance) | `403 wrong_key_kind` |
| Admin key | Another project | `404 not_found` |
| Setup session | Anything outside `/v0/profile` | `403 wrong_key_kind` |
| Session | A project the user isn't a member of | `404 not_found` |
| Session (not instance admin) | `/v0/instance` | `404 not_found` |
| Any API credential | `/node/v0`, `/agentd/v0` | `401 invalid_token` |

The rule: `404` when the principal may not know the resource exists; `403` when the resource is plainly there but the principal is the wrong kind of credential for it.

---

## Security

- **Hashes, not secrets.** A database leak reveals no usable key. SHA-256 (not a slow hash) is right for 256-bit random tokens.
- **Constant-time** comparisons where a secret is compared directly (CSRF tokens, webhook signatures).
- **Prefixes registered with GitHub's secret scanning** (Later) so leaked `lba_`/`lbp_` keys in public repositories are reported and revoked automatically.
- **No key in logs**: the access log writes the `key_` id; the redactor strips anything matching a Lewbox token pattern from logged errors and stderr tails.
- **Brute force**: 256-bit tokens can't be guessed; sign-in codes are rate-limited and limited to 5 attempts each ([rate-limits](rate-limits.md)).
- **Session fixation**: a new session id on every sign-in; signing out deletes it server-side.

## Observability

- Log fields: `principal.kind`, `principal.id` (`key_…`, `usr_…`), `project`, `profile`, `sandbox`.
- Metrics: `lewbox_auth_lookups_total{result=hit|miss|negative}`, `lewbox_auth_refusals_total{code}`, `lewbox_auth_cache_invalidations_total`.
- Activity actors: `{type: "user"|"admin_key"|"profile_key"|"sandbox_key"|"setup_session"|"lewbox", id, name}`; a sandbox key's events name the key's maker as well.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_SANDBOX_KEY_TTL` | `15m` | Sandbox key lifetime |
| `LEWBOX_USER_SESSION_IDLE` | `720h` | A dashboard session ends after this long unused (sliding) ([projects/users-and-login](../projects/users-and-login.md)) |
| `LEWBOX_USER_SESSION_MAX` | `2160h` | A dashboard session ends this long after its sign-in code, however active |
| `LEWBOX_SETUP_LINK_TTL` | `168h` | Setup link lifetime |
| `LEWBOX_SETUP_LINK_SESSION_TTL` | `1h` | Setup session lifetime |
| `LEWBOX_AGENT_TOKEN_TTL` | `24h` | agentd token lifetime |
| `LEWBOX_AUTH_CACHE_TTL` | `5s` | Principal cache, per replica; revocations also invalidate it through `NOTIFY` |

## Testing

- The principal matrix in [api](../api.md#testing).
- Revocation across replicas: two servers on one database; revoke on one; the other refuses within 2 s.
- Expiry while cached; parent revocation of sandbox keys; stream closure on revocation.
- CSRF: cross-origin form posts, missing header, wrong token, WebSocket from another origin.
- Fuzz token parsing.

## Build plan

`internal/auth`: credential extraction; token shapes; `Principal` type; lookup query (sqlc) joining key, project, profile, sandbox; cache with singleflight and `LISTEN auth_changed`; `last_used_at` batcher; session store; CSRF middleware; setup sessions; agentd and portd token signing and verification with keyrings; node and install token checks.

Tasks, M0: tokens and `api_keys`; principal and middleware; cache and invalidation; sandbox keys. M1: sessions, sign-in codes, CSRF; node and install tokens. M3: setup sessions; agentd tokens.

## Open questions

1. ~~**Token checksums.**~~ Decided (2026-10-29): yes, before the alpha: `<prefix>_<58 lowercase hex><6 base62>`, registered with GitHub secret scanning ([foundation/ids-and-keys](../foundation/ids-and-keys.md#checksums)).
2. ~~**The dashboard inside sandboxes.**~~ Decided (2026-10-29): the session cookie works directly on inside-sandbox routes; sandbox keys only for WebSockets to other origins.
3. ~~**Re-authentication for dangerous actions.**~~ Decided (2026-10-29): yes, for making admin keys and deleting projects only.
4. ~~**Sign-in methods.**~~ Decided (2026-10-29): the alpha signs in by emailed code only. GitHub OAuth and passkeys come in the Beta; generic OIDC (SSO for self-hosters) is Later.
