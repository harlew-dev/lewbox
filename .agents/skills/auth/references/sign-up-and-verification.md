# Sign-up and email verification

Read this when building sign-up, the "enter the code" page, resend, email verification of existing accounts, or policy agreement at sign-up. Rate-limit mechanics are in `ops`; code and token storage in `security` (`tokens-and-codes.md`).

## The design in one sentence

**No account exists until a code sent to the inbox is typed back, and every step before that looks the same for a new, taken, blocked or suspended address.** Sign-up is the last place an app would otherwise say "this email is registered"; verification closes it.

## Sign-up with verification on (the production design)

### Routes

| Route | Purpose | Challengeable |
| --- | --- | --- |
| `POST /api/auth/registrations` (or `/register`) | Start a sign-up: 202 + flow cookie | yes (`register`) |
| `POST /api/auth/registrations/check` | Which email is this browser's pending sign-up for (reload resume) | no |
| `POST /api/auth/registrations/resend` | New code for the same pending row | yes (`register`) |
| `POST /api/auth/registrations/verify {code}` | Complete it | no |

(Names follow `naming`; the shape matters more than the exact paths.)

### Request handler, in this order

```
1. decode (unknown fields → 400)
2. body-only validation → 422: email shape (trim, lowercase, ≤254 bytes),
   password (≥12 Unicode chars, ≤128 UTF-8 bytes), name, policy versions
3. auth:per-ip  (+ acquire a hashing slot; 503 when busy)
4. register:per-ip → register:per-email (typed address, every address alike) → register:global
5. hash the password (always)
6. INSERT pending_registrations (cookie_digest, email, password_hash, name, policy_versions,
   code_hash NULL, attempts 0, expires_at now()+15 min)
   + event auth.registration.requested {email, ip}           -- same transaction
7. Set-Cookie: __Host-app_registration=<256-bit token>; HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=900
8. 202 {"verificationRequired": true, "email": "<typed, normalised>"}
```

**NEVER** read `users` or the block list in steps 1–8. Not "carefully", not "just to log it". The lookup belongs to the listener.

### Listener (`auth.registration.requested` → send mail), after the response

```
row = find pending row by the event's ID; drop if gone or expired
code = 6 random digits
if users has the email:                       # taken
    store HMAC(codeKey, randomCodeNobodyGets) on the row
    if allow("account-exists:per-account", 5/day):     # hidden cap, in the listener
        send "You already have an account" (Sign in, Reset password links; no code)
    else: record rate_limit.reached once; send nothing
else:                                         # new (or blocked)
    store HMAC(codeKey, code) on the row
    send "verify-email" with the code          # Send() refuses blocked addresses → treat as done
record auth.registration.sent {existingAccount}   # operator-only event
```

- The verify-email template contains **no text the requester typed** (no name): otherwise anyone can make your server send their words to any inbox.
- The account-exists template greets the **stored** name of the account, never the typed one, and links to `/auth/login?notice=account-exists` and the forgot-password page.

### Code page and completion

- `check` (by cookie) → 200 `{email, expiresAt}` for every row. Lets a reload resume.
- `resend` → new code for the same row: `code_hash = NULL`, attempts reset, 15 more minutes, counted against `register:per-email` and `register:global` like a sign-up, a new request event. 202 for every address.
- `verify {code}`:

```
tx:
  row = lock pending row WHERE cookie_digest = sha256(cookie)      -- never by email
  if none or expired: 401 "This sign-up has expired. Start again."
  if code_hash IS NULL: 409 {pending: true}  (mail not issued yet; no attempt spent)
  candidate = isSixDigits(code) ? code : FIXED_NON_CODE             -- no fast path for junk
  if !constantTimeEqual(HMAC(codeKey, candidate), row.code_hash):
      attempts++; if attempts >= 5: delete row; 401 "Too many wrong codes. Start again."
      else 422 {field: "code"}
  lockEmail(row.email)                        -- the shared email-keyed lock
  if blocked(row.email): delete rows; 401 "This address has been blocked…"   (code holder only)
  INSERT users (... email_verified_at = now()) ON CONFLICT (email) DO NOTHING
  if conflict: 409 "You already have an account. Sign in instead."          (code holder only)
  delete every pending row for row.email
  event auth.registered {verifiedBy: "code"}
  create session (rotation) → 201
```

### What each caller sees

| Step | New address | Taken address | Blocked address | Suspended account's address |
| --- | --- | --- | --- | --- |
| 422 / 429 / 428 | same | same | same | same |
| `POST` register | 202 + cookie | **identical** | identical | identical |
| Inbox | the code | "You already have an account" (≤5/day) | nothing | "You already have an account" (harmless: inbox only) |
| `check` / `resend` | 200 / 202 | identical | identical | identical |
| Wrong code ×4, then 5th | 422 ×4, 401 | identical (HMAC of an unsent code) | identical | identical |
| Right code | 201 + session | unreachable (no code was sent), unless the account appeared after the code went out → 409 | unreachable, unless blocked after the code went out → 401 "blocked" | unreachable |

**Where the paths converge:** at the request (no lookup), at the row (every row gets a real-looking HMAC), at the code check (constant-time compare against that HMAC), and at the redirect (every address goes to the code page). They diverge only in the inbox.

### Why the cookie binding

If rows were found by email, an owner and a stranger signing up the same new address at once would share a row: the owner types the code from their inbox and creates the account **with the stranger's password**. Binding the code to the browser that started the sign-up (one email can have many pending rows) stops it and lets a reload resume. Test: `sign-up code belongs to its browser`.

### Codes

6 digits from the CSPRNG; 15 minutes; 5 wrong tries end the row; `register-code:per-email` (e.g. 10 checks/day across browsers and IPs, counted for the pending row's address) caps guessing to ~1 in 100,000 per day. Stored as HMAC-SHA256 under a key derived from the app key, never a plain hash (a million codes reverse instantly). Never in a URL, event or log; shown spaced in the email, never as a link, so it works on whichever device reads the mail.

## Sign-up with verification off (development only)

Same validation and limits (no `register:per-email`), hash, then insert the user directly. A taken **or blocked** address answers **409 "Unable to create this account. Try signing in or use a different email."** Blocked is folded into taken so a block is no more visible than an account.

This mode **necessarily leaks existence**: there is nothing to send the difference to. Therefore:

- **MUST** be on in production. The server **refuses to start** in production with verification off (and refuses verification on with mail off).
- Document the leak where the switch is documented.

## Existing, unverified accounts

Accounts created before verification existed (or while it was off) have `email_verified_at = NULL`.

- Verifying is a **signed-in** flow (nothing to hide from the owner): request a code (`email-verification:per-user` 3/h, plus a global cap), enter it (422 wrong, 410 ended or too many tries, 409 already verified, 200).
- "Verification required" mode: sign-in works normally (password, then code); then the session middleware answers **every** route 403 `{verificationRequired: true}` except an explicit allow-list (read session, sign out, sign out others, the verification routes, the email-change routes so someone who lost the old inbox can fix it). Mark the allow-list with a wrapper in the route table and test its exact size, so a new route is gated by default.
- So "unverified" is revealed only after full sign-in.
- A completed password reset marks the email verified (the link went only to that inbox).
- Invitation links **never** verify an address: possession of a link proves possession of the link, not the inbox (links get forwarded).

## Policy agreement at sign-up (if you have terms to accept)

- The client sends the exact versions it showed; the server accepts only the current ones (422 `field: "policies"` otherwise).
- The check reads **only the body and the server's versions**, never `users`, and runs with body validation, before every per-email limit, the hash and any lookup. The same 422 for every address.
- The pending row stores the versions; completion copies them to the user with the acceptance time = the sign-up's submission time.
- A sign-up started before policies were switched on ends at verify with "This sign-up has expired. Start again.", the same for every address.
- With policies off, the policy pages are unknown URLs (the same 404 as `/nowhere`).

## UI rules

- After `POST` register, **every** address navigates to the code page. Carry the email in navigation state, never the URL; the page calls `check` on reload.
- The code page's "Didn't receive the email?" help is **identical for everyone**: check spam; if you ever used "Block this address" in one of our emails no code can reach it, and that link's page unblocks it; or sign in / reset your password if you already have an account.
- The login page maps `?notice=account-exists` to a fixed sentence. Unknown notice keys show nothing. Nothing from the URL is ever rendered.

## Tests (see `testing-auth.md` for helpers)

- `sign-up looks the same for a taken email`: register, check, resend and 4 wrong codes then the 5th, for a new and a taken address: status, body (address substituted), all headers except the request ID, cookie count and attributes, **tables touched per request (never `users`)**; only the inbox differs.
- The same for a blocked address; and the verification-off 409 bodies identical for taken and blocked.
- Code belongs to its browser; code ends after 15 min and 5 tries; limits count unknown addresses.
- Listener: taken address gets the notice and no code; the cap drops the 6th silently and records the crossing once.
- Production refuses to boot with verification off.
