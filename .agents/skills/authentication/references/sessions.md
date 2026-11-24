# Sessions: storage, rotation and revocation

Read this when creating, checking, rotating or ending a session, or adding a flow cookie. The CSRF/Origin middleware itself is in `security-defaults` (`csrf-and-headers.md`).

## Storage

| Step | Rule | Why |
| --- | --- | --- |
| Create | 32 bytes from the CSPRNG, base64url (43 chars). Store only `sha256(token)` with user ID, `created_at`, `last_seen_at`, absolute `expires_at`, and the user's `email_version` | A DB dump holds no usable session |
| Return | Set the cookie; return `{user, csrfToken, expiresAt, signedInAt}` in the body | Your JS needs the CSRF token; other origins can't read it |
| Check | Reject a cookie of the wrong shape **before any query**; look up by digest; join the user; **exclude suspended users**; reject idle > 30 min or past absolute expiry (401); slide `last_seen_at` in the same statement | Idle bounds an abandoned laptop; absolute bounds a stolen cookie |
| Cap | At most N per user (e.g. 5), enforced when creating, in the transaction holding the user row lock; delete the oldest | Bounds leaked sessions and table growth |
| End | Logout deletes the row and clears the cookie | Clearing the cookie alone leaves a stolen copy valid |

- **Opaque server-side sessions only.** No JWTs for browser sessions: they can't be revoked, and every rule below revokes.
- **Cookie:** `__Host-<app>_session`, `HttpOnly`, `Secure`, `Path=/`, no `Domain`, `SameSite=Lax`. `__Host-` makes the browser refuse it unless Secure, host-only and Path=/, so a sibling subdomain can't plant or overwrite it. Local `http://` development can't use `Secure`/`__Host-`: derive both from config, and production refuses to start without HTTPS.
- **Lax for the session, Strict for flow cookies.** The main session is Lax so a link from an email opens the app signed in (top-level GET only; mutations are guarded by Origin + CSRF). Pending-flow cookies (sign-up, MFA challenge, admin sessions) are `Strict`, short-lived and cleared on every terminal outcome.
- **"Last seen"** for analytics: piggyback on the session check, write at most once an hour (skip if the row is locked rather than wait, to avoid deadlocks), never an event per request.
- **NEVER** put session or CSRF tokens in `localStorage`, `sessionStorage`, URLs, logs or analytics.

## The CSRF token is never stored in plaintext

The session token is digest-only; a plaintext CSRF token in the same row is an inconsistency an auditor will (rightly) flag. Two acceptable forms:

1. **Preferred: derive it, store nothing.** `csrf = base64url(HMAC(csrfKey, rawSessionToken))`, with `csrfKey = HKDF(appKey, "csrf v1")`. The server recomputes it from the cookie on every mutation and returns it from `GET /api/auth/me`, so a reloaded SPA (which lost it from memory) gets it back. It rotates with the session automatically. A DB dump yields nothing, because the raw session token isn't stored either.
2. **Random, digest stored.** Store `sha256(csrf)` and compare digests in constant time. The raw value can then be returned only in responses that create it, so `me` must re-issue (and store) a new one, which breaks other open tabs. Use form 1 unless you have a reason.

Either way: compare in constant time; check it before the request counts against any budget.

## Rotation and revocation

**Rotate** = delete the old row, create a new token and CSRF token, in one transaction under the user lock. Rotation kills session fixation: a token planted before sign-in is worthless after it.

| Event | This browser | Other sessions |
| --- | --- | --- |
| Sign-in, MFA sign-in, sign-up completion | new session | untouched (cap applies) |
| Password change (signed in) | new session | **all deleted**, admin sessions too |
| Password reset by link | **none** (go to sign-in) | **all deleted**, admin sessions too |
| Email change completed | new session | **all deleted**, admin sessions too |
| MFA enabled | kept | **all deleted** (they never passed it) |
| MFA reset by an operator | — | all deleted |
| Suspension | — | all deleted, with reset links and sign-in challenges, in one transaction under the user lock |
| Account deletion | cookie cleared | all deleted (cascade) |
| Logout | deleted | untouched |
| "Sign out other sessions" | kept | all deleted |

- **Suspended accounts have no valid session** by two independent checks: the lookup excludes them, and creation refuses them under the user lock. Every session middleware then refuses them, however a session came to exist.
- **Email generation:** sessions and sign-in challenges carry the `email_version` they were made under; creation rechecks it under the lock, so a sign-in that started before an email change can't complete after it.
- **"Sign out other sessions"** (`DELETE /api/auth/sessions`) is rate-limited **per session**, not per user: a stolen session exhausting its own allowance can't stop the owner's session from revoking it. Record an event only when something was revoked.
- **Recovery routes skip the per-user write budget** (logout, sign out others, change password, MFA off, new recovery codes) and keep their own auth limits, so a stolen session can't burn the budget to stop its owner recovering.
- **Admin sessions are separate:** their own table, `Strict` cookie, shorter lifetimes (e.g. 15 min idle, 8 h absolute), a lower cap (e.g. 2), started only by admin sign-in with a code, and bound to the admin route key they were made under. Never "a normal session with a flag". Details in `production-hardening`.
- API keys are separate credentials and survive password and email changes by design; say so in the security docs and the "your password was changed" notice ("API keys are not revoked; review them in settings").

## Client side

- Keep the session (user, CSRF token) in an in-memory session context loaded from `GET /api/auth/me`; the API client attaches the CSRF token to every mutation automatically.
- After a password change, swap in the rotated CSRF token from the response.
- Signed-out requests to signed-in pages redirect to sign-in **before any HTML is sent**; `?next=` goes through the allowlist (`security-defaults`).
- Signed-in visitors on guest pages (sign-in, sign-up) go to the app.
- On sign-out or account deletion: navigate away, then clear every client cache. Every cache key includes the user ID.
