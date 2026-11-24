# Forgot password, change password, change email

Read this when building password reset, a signed-in password change, an email change, or the security notices they send.

## Forgot password and reset

### Request: 202, no lookup

```
POST /api/auth/password-resets {email}            # Challengeable("password_reset"); no hashing slot needed

mail off → 503 for everyone
email shape → 422
auth:per-ip → password-reset:per-ip → password-reset:per-email (typed, every address) → password-reset:global
record auth.password_reset.requested {email, ip}
202, empty body
```

**No account lookup in the request.** Status, body and timing are the same for every address because the request does the same thing for every address.

### Listener: decide, mint, send

```
drop the job if older than the link lifetime (30 min)  -- retries can run an hour later
token = 256 random bits                                 -- minted here, never in the event
tx: lock user by email; nothing sent and nothing recorded when:
      no account | suspended | request made before the account was created
      | request made before the account's last email change (email_changed_at)
    upsert password_resets (user_id unique, digest = sha256(token), expires 30 min)  -- one live link per user
send APP_ORIGIN + "/auth/forgot-password/" + token      -- configured origin, never the Host header
record auth.password_reset.sent
```

### Page and completion

- The page `/auth/forgot-password/<token>` first calls `POST …/check {token}` → 204 or 422 `field: "token"`. The token travels to the API **only in a JSON body**. The page is never challengeable and loads nothing third-party.
- `PATCH /api/auth/password-resets {token, newPassword}` under `auth:per-ip` + hashing slot:
  1. token shape, new-password rule (422);
  2. a cheap validity check before hashing (don't burn Argon2 on a dead link);
  3. tx: lock user, **then** the link (the same order as a password change, so they can't deadlock); recheck the link and that the user isn't suspended; save the hash; delete the link, **every** session and admin session; **clear the account lock and its failures**; mark the email verified; record `auth.password_reset.completed`;
  4. answer `{email}` so the login page can prefill it. **Nobody is signed in** (if MFA is on, a reset link alone must not create a session).
- **MFA stays on.**
- Send the owner a **"Your password was reset"** notice (security mail: no link that acts; "if this wasn't you, reset your password again and contact support").

### What each caller sees

| Case | API | Inbox |
| --- | --- | --- |
| Unknown address | 202 | nothing |
| Active account | 202 **identical** | link (30 min, single use) |
| Suspended | 202 identical | nothing (suspension also deleted live links) |
| Locked | 202 identical | link; completing it clears the lock |
| Unverified | 202 identical | link; completing it verifies the email |
| Blocked address | 202 identical | n/a (only accountless addresses can be blocked) |
| Dead, used or malformed token | 422 `token`, one message | |
| Two tabs completing one link at once | one 200, one 422 `token` | |

**Where the paths converge:** entirely at the request. The listener is the only place that knows.

UI copy is conditional for everyone: "If an account uses this email, we've sent it a link to reset the password. It works for 30 minutes."

## Change password (signed in)

| | MFA off | MFA on |
| --- | --- | --- |
| UI steps | current password → optional `POST …/password/verify` (204/422) → new + confirm → `PATCH` | new + confirm (client-only check) → code → `PATCH` |
| Body | `{currentPassword, newPassword}` | `{newPassword, code}`; any `currentPassword` is ignored |
| Limit | `password-check:per-user` (user ID) | `mfa:per-account` (user ID); a blank code → 422 before counting |
| Wrong factor | 422 `currentPassword` | 422 `code` (also when the right password is sent without a code) |

```
PATCH /api/auth/password            # recovery route: unmetered by the per-user budget, keeps its auth limits
tx: lock user row
    required factor from the locked row (MFA on → code; off → password)
    verify factor (consumes the TOTP step or recovery code)
    newPassword ≠ current?  (an Argon2 check against the current hash, only AFTER the factor passed)
       → 422 {field: "newPassword"} and ROLLBACK, so the code isn't consumed
    save hash; delete reset link; delete EVERY session (incl. this one) and admin sessions;
    clear the account lock; cancel any pending email change
    record auth.password_changed
create a new session + CSRF token for this browser (rotation); the client swaps them in
listener: mail the owner "Your password was changed" (no acting link; "if this wasn't you, reset your password")
```

- **Same-as-current is checked after the factor passed.** Otherwise "must differ from your current password" is a password oracle for a stolen session.
- **The server decides the factor from the locked row.** A client sending `{currentPassword}` to an MFA account gets 422 on `code`.
- **Recovery routes are unmetered by the per-user write budget** (but keep `auth:per-ip`, their own per-user limits and the hashing slot): a stolen session must not be able to burn the owner's budget and stop them changing the password.

## Change email (signed in)

A singleton resource for the current account: `GET` (resume this session's pending change), `POST` (start or replace with fresh proof), `PATCH {code}` (complete), `DELETE` (cancel, idempotent).

### Request, verification on

```
POST /api/auth/email-change {email, currentPassword | code}

body-only validation (email shape; same as current address → 422 field email: only reveals the account's own value)
email-change:per-user → email-change:per-email (destination, every address alike) → email-change:global
fresh proof chosen by the locked row (password, or code when MFA is on) → 422 on the field
   a refused proof or limit leaves any earlier pending request usable
replace this account's pending change (bound to user AND this session's digest)
record auth.email_change.requested
202 {pending: {email, expiresAt, state: "sending"}}
```

**No lookup of the destination** in the request or in `GET`. The listener, inside its transaction:

| Destination | Listener does | Requester sees |
| --- | --- | --- |
| Free | stores HMAC(code), sends the code | `state` goes `sending` → `code` |
| Owned by another account | stores HMAC of a **random code nobody gets**; sends that account's owner an "Email change requested" notice (no code, no requester details; capped in the listener, sharing the cap with "you already have an account") | **identical** `sending` → `code` |
| Blocked | stores HMAC(code); `Send` refuses; treated as done, no `.sent` | identical |

### Completion

```
PATCH {code}   (only the initiating session; destination taken from the row, never the body)
  not issued yet → 409 {pending: true} (no attempt spent)
  expired / replaced / cancelled / other session → 410
  wrong → 422 {field: "code"}; 5th wrong → 410
  right → tx: user lock → session lock → pending row → lockEmail(destination)
          destination taken OR blocked → 409 "This email address cannot be used. Choose another address."   (one error for both; code holder only)
          reserve the completion cap inside the tx
          history row (old address, owned_from, released_at = now); new email; email_version + 1; email_changed_at
          delete reset links, verification codes, MFA challenges, unconfirmed TOTP setups
          revoke every app and admin session
          record auth.email_changed
        then rotate this browser's session; if rotation fails after commit → 401 "Sign in with your new address"
```

- **Notices to both inboxes**, from two independent listeners (one failing doesn't resend the other), each reading the **committed event's captured addresses**, never the current one. The old-inbox notice **never shows the new address**. Neither carries a reversal token; they're notices, not approvals; they aren't rate-limited (security mail).
- **Ownership generation:** `email_version` is captured when a sign-in or MFA challenge starts and rechecked under the account lock before any session is created; `email_changed_at` rejects queued reset or verification work from an earlier ownership (A→B→A).
- **Survives:** user ID, password, MFA, remaining recovery codes, memberships, policy agreement, API keys.
- **NEVER** merge accounts, reserve addresses, or let sign-up or an email change claim an address an account already uses.
- A password change, MFA change or session revocation cancels a pending email change.
- The former-address history exists only for time-bounded erasure; it is never a login alias, reservation or lookup key.
- **Concurrency:** the final claim takes the same email-keyed lock as sign-up and blocking, and never locks the other user's row (that deadlocks A↔B swaps).

### Verification off

`POST` completes directly after fresh proof: 200 with the session; the new address is **unverified**. Taken and blocked share the same generic 409, matching verification-off sign-up (an accepted leak of that mode; production runs with verification on). The `requested` event is still committed so the owner of a taken address gets the notice. Use a savepoint so a refused claim doesn't consume a recovery code.

### What each caller sees

| Caller | Sees |
| --- | --- |
| Initiating session | its own pending destination, expiry and state |
| Another session of the same account | `{pending: null}`; `PATCH` → 410; may replace with fresh proof |
| Owner of the destination account | an informational notice only |
| Blocked destination | nothing |
| Stolen session without proof | can't start a change |
| The requester's data export | no `auth.email_change.sent` (it carries `existingAccount`) |

### Tests

- POST, GET (after the listener) and wrong-code `PATCH` for a free, a taken and a blocked destination: **byte-for-byte** (status, body with the address substituted, headers except request ID) and the same tables touched; only the inboxes differ. Status and `state` alone are not enough.
- A right code after the destination was taken or blocked → the same 409 body for both.
- Concurrent claims of one address by two accounts: one wins, the other 409, never 500.
- A→B→A: a reset queued before the change is never delivered.

## Security notices: the rule

Send the owner a fixed-text notice for every change an attacker would want to hide: password changed, password reset, MFA turned off, email changed (both inboxes), a recovery code used, a successful sign-in during a lock. Notices:

- go from listeners after commit, to the address captured in the event;
- contain no acting link except "reset your password" (and the lock's unblock link);
- are never suppressed by a block (only accountless addresses can be blocked) and never capped, **except** those an anonymous or password-only caller can trigger repeatedly (lock notices, "you already have an account", "email change requested"), which have a listener-side cap.
