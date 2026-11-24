# Sign-in, account states, the account lock, and MFA

Read this when building sign-in, the second-factor step, suspension/lock/verification gates, TOTP setup, recovery codes, or any action that needs fresh proof.

## Password storage

```
hash   = argon2id(password, salt=random(16), memory=19 MiB, iterations=2, parallelism=1, length=32)
stored = "$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>"     # PHC string: parameters travel with the hash
verify = constant-time compare; needsRehash(stored) when its parameters are below current
```

- Argon2id at least at the OWASP minimum above. Fallback only if unavailable: scrypt, or bcrypt cost ≥ 10 (bcrypt silently truncates at 72 bytes).
- **Length:** new passwords (sign-up, change, reset) ≥ 12 Unicode characters (code points); every password ≤ 128 UTF-8 bytes, checked **before** hashing (megabyte passwords burn CPU and memory). Login checks only the maximum, so a rule change never locks anyone out. No composition rules. A breached-password check, if any, runs only when a password is set, never at login.
- **Bound hashing concurrency** per process (e.g. 4 slots; 503 with `Retry-After` when full, the same for everyone). 1,000 concurrent Argon2id hashes at 19 MiB is 19 GB.
- **NEVER** log, store, echo or put in an event a plaintext password, even a wrong one.

## Sign-in

### Handler, in this order

```
POST /api/auth/sessions {email, password}        # Challengeable("login")

1. decode; normalise email (trim, lowercase)
   malformed email or password > 128 bytes → the same 401 as a wrong password (no 422 here)
2. auth:per-ip (+ hashing slot; 503 when busy)
3. login:per-email   (typed address, every address alike; its own counter, shared with no other flow)
4. row  = SELECT id, password_hash FROM users WHERE email = $1     -- one fixed-shape index lookup
   hash = row ? row.password_hash : DUMMY_HASH                     -- DUMMY_HASH made at boot with current params
5. ok   = verifyPassword(hash, password) && row != null           -- always one full hash
6. if !ok:
       record auth.login_attempted {result: row ? "wrong_password" : "unknown_account"}   -- same insert shape
       count a failure for the lock (same upsert for every address, see below)
       → 401 {"error": "Email or password is incorrect."}
7. --- proof boundary: everything below runs only for the right password ---
   if needsRehash(row.password_hash): store a new hash with current params
   state = SELECT mfa_enabled_at, suspended_at, email_verified_at, email_version FROM users ...
   if MFA on:  create challenge; Set-Cookie app_mfa (Strict, 5 min); 200 {"mfaRequired": true}
               record result "mfa_required"   -- nothing else is checked yet
   else:       finishSignIn(user)              -- see "Revealing state"
```

- **The dummy hash** is computed once at startup by the same function and parameters as real hashes (`hashPassword("placeholder-not-an-account")`). Without it, an unknown email answers in ~1 ms and a known one in ~50 ms.
- **Nothing about the account is read before step 5** except the hash. MFA, suspension, lock, verification and admin role are all read after the password is right. A query whose shape depends on whether the row exists (subqueries that run only for found rows, a second query for "known" emails) is a timing difference, however small.
- **Record the attempt on every path** with the same insert (nullable user ID), so the write costs the same; the real reason goes in `result`, visible only to operators.
- **Rehash on login** when the stored parameters are weaker than current ones (`needsRehash` reads the PHC string). Otherwise old hashes verify at their old, cheaper cost while the dummy runs at the new cost, and "fast 401" means "old account". Also: raising parameters → rehash on next success, and optionally force-expire hashes that never upgrade.

### MFA step

```
POST /api/auth/sessions/mfa {code}              # not Challengeable: the password already proved something

challenge = find by sha256(mfa cookie), purpose "login", not expired, same email_version
  missing / expired → 401 "Enter your password again."
mfa:per-account (user ID, from any IP; 5 / 15 min) + auth:per-ip block level
wrong code → 422 {field: "code"}; 5th wrong → delete challenge, 401 "Too many wrong codes. Enter your password again."
right code → delete challenge; finishSignIn(user)
```

The cookie is `__Host-app_mfa`, HttpOnly, Secure, SameSite=Strict, 5 minutes, cleared on every terminal outcome. Challenges carry a `purpose` (`login`, `admin`, …) so one flow's challenge can't be spent in another.

### Revealing state: `finishSignIn`

Runs only after full proof (password, plus code when MFA is on), under the user row lock:

```
finishSignIn(user):
  lock user row; recheck email_version (an email change in between → 401 "Sign in again")
  if suspended:  record result "suspended"; 403 {"error": "This account has been suspended. Contact support.", "suspended": true}
  if locked(user.email):  record result "locked" (the listener mails the owner)
                 403 {"error": "This account has been blocked due to repeated failed login attempts. Check your email to unblock.", "locked": true}
  create session (rotation, cap), clear the lock's failure count
  record auth.logged_in → 200 {user, csrfToken, ...}
  if unverified and verification is required: the session exists, but every route except the allow-list answers 403 {verificationRequired: true}
```

`createSession` also refuses suspended users under the lock, as a backstop for any path that forgets.

### What each caller sees

| Case | Password step | Code step |
| --- | --- | --- |
| Unknown email | 401 (dummy hash) | n/a |
| Known, wrong password, any state | 401 **identical** | n/a |
| Right password, MFA off, active | 200 session | n/a |
| Right password, MFA off, suspended | 403 suspended, no session | n/a |
| Right password, MFA off, locked | 403 locked, no session, owner mailed | n/a |
| Right password, MFA off, unverified (required mode) | 200 session, then 403 `verificationRequired` everywhere else | n/a |
| Right password, MFA on, **any state** | 200 `{mfaRequired}` (identical for active, suspended, locked) | wrong: 422, identical for every state |
| Right code, active / suspended / locked / unverified | | session / 403 suspended / 403 locked + mail / session + gate |

**Where the paths converge:** unknown and wrong passwords at the dummy hash and the single message; every account state at the password step when MFA is on; every state at "wrong code". The web client treats a 403 at the code step like a 401: back to the password step, showing the server's message.

## The account lock (brute-force defence that is not an oracle)

### Goal and why it's shaped this way

After too many failed sign-ins for an address, sign-in is locked until the owner unlocks it from their inbox. The obvious version ("Account locked" after 10 wrong passwords) is an oracle: lock today, not locked tomorrow → the account exists and is under attack; and anyone can probe state. So:

- **The attacker never sees the lock.** Wrong passwords keep getting the ordinary 401, before and during a lock.
- **The lock is revealed only after a correct password (and code, if MFA is on)**, with:
  > This account has been blocked due to repeated failed login attempts. Check your email to unblock.
- **That successful-but-locked sign-in emails the owner**: a sign-in succeeded but the account is blocked; if it was you, **[Unblock account]**; if not, **[Reset password]**, and we recommend turning on two-factor authentication (omit the recommendation when it's already on).

### Data and counting

Key the lock by the **typed address**, so counting and locking do identical work for every address and never touch `users` before the hash:

```sql
CREATE TABLE login_locks (
  email_key       bytea PRIMARY KEY,      -- HMAC(lockKey, normalised email), never the raw address
  failures        int NOT NULL,
  window_ends_at  timestamptz NOT NULL,   -- failures count within e.g. 24 h, aligned windows
  locked_at       timestamptz NULL,
  lock_expires_at timestamptz NULL        -- hard bound on one lock (see "persistent lockout")
);
```

- **Every failed sign-in** (unknown email or wrong password) runs the same upsert: increment `failures` in the window; when it crosses the threshold (e.g. **20 in 24 h**), set `locked_at`, `lock_expires_at`, and record `auth.login.locked` (operator-only). Rows for unknown addresses are harmless and pruned when their window and lock end.
- The threshold is far above `login:per-email` friction for a human (who mistypes a few times) and well below what a guesser needs.
- A **successful full sign-in** while not locked clears `failures` (only the owner can do that).

### The check runs after the hash

`locked(email)` is read in `finishSignIn`, after the password (and code) are proven. Never before the hash, never as a reason to skip the hash, never as a different 401. The order is: suspended (operator business; no lock mail) → locked → session.

### The owner notice

- `finishSignIn` records `auth.login_attempted {result: "locked"}`; a listener on that result sends the notice. Don't send from the request: the response time must not include mail work.
- **Cap the notice inside the listener** (`lock-notice:per-account`, e.g. 3/day): above it, send nothing and record `rate_limit.reached` once. Someone with the password could otherwise make the server mail the owner endlessly. The cap is in the listener because a request-time 429 would itself reveal the lock.
- **[Unblock account]** is a link token: 256 bits, SHA-256 digest stored, single use, e.g. 24 h; the newest replaces older ones. Its page checks the token (`POST …/check {token}`), then unblocks on a button press (`POST …/unblock {token}`; never on GET, since mail scanners follow links). Unblocking clears `locked_at` and `failures`, records `auth.login.unlocked {via: "link"}`, and signs nobody in.
- **[Reset password]** is a real password-reset link (same table and rules as forgot-password: 30 min, single use, replaces any older link).
- The template has fixed text, the configured app origin, no requester-typed text, and the IP/time of the attempt only if you already show those in other security mail.
- Token pages (unblock, reset) are never challengeable and load no third-party script (see the CAPTCHA guard in `known-pitfalls.md`).

### What clears a lock

| Event | Clears the lock | Why |
| --- | --- | --- |
| Unblock link used | yes | Inbox proof |
| Password reset completed | **yes**, and `failures` | Inbox proof, and a new password makes old guesses worthless |
| Operator action (audited, with reason) | yes | Support path |
| `lock_expires_at` passes | yes | Bounds a forgotten lock (see below) |
| Signed-in password change | yes | Fresh proof plus a new password |
| Correct password alone | **no** | That's exactly the case the lock exists for |

### Persistent lockout: the risk and how to bound it

**The risk:** anyone who knows an address can send 20 wrong passwords a day and keep the account locked forever. The lock doesn't leak, but the owner now has to visit their inbox for every sign-in. That is a denial-of-service lever, and turning the previous 15-minute rate-limit lockout into a persistent one makes it worse.

Bound it, in this order:

1. **Raise the cost of each failure before the lock counts it.** Give `login:per-email` a **challenge level** (e.g. above 5 per window): further attempts for that typed address need a solved CAPTCHA. It is counted for every address alike, so the challenge reveals nothing about existence. Mass-locking then costs a solved CAPTCHA per failure.
2. **Bound each lock** (`lock_expires_at`, e.g. 24 h) so an abandoned attack ends on its own.
3. **One-click recovery for the owner:** the notice arrives the moment they sign in; unblock or reset is one click.
4. **Detect mass locking:** alert when `auth.login.locked` per hour is far above baseline (see `production-hardening`, monitoring). An operator can then raise the threshold or add an edge rule.
5. **Document it** as an accepted residual risk next to the lock's description, with the threshold values.

**NEVER** "fix" persistent lockout by revealing the lock early, by locking per IP only (useless against distributed guessing), or by exempting "trusted devices" via a long-lived cookie (that's a second credential with no proof behind it).

### Tests

- Wrong passwords before, at and after the threshold: byte-identical 401s, identical to an unknown address at the same counts.
- The failure upsert touches the same tables for known and unknown addresses (query hook); `users` is read once (the hash fetch) on both.
- Right password while locked (MFA off): 403 with the exact message, no session, one notice queued; MFA on: `{mfaRequired}` identical to an unlocked account, wrong codes identical, right code → 403 + notice.
- The notice cap: the 4th successful-but-locked sign-in in a day sends nothing and records the crossing once.
- Unblock link: check, unblock, single use, dead link → 422 `token`; GET never unblocks.
- Reset completion clears the lock; expiry clears it.

## MFA (TOTP)

### Defaults

| Part | Rule | Why |
| --- | --- | --- |
| Algorithm | RFC 6238: HMAC-SHA1, 6 digits, 30 s steps, ±1 step drift, constant-time compare | What authenticator apps implement |
| Replay | Store `last_used_step` under the TOTP row lock; reject any step ≤ it | A code works once, even within its 90 s validity |
| Secret | 160 random bits, **sealed with AES-256-GCM** under `HKDF(appKey, "totp v1")`, **associated data = the owner's internal user ID** | A DB dump alone is useless; a value copied to another row fails to decrypt |
| Setup | Needs the current password; returns `{secret, uri}` once (`no-store`); stored pending; unconfirmed setups expire in 15 min | A stolen session alone can't attach the attacker's authenticator |
| QR code | Drawn client-side from the URI, hidden behind "Reveal" | The secret never goes to a third-party QR service |
| Enable | First valid code confirms; 10 recovery codes returned once; **every other session revoked** | Those sessions never passed a second factor |
| Disable / new recovery codes | Password **and** a code | A stolen session plus one factor isn't enough |
| Password change / reset | MFA stays on | A reset link alone must never sign anyone in |
| Notices | Mail the owner when MFA is turned off (and when a recovery code is used, if you can) | The owner learns if someone else did it |
| Lost everything | Operator reset only, with out-of-band identity check, step-up, reason, audit | It bypasses the second factor |
| App key rotation | Changing the app key without re-sealing locks out every MFA account: document it | |

Be honest about the limit: TOTP doesn't stop a real-time phishing relay. Only passkeys/WebAuthn do; don't claim otherwise.

### Recovery codes

- 10 codes of 10 Crockford base32 characters (50 bits), shown `k7p2m-x9qfr`; case, hyphen and space insensitive.
- Stored as **HMAC-SHA256 under a derived key**, not a plain hash (50 bits is brute-forceable offline from a dump without the key).
- Single use (`used_at`); record `recoveryCodesLeft` on use, so the UI can warn.
- The code field accepts either; tell them apart by **shape**, not by trying both.
- A new set needs the password and a code (an old recovery code counts). All code checks count against `mfa:per-account`.

### Fresh proof (step-up), chosen by the server

Sensitive signed-in actions re-prove in **the same request and transaction**. The server reads which factor is required from the **locked account row**; the request body never decides.

| Action | MFA off | MFA on |
| --- | --- | --- |
| Change password | current password | a code **instead of** the password |
| Change email | current password | a code instead of the password |
| Delete account / delete tenant | current password | **a code** (the password alone is refused with 422 on `code`) |
| Export personal data, MFA setup, create/broaden API key | current password | a code (MFA setup: n/a) |
| Disable MFA, new recovery codes | n/a | password **and** code |
| Admin actions | n/a | a code from the last 5 minutes (step-up window) |

```
tx:
  user = SELECT ... FROM users WHERE id = $session_user FOR UPDATE
  if user.mfa_enabled_at: consumeCode(user, body.code)          → 422 {field: "code"}
  else:                   verifyPassword(user, body.currentPassword) → 422 {field: "currentPassword"}
  ... the action, in the same transaction; a refusal later rolls back so the code isn't consumed ...
```

- **Why a code instead of the password** when MFA is on: a stolen session plus a phished password must not be enough. (Session plus second factor without the password is: the same trade-off as GitHub's sudo mode.)
- **Wrong proof is 422 on the field, never 401**: clients treat 401 as "session expired" and sign the user out.
- A UX pre-check endpoint ("is my current password right?" so the form can advance) is fine, but the action checks again; nothing remembers that the first check passed.
- Signed-in password checks count against `password-check:per-user` (keyed by user ID), **not** the anonymous `login:per-email`; code checks against `mfa:per-account`. See `known-pitfalls.md` for why they must not share.
- An MFA change racing the action (detected under the lock) → 422 asking for the proof again, not 500.
