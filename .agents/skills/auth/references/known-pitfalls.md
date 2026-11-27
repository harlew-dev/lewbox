# Known pitfalls (found in a previous implementation)

A previous production app built most of this skill's design and was then audited flow by flow. These are the weaknesses the audit found. The design in this skill already includes every fix; this file explains what goes wrong when you skip one, so you recognise it in review.

Each entry: **what was wrong → why it matters → the fix → the test.**

## 1. Rate-limit keys were unsalted SHA-256 of the email or IP

- **Wrong:** counter keys were `<name>:sha256(email)`. The docs called this privacy-protective.
- **Why it matters:** emails and IPv4 addresses are dictionary-reversible. Anyone with a dump of the counters table (a backup, a support export) hashes a list of known emails and learns who tried to sign in, sign up or reset recently.
- **Fix:** `<name>:HMAC-SHA256(rateLimitKey, subject)`, with `rateLimitKey = HKDF(appKey, "rate-limits v1")`. The same applies to lock rows (`login_locks.email_key`) and any other table keyed by an address for counting. Rotating the app key resets counters, which is acceptable.
- **Test:** the stored key for a known email is not `sha256(email)` and changes with the app key.

## 2. Shared per-email counters across flows

- **Wrong:** one `auth:per-account` counter, keyed by the email, was incremented by anonymous sign-in, by sign-up (before `register:per-email`), **and** by signed-in password checks (deleting, exporting, MFA setup). The email-code counter was shared by anonymous sign-up verification and signed-in email verification. Windows were fixed and started at the first hit, and the 429 carried `Retry-After` = time left.
- **Why it matters:**
  - **Activity oracle:** a prober who exhausts the counter, or gets a 429 sooner than expected, or reads `Retry-After`, learns that *someone* hit that address recently, including the owner's own signed-in actions.
  - **Cross-flow lockout:** sign-up spam for an address (even when refused by `register:per-email`) kept the victim's **sign-in** counter exhausted.
- **Fix:**
  - **Separate counters per flow:** `login:per-email` (anonymous sign-in only), `register:per-email` (sign-up and resend only), `register-code:per-email` (anonymous code checks), `password-check:per-user` (signed-in password proofs, keyed by user ID), `mfa:per-account` (code checks, keyed by user ID; reachable anonymously only after a right password), `email-code:per-user` (signed-in verification and email-change codes). **No anonymous-visible counter is ever moved by a signed-in action**, and no flow moves another flow's counter.
  - **Aligned windows** for anonymous per-email limits (the window is `floor(now / length)`), so `Retry-After` is the same for every address at a given moment and says nothing about when the first hit happened.
  - **Generic 429 text** that doesn't name the subject ("Too many attempts. Try again in 12 minutes.", not "This email address has had too many invitations recently").
  - Accept and document the residue: an anonymous per-email 429 still tells a prober that someone else probed that address this window. It never says whether an account exists.
- **Test:** `signed-in checks don't move anonymous counters`; `sign-up spam can't lock sign-in`; two addresses whose counters started at different times give the same `Retry-After`.

## 3. Deleting an account or tenant accepted the password even with MFA on

- **Wrong:** deletion took the password only. Email change, password change and API-key creation already required the second factor.
- **Why it matters:** a stolen session plus a phished password (exactly what MFA exists to defeat) could destroy an MFA-protected account or every tenant it owns, irreversibly.
- **Fix:** deletion uses the same server-chosen fresh proof as every other sensitive action: password when MFA is off, a code (authenticator or recovery) when it's on. Audit the check.
- **Test:** for each sensitive action, an MFA account sending only the right password gets 422 on `code`, and nothing is deleted.

## 4. The CAPTCHA script could load on a page whose URL held a token

- **Wrong:** token pages (reset link, invitation link) were never challengeable, but sign-in and sign-up were, and they run at `/auth/login?next=/auth/invitations/<token>`. A challenged sign-in from an invitation link loaded the provider's third-party script with the live token in `location.href`. The client had no URL guard.
- **Why it matters:** a third-party script reads `location.href`, history state and the DOM. A live invitation or reset token handed to a third party is a credential leak.
- **Fix, three layers:**
  1. **Server:** token pages and their API routes are never challengeable.
  2. **Server, HTML:** when serving any page whose path **or query** (decoded, including `?next=`) contains a token-page path, omit the CAPTCHA provider's origins from that response's CSP. The browser then can't load the script even if the client is wrong.
  3. **Client guard:** before loading the script on a 428, check `mayLoadChallengeScript(location)`: refuse if the path or any query parameter (URL-decoded repeatedly until stable) matches a token-page pattern, or if the current SPA session ever showed a token page. On refusal, **do a full-page `location.replace()` to the clean page** (e.g. `/auth/login?notice=check-required`, a fixed sentence: "Complete the check, then open your link again"), so the token is gone from the document and its memory before the script loads. Never move the token into storage or history state "for later": the script can read those too.
- **Test:** unit-test the guard against token paths, `?next=` with encoded and double-encoded token paths, and clean URLs; an end-to-end test that a 428 on `/auth/login?next=/auth/invitations/<token>` makes **no request** to the provider's origin; an HTML test that such a page's CSP lacks the provider origins.

## 5. No rehash when hash parameters change

- **Wrong:** no rehash-on-login. After raising the Argon2 parameters, old hashes verify at their old cost while the dummy hash (made at startup with current parameters) runs at the new cost.
- **Why it matters:** "fast 401" then means "an old account exists"; and old hashes stay weaker forever.
- **Fix:** after a successful password check, `if needsRehash(stored): store hash(password)` with current parameters. Keep the dummy hash on current parameters. Optionally expire hashes that never upgrade.
- **Test:** a seeded old-parameter hash is replaced after one successful sign-in; the dummy hash's PHC parameters equal current ones.

## 6. The CSRF token was stored in plaintext

- **Wrong:** `sessions.csrf_token` held the raw token, while the session token was digest-only.
- **Why it matters:** low on its own (useless without the session cookie), but it breaks the "a dump yields nothing usable" rule and invites copying the pattern for something that matters.
- **Fix:** derive it as `HMAC(csrfKey, rawSessionToken)` and store nothing (preferred), or store a digest. See `sessions.md`.
- **Test:** no column holds a value equal to the CSRF token returned to the client.

## 7. "Identical" was asserted by key sets or status codes

- **Wrong:** sign-up and the admin door had byte-for-byte tests, but change email checked only status and `state`; invitations compared **the set of JSON keys**; tenant 404s and out-of-scope API-key 404s compared **status only**; a blocked invitation had no response comparison at all.
- **Why it matters:** a future edit (a different message, an extra field, a header) passes every test and leaks.
- **Fix:** `sameAnswer` (status, body bytes, every header except the request ID) with explicit substitution of legitimately different values, for every flow in the comparison table in `testing-auth.md`.
- **Test:** the table is the test list.

## 8. Erasure of the current address wasn't time-bounded

- **Wrong:** deleting an account redacted its **former** addresses only within the periods it owned them, but redacted its **current** address everywhere, including events from a previous owner of a recycled address.
- **Why it matters:** over-erasure destroys another account's audit trail (including that previous owner's own "email changed from X" record).
- **Fix:** record ownership periods (`owned_from`, `released_at`) for every address an account holds, current included; erasure redacts only events inside this account's periods.
- **Test:** A owns X, changes away; B takes X and deletes their account: A's events about X survive.

## 9. Races fell through to 500

- **Wrong:** accepting an invitation while the account was suspended mid-flight returned the store's "suspended" error, which the handler didn't map: 500.
- **Why it matters:** a 500 is an inconsistent, alertable answer and sometimes a distinguishable one.
- **Fix:** every store error sentinel has a mapped status in every handler that can reach it (suspended mid-flight → 401 like a dead session; a claimed address → 409; a used link → 422 `token`). An unmapped error reaching the 500 path is a bug: log it at error level.
- **Test:** race tests assert the loser's documented status (see `testing-auth.md`).

## 10. The per-email existence lookup did unequal work

- **Wrong:** the sign-in lookup selected MFA and suspension state through subqueries that ran only when a row existed; the admin door's password step ran an extra "is this an admin?" query only for known emails.
- **Why it matters:** sub-millisecond differences against a ~50 ms hash are hard to measure, but "hard" isn't "impossible" with enough samples, and the pattern spreads.
- **Fix:** before the password is verified, run **one fixed-shape query** that fetches only the hash (an index lookup whose plan doesn't depend on a match). Read every state (MFA, suspended, locked, admin role, verification) **after** the password passed, when the caller has proven ownership and differences no longer matter. Event inserts have the same shape on both paths.
- **Test:** `tablesDuring` for sign-in and the admin door's sign-in is identical for known and unknown addresses.

## 11. Sign-up with verification off leaks by design

- **Wrong (accepted, but dangerous as a default):** with verification off, a taken address answers 409. There's nowhere else to send the difference.
- **Why it matters:** if production ever runs in that mode, sign-up becomes an existence oracle.
- **Fix:** verification is **on** in production; the server refuses to start otherwise. Document the leak next to the switch.
- **Test:** production config with verification off fails to boot with a message naming the setting.

## 12. No notices for password change and MFA disable

- **Wrong:** "Your password was changed", "Your password was reset" and "Two-factor authentication was turned off" were planned but never built.
- **Why it matters:** these are exactly the changes an account hijacker makes first; without a notice, the owner learns only when they're locked out.
- **Fix:** fixed-text notices from listeners after commit, to the account's address, with a "reset your password" link and no acting links. Also for a used recovery code if feasible.
- **Test:** each action queues exactly one notice to the right address; the notice contains no token except a fresh reset-page link.

## Smaller items worth copying

- **Tell-tale events need their real reason written down.** Hiding `invitation.sent` from the tenant log was load-bearing for block privacy, but the code comment said "delivery plumbing".
- **Accepted leaks belong in the docs:** `{mfaRequired}`, verification-off 409, per-email 429 residue, previous-admin-key timing.
- **"Contact support" needs a support path.** Notices that say it should link to a real process (and the docs should say what support can and can't do).
