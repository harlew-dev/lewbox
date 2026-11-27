---
name: auth
description: Account flows designed so nothing an outsider can observe (status, body, headers, cookies, timing, emails, rate limits, CAPTCHA, DB writes, events, UI copy, redirects) reveals whether an account, address or hidden resource exists, or its state, until they prove ownership. Covers sign-up with email verification, sign-in with dummy hash and MFA, suspended/locked/unverified states, the account lock, forgot/change password, change email, invitations, email blocks, account and tenant deletion, API key failures, the hidden admin door, tenant 404s, sessions, TOTP and fresh proof, and how to test it all byte for byte. Use when building or changing any sign-in, sign-up, password, email, session, MFA, invitation, deletion or credential flow, or any endpoint that takes an email, ID, token or key from someone not yet proven.
---

# Authentication

**The principle:** nothing an outsider can observe ever reveals whether an account, address or hidden resource exists, or what state it is in, until they have proven ownership: the password, the password plus a second factor, a code or link from the inbox, or a live session. "Observe" means **status, body, headers, cookies, timing, emails sent, rate-limit counters and `Retry-After`, CAPTCHA appearance, DB writes, events, UI copy and redirects.** Every channel is a separate oracle; a flow is only as private as its leakiest channel.

Why: a list of which emails have accounts feeds credential stuffing and targeted phishing; a list of account states (no MFA, suspended, locked) tells attackers where to aim; for some products having an account at all is sensitive; and "this address is blocked" tells a harasser a person reads it.

Related skills: `security` (token/code storage, CSRF middleware, headers, authorisation, API key format), `ops` (rate-limit and CAPTCHA mechanics, admin console, mail delivery, erasure), `api` (status codes, error bodies), `testing` (race tests, fakes), `naming` (permission, event and limit names).

## When to use

- Building or changing sign-up, sign-in, MFA, password reset or change, email verification or change, sign-out, sessions.
- Adding any endpoint reachable signed out that takes an email, username, ID, token or key.
- Adding a tenant-scoped route, an invitation, a "block this address" link, a deletion, an API key route or a privileged console.
- Adding a rate limit, CAPTCHA challenge, email template or event on any of the above.
- Reviewing a PR that claims "the responses are identical".

## Reference files

| File | Read when |
| --- | --- |
| [references/enumeration-resistance.md](references/enumeration-resistance.md) | Designing any flow: the threat model, every channel, the four patterns, the techniques catalogue, accepted leaks |
| [references/sign-up-and-verification.md](references/sign-up-and-verification.md) | Sign-up, the code page, resend, verifying existing accounts, policy agreement |
| [references/sign-in-mfa-and-lock.md](references/sign-in-mfa-and-lock.md) | Password storage, sign-in, the MFA step, revealing states, the account lock, TOTP, recovery codes, fresh proof |
| [references/password-and-email-changes.md](references/password-and-email-changes.md) | Forgot password, signed-in password change, email change, security notices |
| [references/invitations-blocks-and-deletion.md](references/invitations-blocks-and-deletion.md) | Tenant invitations, email blocks, account and tenant deletion, erasure |
| [references/hidden-resources.md](references/hidden-resources.md) | Bearer/API key failures, the hidden admin door, tenant and child-resource 404s |
| [references/sessions.md](references/sessions.md) | Session storage, cookies, CSRF token storage, rotation and revocation |
| [references/testing-auth.md](references/testing-auth.md) | Writing the byte-for-byte, tables-touched and dummy-hash tests |
| [references/known-pitfalls.md](references/known-pitfalls.md) | Reviewing a design or PR: twelve weaknesses a previous implementation had, and their fixes |

## 1. The four patterns (name the one you're using)

1. **Same front door for every input.** Same status, body, headers, cookies, rows and tables touched. Branching happens in an **asynchronous listener** after the response (usually "send mail"), where only the inbox sees it.
2. **Fall through to the real unknown handler**, never imitate it. A wrong admin key is served by the `/nowhere` handler; a key on another tenant's path gets the unknown-route 404.
3. **Reveal only after proof.** States and conflicts are told to whoever proved ownership, when they can act on it.
4. **The truth goes to the audit trail.** `auth.login_attempted {result}` records `unknown_account`, `wrong_password`, `suspended`, `locked`; only operators read it.

- **MUST** validate from the body alone (422) **before** any lookup or per-email limit, so validation errors can't correlate with existence.
- **MUST** shape-check IDs and tokens before any query; malformed answers exactly like unknown.
- **NEVER** read `users`, the block list or any account state in a request whose answer must not depend on it. Record an intent event, answer, and let the listener look.
- **NEVER** add "helpful" branches: "this email is already registered", a different redirect, help text that changes per address.

## 2. Sign-up: no account until the inbox answers

- **MUST** run with email verification **on** in production; the server refuses to start otherwise. Verification-off sign-up necessarily answers 409 for a taken address (fold "blocked" into that same 409) and is for development only.
- Register: validate → `auth:per-ip` → `register:per-ip` → `register:per-email` → `register:global` → hash (always) → insert a **pending row** → set a `Strict` flow cookie → **202 `{verificationRequired, email}`** for every address.
- The listener decides: new address → a code; taken → "You already have an account" (no code; capped **in the listener**, e.g. 5/day); blocked → nothing (`Send` refuses).
- **ALWAYS** store an HMAC on every pending row; a taken or blocked address gets the **HMAC of a code nobody is sent**, so every guess fails exactly like a wrong code.
- **ALWAYS** find pending rows by the **cookie digest, never by email**: otherwise an owner can complete a stranger's simultaneous sign-up, with the stranger's password.
- The right code takes the **email-keyed lock**, rechecks the block list (401 "blocked", code holder only) and creates the user with `ON CONFLICT DO NOTHING` (409, code holder only).
- **NEVER** put requester-typed text (a name) in mail sent to an inbox the requester hasn't proven.

Detail: [sign-up-and-verification.md](references/sign-up-and-verification.md).

## 3. Sign-in, the MFA step, and states

- **MUST** answer unknown email, wrong password, malformed email and an over-long password with the **same 401** "Email or password is incorrect."
- **MUST** run one full password hash on every attempt: unknown addresses verify against a **dummy hash** made at startup with the same function and current parameters.
- **MUST** fetch only the hash before verifying, with one fixed-shape query. MFA, suspension, lock, verification and admin role are read **after** the password is right.
- **ALWAYS** record `auth.login_attempted` on every path with the same insert shape.
- **ALWAYS** rehash on a successful sign-in when the stored parameters are below current.
- MFA on: the right password creates **no session**, only a challenge (256-bit, digest stored, 5 min, 5 tries, `Strict` cookie) and `{mfaRequired: true}` (an accepted leak: the password was right). The code step creates the session.
- **States are revealed only after full proof** (password, plus code if MFA is on), in this order: suspended → 403 `{suspended: true}`; locked → 403 `{locked: true}` + owner notice; unverified (required mode) → a session that every non-allow-listed route answers 403 `{verificationRequired: true}`.
- `createSession` refuses suspended users under the user lock as a backstop; the session lookup excludes them too.

Detail: [sign-in-mfa-and-lock.md](references/sign-in-mfa-and-lock.md).

## 4. The account lock

- After too many failures for a typed address (e.g. 20 in 24 h), sign-in is locked. **The attacker keeps seeing the normal 401**, before, during and after.
- **MUST** count failures for **every** typed address with the same upsert into a table keyed by `HMAC(key, email)`, so known and unknown addresses do identical work and `users` isn't touched.
- **MUST** check the lock **after the password hash** (and after the code when MFA is on). Only then answer 403: "This account has been blocked due to repeated failed login attempts. Check your email to unblock."
- That successful-but-locked sign-in triggers, **from a listener**, an owner email: **[Unblock account]** (256-bit single-use link; unblocks on a button `POST`, never on GET) / **[Reset password]** (a real reset link), recommending two-factor authentication. **MUST** cap that notice **inside the listener** (e.g. 3/day), silently.
- A completed **password reset clears the lock** (inbox proof); so do the unblock link, a signed-in password change, an operator, and the lock's own expiry.
- **Persistent lockout is the residual risk:** anyone can relock an address daily. Bound it with a CAPTCHA challenge level on `login:per-email` (counted for every address alike) before failures count, a hard expiry per lock, one-click recovery, and an alert on mass locking. **NEVER** bound it by revealing the lock early or by "trusted device" cookies.

Detail and tests: [sign-in-mfa-and-lock.md](references/sign-in-mfa-and-lock.md#the-account-lock-brute-force-defence-that-is-not-an-oracle).

## 5. Password reset, password change, email change

- **Reset request:** 202, empty body, **no lookup**; the listener mints the token and sends nothing for unknown, suspended, pre-creation and pre-email-change requests. Links: 256-bit, digest stored, 30 min, single use, one live per user, built from the configured origin.
- **Reset completion:** lock user then link; delete every session; clear the lock; verify the email; **sign nobody in**; MFA stays on; notify the owner.
- **Change password:** the factor comes from the locked row (MFA on → a code **instead of** the password); "must differ from current" is checked **after** the factor passed and rolls back so the code isn't consumed; every session deleted, this browser rotated; notify the owner.
- **Change email:** fresh proof; no destination lookup in the request; a taken destination's owner gets an "Email change requested" notice and the requester's pending row holds an HMAC of an unsent code, so `state` advances identically; the right code then a taken **or** blocked address → one generic 409. Notify both inboxes (the old notice never shows the new address); revoke every session; bump the email generation.
- **ALWAYS** send fixed-text security notices for password changed/reset, MFA turned off, email changed (both inboxes) and a successful sign-in during a lock.

Detail: [password-and-email-changes.md](references/password-and-email-changes.md).

## 6. Invitations, email blocks, deletion

- **Inviting** answers an identical 201 for registered, unregistered and blocked addresses; only "already a member of this tenant" (which the inviter can already see) differs. The link page always offers both sign-in and sign-up. Sign-in and sign-up never look up invitations; joining needs the token **and** a matching signed-in address.
- **MUST** hide `invitation.sent` from tenant-visible logs, and write the real reason next to the rule: its absence reveals a blocked address.
- **Email blocks:** only accountless addresses can be blocked (security mail is never suppressed); every send checks the list and treats a refusal as done; only a holder of the inbox's link or code ever sees the word "blocked".
- **ALWAYS** serialise sign-up completion, block and email-change completion with **one email-keyed lock**.
- **Deletion** of an account or tenant needs fresh proof chosen by the locked row: **a code when MFA is on**, the password otherwise; tenants also need the typed name, rechecked under the lock. A non-member gets the same 404 as a missing tenant.
- **Erasure is time-bounded per address owner**, the current address included.

Detail: [invitations-blocks-and-deletion.md](references/invitations-blocks-and-deletion.md).

## 7. Hidden resources

- **Bearer failures:** one `401 {"error": "Invalid API credential."}` + `WWW-Authenticate: Bearer` for missing, malformed, unknown, wrong, revoked, expired and ineligible-owner keys, with a dummy constant-time compare when there's nothing to compare. A valid key on another tenant's path → the unknown-route 404, without querying that tenant. **Scope before permission** (404 before 403).
- **Hidden admin door:** a wrong, near-miss or missing key, admin disabled, or the right key from outside the allowlist → **exactly** `/nowhere`, served by the same handler. A catch-all `/api/admin/` route behind the gate prevents 405 leaks. Constant-time comparison against all configured keys, no early exit. No admin code in the public bundle.
- **Tenant 404s:** look the tenant up **through the caller's membership in one query**; non-member, missing and malformed are one 404; child IDs are always `WHERE public_id = ? AND tenant_id = ?`.

Detail: [hidden-resources.md](references/hidden-resources.md).

## 8. Sessions

- **MUST** be opaque server-side sessions: 32 random bytes, only the SHA-256 digest stored; `__Host-` cookie, `HttpOnly`, `Secure`, `Path=/`, `SameSite=Lax` (flow and admin cookies `Strict`); 30 min idle, 24 h absolute; at most ~5 per user under the user lock.
- **NEVER** store the CSRF token in plaintext: derive it as `HMAC(csrfKey, rawSessionToken)` (preferred) or store a digest.
- **Rotate** on sign-in, MFA sign-in, sign-up completion, password change and email change. **Revoke every other session** on password change/reset, email change and MFA enable; all of them on suspension and deletion.
- Recovery routes (logout, sign out others, change password, MFA off, new recovery codes) skip the per-user write budget; "sign out others" is limited **per session**.

Detail: [sessions.md](references/sessions.md).

## 9. MFA (TOTP) and fresh proof

- TOTP RFC 6238; replay blocked by `last_used_step` under the row lock. The secret is **sealed with AES-256-GCM** under an HKDF subkey of the app key, with the owner's internal ID as associated data.
- 10 recovery codes (~50 bits each), **HMAC-stored**, single use, shown once.
- Setup needs the password; enabling revokes other sessions; disabling and new recovery codes need password **and** code; reset and password change leave MFA on.
- **Fresh proof is chosen by the server from the locked account row**, never by the request: password when MFA is off, a code when it's on, for every sensitive action (password, email, deletion, export, API keys). Wrong proof is **422 on the field**, never 401.

## 10. Rate limits and CAPTCHA in auth flows

- **MUST** count every per-email limit for **every typed address alike, before any lookup**, keyed by **`HMAC(rateLimitKey, subject)`**, never a plain SHA-256.
- **MUST** use **separate counters per flow**: `login:per-email` (anonymous sign-in), `register:per-email` (sign-up), `password-check:per-user` and `mfa:per-account` (by user ID), and so on. No signed-in action moves an anonymous-visible counter; sign-up spam can't lock anyone out of sign-in.
- **Prefer** aligned windows for anonymous per-email limits so `Retry-After` doesn't reveal when the first hit happened; keep 429 text generic.
- Account-specific caps (account-exists notices, lock notices) live **in the listener**, never on the request.
- CAPTCHA challenges depend only on counts that treat every address alike. **NEVER** let the CAPTCHA script load on a page whose URL, including `?next=`, holds a token: server-side CSP omission plus a client guard plus a test.

Mechanics: `ops` (`rate-limits-and-quotas.md`, `captcha-and-bot-defence.md`).

## 11. Races and errors

- **MUST** map every store error a race can produce to its documented status (a used link → 422, a claimed address → 409, suspended mid-flight → 401). An unmapped error reaching 500 is a bug.
- A refused proof or limit leaves earlier state usable (a pending change, an unused code).

## 12. Testing

- **ALWAYS** assert "same answer" with `sameAnswer`: status, **body bytes**, every header except the request ID, with explicit substitution of values that legitimately differ. **NEVER** key sets, parsed JSON or status only.
- **ALWAYS** assert the **tables touched** via a query hook instead of timing, and that `users` isn't among them where the design says so.
- **ALWAYS** test the dummy-hash path with a counting verifier.
- One comparison test per flow, named after the property. List: [testing-auth.md](references/testing-auth.md).

## Pre-merge checklist

- [ ] For every unproven input this change accepts, I listed what each caller sees, and the paths converge on status, body, headers, cookies, rows and tables.
- [ ] No request-time read of `users`, blocks or account state where the answer must not depend on it; the branching is in a listener.
- [ ] Body-only validation runs before lookups and per-email limits; malformed IDs and tokens answer as unknown with no query.
- [ ] Sign-in: one 401 message, dummy hash on current parameters, hash-only lookup before verify, rehash on success, attempt event on every path.
- [ ] States (suspended, locked, unverified, blocked, taken) are revealed only after the proof that lets the owner act.
- [ ] Lock: failures counted for every address alike; checked after the hash; notice from a capped listener; reset clears it.
- [ ] Per-email limits are HMAC-keyed, counted for every address, and not shared across flows or with signed-in actions.
- [ ] Account-specific caps are in listeners. Mail has no requester-typed text.
- [ ] Sensitive actions (including deletion) take fresh proof chosen from the locked row; MFA on means a code.
- [ ] Sessions rotate and revoke per the table; CSRF token not stored in plaintext.
- [ ] Notices sent for password change/reset, MFA off, email change, sign-in during a lock.
- [ ] No token page (path or `?next=`) can load the CAPTCHA script.
- [ ] Every race maps to its documented status, never 500.
- [ ] Byte-for-byte, tables-touched and dummy-hash tests added for the flow; accepted leaks documented.
