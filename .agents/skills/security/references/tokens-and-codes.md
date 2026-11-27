# Tokens, links and short codes

Read with `SKILL.md` sections 1 and 10.

## Choosing how to store a secret

| Secret | Entropy | Store as | Why |
| --- | --- | --- | --- |
| Session token, link token, API key | >= 256 bits | `SHA-256(token)` | Unguessable input, so a fast hash is irreversible. Lookup by digest is an indexed equality |
| 6-digit code, recovery code | 20-50 bits | `HMAC-SHA256(derivedKey, code)` | 10^6 codes hash in milliseconds; only a server-held key makes the stored value useless |
| TOTP seed, webhook signing key, anything you must read back | any | AES-256-GCM, key from `HKDF(appKey, purpose)`, AAD = owning row's internal ID | Must be recoverable; AAD stops copying a ciphertext to another row |
| Password | low | Argon2id ([sessions-and-auth.md](sessions-and-auth.md)) | Low entropy, attacker has the salt; needs a deliberately slow hash |

- Derive a **separate key per purpose** (`HKDF(appKey, "email-code")`, `"recovery-code"`, `"totp"`), never reuse the app key directly.
- The app key is required in every environment; production refuses to start without one of adequate length.
- Never log, return or put in an event: the token, its digest, the code, the HMAC.

## Generating

```
token = base64url(csprng(32))          # 43 chars
code  = format("%06d", csprng_uniform(0, 1_000_000))   # uniform, no modulo bias
```

Use the platform's uniform integer helper (`crypto/rand.Int`, `secrets.randbelow`, `crypto.randomInt`, `random_int`); `random() % 10**6` is biased and not cryptographic.

## Emailed link tokens (reset, invitation, unsubscribe/block)

1. The request endpoint validates input and records an intent ("reset requested for X"), answering immediately (202). It does **not** look the account up.
2. A background job, a moment later: if the account exists and is eligible, generate the token, store its digest (one row per user/target, so a newer link replaces the older), send `APP_ORIGIN + "/auth/forgot-password/" + token`. Drop stale jobs older than the link lifetime.
3. The page at that path posts the token **in a JSON body** to a `check` endpoint, then to the action endpoint.
4. The action locks the row, checks it again (live, not expired, not used), acts, deletes the row.

| Link | Lifetime | Uses | Ends also when |
| --- | --- | --- | --- |
| Password reset | 30 min | 1 | A newer request, a reset, a signed-in password change, suspension |
| Invitation | 7 days | 1 | Revoked, accepted |
| Block/unsubscribe | long (e.g. 1 year) | reusable for block/unblock | - |

**Token in a URL: the one exception.** A single-use emailed link token may sit in its own page's **path**, and in `?next=` pointing back to that page (the allowlist matches the exact token shape). Mitigations, all required:

- Short lifetime and single use limit what browser history leaks.
- The page loads **nothing third-party** (no analytics, fonts, CAPTCHA scripts) and sends `Referrer-Policy: strict-origin-when-cross-origin` (API: `no-referrer`).
- The access log records the page label (`page /auth/*`), never the path.
- The token is **never** in an API URL (`POST /api/password-reset/check {token}`, not `GET /api/password-reset/<token>`).

**A link is not proof of inbox ownership.** Links get forwarded ("just use my invitation link"). An invitation link never marks an email verified, and accepting one requires the signed-in account's email to equal the invited email (403 otherwise, invitation left intact).

**Joining is deliberate.** Never auto-join tenants by looking up invitations by email at sign-up or sign-in. A crafted `?next=` to an invitation page must land on a "Join" button, never perform the join; only navigation state set by the page's own buttons may auto-accept.

## Short verification codes

Defaults for email verification and email change codes:

- 6 digits, CSPRNG, **15 minutes**, **5 wrong tries** then the pending row is deleted (401/410, start again), plus a per-email daily cap on checks from anywhere (`ops`). Guessing odds: about 1 in 100,000 per day.
- Stored as HMAC under a derived key. Compared in constant time inside a transaction that locks the row (otherwise parallel guesses bypass the try counter).
- **Bound to the browser that started the flow**: find the pending row by an HttpOnly `Strict` cookie holding its own 256-bit token, never by email. Otherwise two people signing up the same new email at once could complete each other's sign-up with their own password.
- Never in a URL, event or log. The email shows it spaced as text, not as a link, so it works on whichever device reads the mail.
- Resend issues a new code, resets the try count and extends the deadline, and counts against the same limits as the original request.
- Sign-up with verification creates **no account until the code is right**. The `users` table only gains verified accounts; pending rows hold the hashed password and expire.
- When the address is already taken, store the HMAC of a code **nobody is sent** and email a "you already have an account" notice instead. Every code then fails the taken-address row exactly as a wrong code fails a new one ([enumeration.md](enumeration.md)).

## Race conditions on unique claims

Anything that claims an email (sign-up completion, user creation, email change completion, blocking an address) takes the **same transaction-scoped advisory lock keyed by the normalised email** before checking "is it free?". Two concurrent flows otherwise both pass the check. A test races them.
