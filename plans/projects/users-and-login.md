# Users and sign-in

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

A **user** is a person who signs in to the dashboard. They sign in with their email address and a one-time code we email them; there are no passwords. A signed-in user has a **session**, kept in Postgres and carried by a cookie, which the dashboard uses to call the same API as admin keys do. This plan covers users, codes, sessions, the dashboard's protection against cross-site requests, and the user's own account.

## Today

- `users` (`database/migrations/0001_01_01_000000_create_users_table.php`): `name`, `email` (unique), `email_verified_at`, and a `password` column Laravel made that nothing uses.
- Sign-in is `harrisonclewis/laravel-passwordless` in code mode (`config/passwordless.php`): a 6-digit code, 5 wrong guesses per code, then it's dead; the verify endpoint is rate-limited; the mail is `MAIL_MAILER=log` by default, so a fresh install can't sign in without reading the log.
- Anyone can register: entering a new email creates the user. Then `/tenants/select` or `/tenants/create`.
- Laravel's `sessions` table (database driver), cookie-based, with Laravel's CSRF tokens on every form.
- Login tokens are pruned daily (`model:prune`).

What's wrong:

- Signing up and signing in are the same step, so a self-hosted instance is open to anyone who finds it.
- No way to change one's email, see one's sessions, or sign out elsewhere.
- No second factor, and codes are the only way in (fine for now).

## Goals

- Passwordless sign-in by emailed code, as today, hardened: rate limits by email and by IP, attempts per code, short expiry, constant-time checks, no account enumeration.
- Sign-up governed by the instance's policy ([instance-administration](instance-administration.md#policies)).
- Sessions in Postgres, listable and revocable by the user.
- The dashboard calls `/v0` with its session cookie, safely: CSRF protection without tokens in every form.
- Re-authentication for sensitive actions.
- Room for OAuth (GitHub, Google), passkeys and OIDC SSO later, without changing the user model.

### Non-goals

- Passwords. Never.
- SAML and SCIM in the alpha.

## Design

### Data model

```sql
create table users (
  id               bigint generated always as identity primary key,
  public_id        text not null unique,             -- usr_…
  email            citext not null unique,
  name             text,                              -- asked for after first sign-in, optional
  email_verified_at timestamptz,                      -- set on first successful code
  last_project_id  bigint references projects(id) on delete set null,
  last_signed_in_at timestamptz,
  status           text not null default 'active',    -- active | disabled
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now()
);

create table sign_in_codes (
  id            bigint generated always as identity primary key,
  email         citext not null,
  code_hash     bytea not null,          -- HMAC-SHA256(server key, code)
  purpose       text not null,           -- sign_in | reauthenticate | change_email
  attempts      int not null default 0,
  expires_at    timestamptz not null,
  consumed_at   timestamptz,
  ip            inet,
  created_at    timestamptz not null default now()
);
create index on sign_in_codes (email, created_at desc);

create table user_sessions (
  id              bigint generated always as identity primary key,
  public_id       text not null unique,       -- uss_…, shown in "Your sessions"
  user_id         bigint not null references users(id) on delete cascade,
  token_hash      bytea not null unique,      -- SHA-256 of the cookie's value
  authenticated_at timestamptz not null,      -- last time a code was entered
  last_seen_at    timestamptz not null,
  expires_at      timestamptz not null,
  ip              inet,
  user_agent      text,
  created_at      timestamptz not null default now(),
  revoked_at      timestamptz
);

create table user_identities (                -- Later: OAuth, OIDC, passkeys
  id          bigint generated always as identity primary key,
  user_id     bigint not null references users(id) on delete cascade,
  provider    text not null,                  -- github | google | oidc:<project> | passkey
  subject     text not null,                  -- the provider's user id, or the credential id
  data        jsonb not null default '{}',    -- e.g. passkey public key, sign count
  created_at  timestamptz not null default now(),
  unique (provider, subject)
);
```

`citext` keeps emails case-insensitive. Emails are also normalized (trimmed, Unicode NFC) before they're stored.

### Signing in

```
POST /v0/auth/codes            { email }                 → 202 always
POST /v0/auth/sessions         { email, code }           → 201 + Set-Cookie
DELETE /v0/auth/sessions/current                          → 204, clears the cookie
```

1. **Ask for a code.** `POST /v0/auth/codes` answers `202` whatever the email, so it doesn't reveal who has an account. Behind it:
   - The instance's sign-up policy decides whether an unknown email may get a code at all (`open`; `invite_only`, only if it has a pending invitation; `allowlist`, only from the listed domains). An email that may not sign up gets no code but the same `202`; the sign-in page says "If you can sign in, we've sent a code".
   - Any earlier unconsumed codes for the email are expired, so only the newest works.
   - A code is 6 digits from `crypto/rand`, stored as an HMAC with a server key, never in the clear, and emailed with the instance's name ("Your Lewbox sign-in code is 482 913. It expires in 10 minutes."). The email also carries a one-click link that fills the code in, for people who'd rather click; the link only pre-fills the form, it doesn't sign in, so mail scanners can't use it.
2. **Enter it.** `POST /v0/auth/sessions` checks the newest unconsumed, unexpired code for the email in constant time. A wrong code adds an attempt; after 5 the code is dead and a new one must be asked for. The right code is consumed, the user is created if they don't exist (and the policy allows), `email_verified_at` is set, and a session is made.
3. The response sets the session cookie and answers with the user and their projects, so the dashboard can open the last one.

Rate limits (in Postgres, see [api/rate-limits](../api/rate-limits.md)): codes, 5 per email per 15 minutes and 20 per IP per 15 minutes; verifies, 10 per email per 15 minutes and 30 per IP. Over them, `429 rate_limited` with `Retry-After`. Codes live 10 minutes. Expired and consumed codes are pruned hourly.

### Sessions

- The cookie is `__Host-lewbox_session`: 32 random bytes, base64url; `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain`. Only its SHA-256 is stored.
- A session lasts 30 days from its last use (sliding), at most 90 days from when a code was last entered. `last_seen_at` is written at most once a minute per session.
- Signing out revokes the session. The user can list their sessions (`GET /v0/me/sessions`, with device, IP, last seen) and revoke any, or all but this one.
- A disabled user's sessions stop working at once ([instance-administration](instance-administration.md#users)).
- Sessions are looked up per request with one indexed query; an in-process cache of a few seconds is allowed, invalidated by `LISTEN/NOTIFY` on revoke, so a revoked session stops working within a second on every replica.

### The dashboard and CSRF

The dashboard is a React app served by the same binary on the same origin ([dashboard/stack-and-structure](../dashboard/stack-and-structure.md)). It calls `/v0` with the cookie. Since the cookie is `SameSite=Lax`, cross-site `POST`s don't carry it in modern browsers, but we don't rely on that alone:

- Every state-changing request with a session cookie must carry `Lewbox-CSRF: 1` (any custom header forces a CORS preflight, which the API refuses for other origins when a cookie is involved), **and** its `Origin` (or `Referer`) must be the instance's own URL. Otherwise `403 csrf_failed`.
- Requests with a bearer key never use cookies, so they skip this and keep the open CORS the API needs for browsers using sandbox keys ([api/authentication](../api/authentication.md)).
- A request may not carry both a session cookie and a bearer key: the key wins and the cookie is ignored, so a page can't ride a user's session with a stolen key or vice versa.

### Re-authentication

Some actions need a code entered recently ("sudo mode"): deleting a project, transferring ownership, creating admin keys, registering or removing a GitHub App, changing email, instance admin actions. The check is `user_sessions.authenticated_at` within 15 minutes; otherwise the API answers `403 reauthentication_required`, and the dashboard asks for a new code (`POST /v0/auth/codes` with `purpose: reauthenticate`, then `POST /v0/auth/reauthentications { code }`), then retries.

### The user's own account

| Method | Path | What |
|---|---|---|
| `GET` | `/v0/me` | The user: `id`, `email`, `name`, `instance_admin`, `last_project` |
| `PATCH` | `/v0/me` | `name`, `last_project` |
| `POST` | `/v0/me/email-changes` | `{ email }`: emails a code to the new address |
| `POST` | `/v0/me/email-changes/confirmations` | `{ email, code }`: changes it; emails the old address that it changed |
| `GET` | `/v0/me/sessions` | Their sessions |
| `DELETE` | `/v0/me/sessions/{uss}` | Revoke one; `DELETE /v0/me/sessions?except=current` revokes the rest |
| `GET` | `/v0/me/invitations` | Pending invitations to their email ([members-and-roles](members-and-roles.md#invitations)) |
| `DELETE` | `/v0/me` | Delete the account: refused with `409 last_owner` while they're the only owner of any project |

The user object:

```json
{ "id": "usr_8Hk2…", "email": "sam@acme.dev", "name": "Sam", "instance_admin": false,
  "last_project": { "id": "prj_…", "slug": "acme" }, "created_at": "…" }
```

### Email

Lewbox sends mail through SMTP ([foundation/binary-and-commands](../foundation/binary-and-commands.md)). Sign-in codes are the one mail the instance can't work without, so:

- `lewbox serve` refuses to start in production without `LEWBOX_EMAIL_SMTP_URL`, unless `LEWBOX_EMAIL_LOG=true` is set explicitly, in which case codes are written to the log at `warn`, as legacy did, and the sign-in page says so to instance admins.
- `lewbox users code <email>` prints a fresh code from the command line, for an operator locked out of their own instance (it needs database access, so it's no weaker than the database itself).
- Templates are plain text with a minimal HTML version, with the instance's name; setup-page mail (none yet) would use the project's branding.

### Later

- **OAuth** with GitHub and Google: `user_identities` with `provider` and `subject`, linked to the user whose verified email matches, or on an explicit "link account". Useful because Lewbox users usually have GitHub.
- **Passkeys** (WebAuthn) as a faster second way in, and as a second factor.
- **OIDC SSO per project**: a project's owner sets an issuer; members of that project must sign in through it (and are created on first sign-in if the project allows). SCIM later still.
- **Two-factor** with passkeys or TOTP, required per project.

## API errors

| Status | Code | When |
|---|---|---|
| `401` | `unauthenticated` | No session cookie and no key |
| `401` | `session_expired` | The session expired or was revoked |
| `403` | `csrf_failed` | A cookie request without `Lewbox-CSRF` or from another origin |
| `403` | `reauthentication_required` | A sensitive action without a recent code |
| `403` | `user_disabled` | The user was disabled by an instance admin |
| `422` | `invalid_code` | Wrong, expired or used code. One code for all three, so it says nothing about which |
| `429` | `rate_limited` | Too many codes or attempts |

## Security

- Codes are short; the guard is attempts (5), lifetime (10 minutes), one live code per email, and rate limits by email and IP. At 6 digits and 5 attempts a guess lands one time in 200,000 per code.
- No enumeration: asking for a code answers the same for every email; a wrong code says the same thing for unknown emails.
- The HMAC key for codes is derived from the instance's secret keyring ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)), so a database dump alone can't test codes offline… though they expire in 10 minutes anyway.
- Session tokens are hashed at rest; the cookie is `__Host-` prefixed, so subdomains (port URLs live on node domains, but a custom domain might share a parent) can't set it.
- Every sign-in, failed verify past the third, and session revoke is logged with IP; sign-ins from a new device email the user ("New sign-in to Lewbox from Firefox on Linux") — Later, behind a setting.

## Observability

- Metrics: `lewbox_sign_in_codes_sent_total`, `lewbox_sign_ins_total{result}` (`success`, `invalid_code`, `rate_limited`, `not_allowed`), `lewbox_user_sessions_active`.
- Logs: sign-ins and failures with the email hashed, IP and user agent.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_EMAIL_SMTP_URL` | | `smtp://user:pass@host:587`, `smtps://…` |
| `LEWBOX_EMAIL_FROM` | `Lewbox <no-reply@{host}>` | |
| `LEWBOX_EMAIL_LOG` | `false` | Write sign-in codes to the log instead of sending them (development, or a production instance without mail, deliberately) |
| `LEWBOX_USER_SESSION_IDLE` | `720h` | Sliding lifetime |
| `LEWBOX_USER_SESSION_MAX` | `2160h` | Absolute lifetime since a code |
| `LEWBOX_LOGIN_CODE_TTL` | `10m` | |
| `LEWBOX_REAUTH_WINDOW` | `15m` | |

## Testing

- Unit: code generation, HMAC check in constant time, attempts, expiry, one live code per email.
- API: the sign-in flow end to end with a fake SMTP that captures mail; policy cases (`open`, `invite_only`, `allowlist`); enumeration (same answer and similar timing for known and unknown emails); CSRF (missing header, wrong origin, bearer plus cookie); session expiry and revoke across two server instances sharing a database (NOTIFY invalidation).
- Dashboard: Playwright test signing in with a captured code.

## Build plan

Package `internal/users` (users, codes, sessions, email changes), `internal/auth` (session middleware, CSRF, re-auth), `internal/mail` (SMTP sender, templates, log sender).

1. Migrations and `citext`.
2. Mail sender with the `log` fallback and the `lewbox users code` command.
3. Codes: ask, verify, rate limits, pruning.
4. Sessions: cookie, middleware, sliding expiry, NOTIFY invalidation, list and revoke.
5. CSRF middleware and the bearer-or-cookie rule.
6. `/v0/me` and email change.
7. Re-authentication.

## Open questions

1. **Magic links as well as codes?** Codes work across devices (sign in on a laptop from a phone's mail); links are one click. The plan sends both, with the link only pre-filling. Is that enough, or should a link sign in directly (risking mail scanners)?
2. **Name at sign-up.** Ask for a name on first sign-in, or derive it from the email until set?
3. **OAuth first?** GitHub sign-in would be the most natural for Lewbox's users. Pull it into the alpha?
4. **Sessions for the CLI.** Should `lewbox login` open a browser and mint an admin key or a personal token, or should the CLI only ever use keys ([sdks-and-cli](../sdks-and-cli.md))?
