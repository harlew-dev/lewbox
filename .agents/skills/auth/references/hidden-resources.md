# Hidden resources: API key failures, the hidden admin door, tenant 404s

Read this when adding bearer-authenticated routes, a privileged console, or any tenant-scoped route. Key format and scopes are in `security` (`api-keys.md`); admin sessions, roles and step-up in `ops` (`admin-and-privileged-actions.md`). This file is only about what an outsider can learn.

## API key / bearer authentication failures

| Bearer presented | Answer |
| --- | --- |
| None, malformed, wrong length, in a custom header, cookie only, two `Authorization` headers | `401 {"error": "Invalid API credential."}` + `WWW-Authenticate: Bearer`; a **dummy constant-time compare** runs; no query |
| Well-formed, unknown key ID | the same 401; one query by public ID, then a constant-time compare against an empty digest |
| Known ID, wrong secret, wrong kind, revoked (digest cleared), expired, unknown scope | the same 401 |
| Personal key whose owner is suspended, removed from the tenant, or must verify their email | the same 401 (the key isn't revoked by the last case) |
| Valid key, **another tenant's** path ID (existing or not, malformed or not) | **404**, byte-identical to an unknown route; the other tenant is **not queried** (compare the key's own tenant ID with the path) |
| Valid key, resource missing, malformed, in another tenant, or outside the key's selection | the same 404, **scope checked before permission** |
| Valid key, resource in scope, action not granted | 403 "This credential does not permit that action." (one text for every 403 path, including a mid-flight demotion caught by the transactional recheck) |
| Unknown `/api/v1/...` path, wrong method | JSON 404 |

- Look up by the public key ID embedded in the credential, then constant-time compare SHA-256 of the **whole** credential. Never `WHERE digest = $1` with an early return that skips the compare for unknown IDs.
- Apply the key's selection **before** search, counts and pagination, so totals don't leak hidden rows.
- Failed authentication has its own per-IP limit (only failures count); anonymous in-memory counters are capped, and saturation answers a generic 503.
- Key management in the browser: showing another tenant's key, someone else's personal key or an unknown ID → the same 404. An `owner=usr_…` filter is scoped to the tenant, so an outsider's user ID gives an empty list, not an error.
- **Tests:** compare the **bodies and headers** of every 401 class (unknown ID vs revoked vs ineligible owner vs malformed), a valid key against another tenant's path vs an unknown route, and out-of-scope vs missing project; status-only assertions miss body drift.

## The hidden admin door

A privileged console reachable only at `/admin/<route key>` (and API calls carrying the key in a header) is defence in depth on top of admin credentials, MFA and an IP allowlist. Its whole value is that nobody can tell it exists.

| Request | Answer |
| --- | --- |
| `/admin`, `/admin/`, `/admin/<wrong>`, `/admin/<wrong>/users`, near misses (case-changed, one character short) | **exactly** `GET /nowhere`: served by the same page handler, same status, body and headers except the request ID; no query |
| Admin disabled (no key configured) | the same, for every page and API route; nothing is compared |
| The right key from outside the IP allowlist | exactly a wrong key's 404; the refusal is recorded **off the request path** (bounded queue, at most once per IP per window) |
| `/api/admin/*` with no or wrong key header, admin off, outside the allowlist, **any unknown path or wrong method** | `404 {"error": "Not found."}`, the same as `/api/nowhere` |
| A previous (rotated) key on the sign-in page; or on a page with no cookie, a dead cookie, a session made under the other key, or a DB outage | exactly `/nowhere` (fail closed) |
| A previous key with a live session made under it | the console, with a "key is rotating" banner |
| Admin static assets without a valid pass, a tampered or other-key pass, an unknown file, `POST` | exactly `/nowhere`; the pass is checked **before** the file lookup |
| An IP block | the block's 403 for everything except the right key from an allowed IP; a wrong key and a right key from outside the allowlist both get the block answer |

Techniques:

- **Fall through, don't imitate.** On a wrong key, call the real unknown-page handler. A hand-written "fake 404" eventually differs by a header.
- **Catch-all behind the gate.** Register `/api/admin/` → `requireAdminKey(notFound)`. Without it, the router answers 405 for a known admin path with the wrong method, revealing the route.
- **Constant-time key matching:** compare SHA-256 of the given key against the digests of **both** the current and previous keys, every time, with a constant-time select and no early exit. An empty configured key never matches.
- **Secondary gates after the key, answered as a wrong key.** The allowlist is checked only once the key matched, and failing it is indistinguishable from a wrong key.
- **Look up a session only under the matched key's digest** (no `IN (current, previous)`), so a refused request never refreshes someone's idle timer.
- **The sign-in at the door** answers the same 401 for wrong password, unknown email (dummy hash), a non-admin with the right password, and an admin without MFA; the real reason goes to `admin.login_attempted.result`. The account lookup is the **same single query** for every address (fetch the hash and the admin role together, or fetch only the hash and read the role after the password passed); a second "is admin?" query only for known emails is a timing difference.
- **Admin status never appears in app responses.** (The one acceptable exception: an admin turning MFA off gets 409 "Admins must keep two-factor authentication on", which needs their own password and code.)
- **No privileged code in the public bundle:** a separate frontend build for the console, served only after the key; the public build fails on any admin import, and a post-build check greps the output for admin paths and header names.
- Don't rate-limit wrong admin keys in the app: no query is made, guessing 128+ bits is hopeless, and a limiter would itself be an observable.

**Test:** every admin route × {no key, wrong key, near miss, admin off, outside allowlist} is `sameAnswer` with `/nowhere` or `/api/nowhere`; a bad Origin with and without the key is `sameAnswer` too (not just "both 403").

## Tenant resource 404s

| Request | Answer |
| --- | --- |
| `/{tenant}` malformed | 404 "Tenant not found.", **no query** |
| `/{tenant}` missing, or the caller isn't a member | the same 404; **one** query looks the tenant up *through* the caller's membership |
| A member lacking the permission | 403, decided by role before the handler, independent of whether the target exists |
| Child IDs (`prj_`, `usr_`, `inv_`, `apk_`, `evt_`) malformed, missing, in another tenant (even one of yours), or hidden | the same 404 per resource type ("Project not found.") |
| Unknown API path | JSON 404, never the SPA shell |

- Every query takes the tenant's internal ID from the request context and the public ID from the path: `WHERE public_id = $1 AND tenant_id = $2`.
- Public IDs are long and random, but **access never relies on that**.
- **Test byte-for-byte**, not status-only: for every tenant route, a non-member, a missing tenant and a malformed ID produce the same status, body and headers (minus request ID); for every child resource, missing, malformed and another tenant's produce the same answer. Run it inside the role × route permission matrix so a new route is covered automatically.
