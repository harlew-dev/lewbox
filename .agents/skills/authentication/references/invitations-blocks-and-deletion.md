# Invitations, email blocks, and account/tenant deletion

Read this when building tenant invitations, an "email me never again" link, or deleting an account or a tenant. "Tenant" means your workspace/organisation/team noun.

## Invitations

### Inviting (`POST /api/tenants/{tenant}/invitations {email, role}`, owners/admins)

Order: mail off → 503; decode; email shape; role valid and assignable by the caller; permission (403, decided by role, not by target); `invitation:per-user` → `invitation:per-email` (typed, every address alike) → `invitation:global`; then the store, under the tenant lock: refuse an existing member of **this** tenant, a live invitation to the same address, and quotas.

| Invitee address | Inviter sees | Hidden reality |
| --- | --- | --- |
| No account | 201 `{invitation}`; later listed with `sentAt` | a link is emailed |
| Has an account | **identical** 201 | the same email; the page offers both sign-in and sign-up |
| Blocked | **identical** 201, listed with `sentAt` like any other | `sent_at` is set when the link is issued, before `Send`, which refuses; no `.sent` event |
| Already a member of this tenant | 422 `email` "Already a member" | only what the inviter's members list already shows |
| Live invitation exists | 422 "Already invited" | tenant-local fact |
| Per-address limit | 429 | counted for every address; message generic ("Too many invitations. Try again in N minutes.") |

- The store's only `users` read is the membership join for *this* tenant; the same queries run for every address.
- The listener mints the 256-bit token (never stored raw; a rerun replaces it), emails `APP_ORIGIN/auth/invitations/<token>`, and records `tenant.invitation.sent`.
- **`invitation.sent` MUST be hidden from the tenant's activity log**, and the reason written next to that rule must say *why*: "a missing `.sent` reveals a blocked address". If it only says "delivery plumbing", someone will helpfully show deliveries and leak every block.

### The link page, check and accept

- `POST /api/invitations/check {token}` (signed out, `auth:per-ip`): live → `{tenant: {name}, invitedBy: {name, email}, email, role, expiresAt}`; malformed, unknown, expired, revoked, accepted or tenant deleted → the **same** 422 `token` "This invitation has expired or was revoked." Shape check before any query.
- The page **always offers both** "Sign in to accept" and "Create an account", whether or not the invitee has an account. Login and register show the invitation callout whenever `?next=` is an invitation link, by calling `check` themselves.
- `POST /api/invitations/accept {token}` (session): dead → the same 422; right token but signed in as a different address → 403 "This invitation was sent to a different email address" (reveals nothing beyond what `check` showed the token holder). Lock order: account row (re-read, so a racing email change can't accept with a stale address) → tenant → invitation (the same order as revoke; the reverse deadlocks).
- **A suspension racing the accept** (detected under the lock) answers like a dead session: **401**, never 500. Every store error sentinel has a mapped status (see `known-pitfalls.md`).
- **Joining is always deliberate:** sign-in and sign-up never look up invitations. The page joins at once only with navigation state set by its own buttons; a crafted `?next=` shows an explicit **Join** step.
- The invitation email shows the inviter's address, so a misleading tenant name can be judged; fixed subject; every name escaped; never an `owner` invitation (invite as admin, then promote).
- With verification off, someone holding a leaked link who registers the invited address first can join. Bounded by single use, 7 days and revocation; verification on closes it.

### Who sees what

| Caller | Sees |
| --- | --- |
| Inviter | 201 for everyone; Sent / Expired; never "blocked", never "has an account" |
| Invitee (inbox) | tenant name, inviter's name and email, role |
| Someone else signed in with the link | 403 "different email" |
| Anyone without the link | nothing: accepting needs a 256-bit token stored only as a digest |

### Tests

- Inviting a registered, an unregistered and a blocked address: **byte-for-byte** 201 bodies and headers after substituting the fields that legitimately differ (the typed address, the new invitation's public ID, timestamps under a frozen clock). **Not** an equal set of JSON keys.
- The listing after the listener: identical rows (`sentAt` set) for all three.
- Dead-token classes produce one 422 body; accept under a racing suspension → 401.

## Email blocks ("Block this address")

Invitations (and any mail sent to an address someone else typed) carry a **block link**: `/auth/email-blocks/<token>` (256 bits, digest stored, long-lived, e.g. a year; the same link unblocks). Its page checks the link, then blocks (`POST /api/email-blocks {token}`) or unblocks (`DELETE`).

| Caller | Sees |
| --- | --- |
| Link holder, address without an account | `check` → `{email, registered: false, blocked}`; block/unblock 204, idempotent |
| Link holder, address **with** an account | `registered: true`; the page explains it can't be blocked while an account uses it (delete the account or change its email first); `POST` → 409 |
| Dead or malformed link | 422 `token` |
| Inviter of a blocked address | 201, listed with `sentAt`, exactly as any address |
| Sign-up, verification on | 202 and the code page, as any address; no code arrives; identical help text; verify with a code sent before the block → 401 "This address has been blocked…" (code holder only) |
| Sign-up, verification off | the taken address's 409 |
| Email change to a blocked address | on: 202, `state: "code"`, nothing sent; off: the generic 409 |
| Operator | can list and **unblock** (role + step-up + reason, audited); can never block |

Rules:

- **Every send checks the block list**, whatever the template, including queued mail. A refusal is treated as "done": no retry, no `.sent` event.
- **Only an address without an account can be blocked**, so security mail (resets, notices) is never suppressed.
- **The race** between "block needs no account" and "account creation needs no block" is serialised by **one email-keyed lock** taken by block, sign-up completion, direct user creation and email-change completion (e.g. a transaction-scoped advisory lock keyed by the first 8 bytes of SHA-256 of the normalised address, such as Postgres `pg_advisory_xact_lock`; or `SELECT … FOR UPDATE` on a lock row keyed by the address's HMAC). Test it with concurrent requests.
- **Why hide blocks at all:** "blocked" tells a prober that a human reads that inbox and has no account, and tells a harasser their invitation was seen.
- Use the URL form of `List-Unsubscribe` only (no one-click POST), so blocking always passes the confirmation page and the account check.

## Account deletion

```
DELETE /api/auth/me {currentPassword | code}       # JSON body; CSRF; recovery-style limits
```

- **Fresh proof chosen by the locked row:** password when MFA is off; **a code (authenticator or recovery) when MFA is on**. A password alone on an MFA account → 422 `code`. A stolen session plus a phished password must not destroy an MFA-protected account.
- In one transaction: lock every tenant the user owns in ID order; refuse with 409 `{error, tenants: [{id, name}]}` (only the caller's own tenants) while they're the only owner of any; take the tenant graph lock before cascading.
- Cascade: sessions, API keys, pending email changes, MFA rows, memberships. The cookie is cleared; other tabs get 401.
- `auth.account_deleted` carries only the public user ID.
- **Erasure is time-bounded per address owner:** redact the address (and the name beside it, and its pseudonymised form) only in events from the periods **this account owned it**: each former address up to its `released_at`, and the **current address from its `owned_from` onwards**. Otherwise deleting the current owner of a recycled address erases a previous owner's audit trail.
- The address can register again afterwards.

## Tenant deletion

```
DELETE /api/tenants/{tenant} {name, currentPassword | code}       # owners only
```

| Caller | Answer |
| --- | --- |
| Owner, right name and proof | 204; memberships, projects, invitations, keys, idempotency records cascade; events keep `tenant_id = NULL` |
| Owner, wrong name | 422 `name` (checked first: members already know it) |
| Owner, wrong proof | 422 `currentPassword` or `code` (code when MFA is on) |
| Non-owner member | 403 |
| Non-member, missing or malformed tenant | the same 404 "Tenant not found." |
| Other members afterwards | 404, identical to "never existed"; invitation links → the dead-token 422 |

- Recheck the name under the tenant lock, so a stale tab can't delete after a rename.
- Audit each proof check (`auth.password_checked {step: "destroy_tenant"}` or the MFA equivalent).
