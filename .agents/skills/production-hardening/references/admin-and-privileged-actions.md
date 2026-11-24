# Admin and privileged actions

The staff console can read every user's data across every tenant, which makes it the most valuable target in the app. Design it so that **no single compromise opens it**, and so that **every capability a stolen admin session has is one an attacker has**. Keep the console's powers reversible, bounded and audited. Keep irreversible or escalating powers on the CLI, which needs server or production-environment access.

## The layered door

| Layer | Stops |
| --- | --- |
| 1. Hidden path (unguessable key) | scanners, and anyone who doesn't know the door exists |
| 2. Optional IP allowlist / zero-trust proxy | a leaked key used from outside |
| 3. Admin grant made out of band (CLI) | escalation through the app or the console itself |
| 4. Password + MFA at **every** admin sign-in | a stolen password, or a stolen app session |
| 5. Separate, short-lived admin session | session theft from the app |
| 6. Role + step-up window + written reason for each write | a stolen session acting; unexplained actions |
| 7. Every read and action audited | abuse going unnoticed |

A stolen read-only session can only read, and every read is recorded. A stolen operator session can act only with the authenticator too, at most 60 actions an hour, and never against another admin (except signing them out).

## 1. Hidden path

- Serve the console at `/admin/<ROUTE_KEY>` (a 256-bit random key, from a CLI generator).
- **The key hides, it doesn't authenticate.** It leaks into browser history, proxy logs and screenshots. Never treat it as a credential.
- **Unset key = console off:** every admin URL is the ordinary 404. Safe default.
- **Every other `/admin…` URL, a wrong key, an unknown admin API path, a request from outside the allowlist and the previous key on a sign-in route all return exactly the ordinary 404:** same status, headers and body as `GET /nowhere`, and no database query. Only after the right key do you see 401 (no session) or 403 (wrong role or no step-up).
- **Validate the key's format at startup:**
  - a UUIDv4, or 32–128 chars of `[A-Za-z0-9_-]`;
  - refuse a single repeated character and the example file's key;
  - error messages never show the value.
- **Constant-time compare** SHA-256 digests against the current **and** previous key every time, with no early exit, so timing reveals neither whether a key matched nor which one.
- **Pages carry the key in the URL; API calls carry it in a header** (`X-Admin-Key`), never in the API path. Script injected into an ordinary app page shares the origin and could send the admin cookie, but it can't know the key. Keep the key in memory in the admin frontend: not storage, not cache keys, not page text.
- **Admin page headers:** `Referrer-Policy: no-referrer` (the key never leaves in a Referer), `X-Robots-Tag: noindex, nofollow`, `Cache-Control: no-store`.
- **Keep `/admin/` out of proxy and CDN access logs**, or redact the path. Your own access line logs `page /admin/*`.

### Separate admin frontend build

- **Ship admin code only to admins.** The public bundle must contain no admin route, admin API path or key header name, or the door's existence and API surface can be recovered from public JS. Enforce it three ways (mechanics: `web-delivery.md` §5):
  1. a bundler plugin that fails the app build when any admin module is resolved (catches aliases and dynamic imports);
  2. lint rules banning admin imports from non-admin code;
  3. a post-build scan of the public output for admin strings, with its own self-test.
- **Serve admin assets** under their own path (no key in it), only to browsers holding a short-lived **assets pass**: a stateless signed cookie set by every admin page.
  - **Value:** `expiry.HMAC(k, "admin-assets" ‖ keyDigest ‖ expiry)`, with `k` derived from the master key by HKDF with its own label.
  - **Scope:** the assets path, `HttpOnly`, `Secure`, `SameSite=Strict`, 8 h.
  - **Checks:** a constant-time MAC compare; an expiry in the future and no more than 8 h plus 1 min ahead. Check several same-name cookies, because a sibling subdomain could plant one.
  - Cache admin assets `private` only, **never in a shared cache or CDN**.
- The server refuses to start with an admin key set but no admin build.

### IP allowlist (optional, stronger in front)

- Accept addresses or CIDRs. Refuse `0.0.0.0/0`, `::/0`, unparseable and IPv4-mapped entries. Check the allowlist **after** a key matches, so a wrong key costs nothing.
- Answer a refused address exactly like a wrong key. Record the refusal (address only, never the path) **at most once per address (/64) per 15 minutes**, through a bounded async queue (64 entries, a seen-set of at most 10,000), so the request never waits on the database and the timing stays close to a wrong key's.
- **A refusal means the key leaked:** rotate it outright, and alert on the first one.
- Behind a proxy it needs trusted client-IP resolution, otherwise everything comes from the proxy and allowlisting the proxy lets everyone in. Warn at startup when the allowlist is set without a client-IP header.
- **Stronger:** put Cloudflare Access, a VPN or Tailscale in front of the admin paths. The in-app allowlist is the layer that works everywhere.

## 2. Separate account, separate session, MFA

- **An admin is a normal user account plus a row in an `admins` table** (role, MFA setup window). There is no separate password store.
- **Always ask for a code at admin sign-in**, even if the app session passed MFA a minute ago.
- **MFA is mandatory.** A new admin without MFA may enrol only within a **24 h setup window** from the grant, closed once MFA is on. Without the window, anyone holding only the password could enrol their own authenticator at the door. Outside it: the normal 401, a log warning, and `mfa_off` in the audit event.
- **One generic error** ("Email or password is incorrect.") for a wrong password, an unknown email (compute a dummy hash for timing parity), a non-admin, and an admin without MFA outside the window. This prevents admin enumeration.
- **Purpose-bound challenges** (`app`, `admin`, `admin_setup`): an app challenge can't finish an admin sign-in, or the reverse. Recheck admin status at the second step. Challenges last 5 min (15 min for setup) and die after 5 wrong codes.
- **Rate limits are shared with app login** (`auth:per-ip`, `login:per-email`, `mfa:per-account`), so guessing at either door draws on one budget. **Deliberately no global admin-login limit:** once the key leaks, a global limit would let anyone lock every admin out.
- With no usable client IP (a unix socket), refuse admin sign-in with 503 rather than create unattributable sessions.

| | App session | Admin session |
| --- | --- | --- |
| Cookie | `__Host-app_session`, `SameSite=Lax` | separate `__Host-app_admin`, **`SameSite=Strict`**, `HttpOnly`, `Secure`, `Path=/` |
| Idle / absolute | 30 min / 24 h | **15 min / 8 h** |
| Max per user | 5 | **2** (a third sign-in evicts the oldest) |
| Valid while | not expired | not expired, **still an admin**, and **created under the route key in this request** |

- **Independent:** signing in or out of one never affects the other. Store only token digests.
- **Bound to the route key:** rotating the key ends sessions. A request with the wrong key never refreshes a session.
- **CSRF:** a token header on every admin mutation, plus Origin and JSON content-type checks before the key check (see `security-defaults`).
- **Record IP and user agent, don't bind to them** (mobile networks change). The first request from a new /24 or /48 records an `admin.session.moved` event once per session. A stolen cookie used elsewhere then shows up in the trail.
- **Kill switches in the same transaction:** revoking admin, deleting the account, a password change or reset, an MFA reset or an email change each delete all admin sessions.
- **Admin read budget:** 1,000 reads per 5 min per admin. This caps bulk exfiltration through a stolen session.
- **Break-glass (CLI only):** `sessions revoke-all` ends every app and admin session and every pending MFA challenge.

## 3. Roles: out of band, least privilege

- **Grant, revoke and change roles only from the CLI.** No API route, UI or seeder. A compromised admin session or an XSS can then never create or escalate admins.
- **Roles:**
  - `viewer` (**the default**): every page and read;
  - `operator`: adds the actions, each behind step-up and a reason.
  - Give `operator` only to those who must act.
- **Enforce at route registration, not by convention:**
  - every mutating admin route is registered through a wrapper that names its role, and the router **refuses to start** (panics) on an unwrapped mutation;
  - an `Action(...)` wrapper composes role(operator) → step-up → per-admin action limit, so an action can't ship without its MFA gate;
  - a narrow "housekeeping" tier (operator plus rate limit, no code or reason) accepts only an explicit allow-list (acknowledge an alert, resolve an error group) and panics on anything else;
  - a source-reading test fails on any admin route outside the admin group or missing the key middleware.
- **Classify by what a stolen session could hide or do:**
  - **silencing an alert** hides a security signal, so it is a full Action;
  - **reading a secret** (a webhook signing key) is an Action, with `no-store` and an audit entry without the value;
  - **changing your own notification preferences, or removing only your own access** (signing yourself out), needs no role.
- **The UI hides operator controls from viewers; the API enforces regardless** (403).
- **Admin status never appears in an ordinary app response**, so the app API can't enumerate admins.

### Never on yourself, never on another admin

- Every action refuses (403) a target that is yourself or another admin. **The one exception:** "sign another admin out", which only removes access.
- Suspending an admin, resetting an admin's MFA and revoking admin are **CLI only**, so one compromised admin can't lock out the others or strip their MFA.
- **Invariants:**
  - a suspended account can't be granted admin, and an admin can't be suspended (revoke first);
  - an admin can't disable MFA;
  - a role change closes all of that admin's step-up windows.
- **CLI ergonomics:**
  - destructive commands preview without `--confirm`;
  - production refuses grants to seed or demo-domain accounts;
  - re-granting without a role keeps the existing role.
- **Access review:** flag admins with no admin sign-in for 90 days, or granted that long ago and never signed in. Never revoke automatically. Export the admin list as CSV for quarterly review.

## 4. Step-up window and reason

- `POST /api/admin/step-up {code}` checks a TOTP or recovery code exactly like login, under the authenticator's row lock. One 30 s TOTP step can be used once across login and step-up.
- Opens a **5-minute window** on that session, computed from the database clock. A window is used instead of a code per action because TOTP is single-use per 30 s and incident response needs several actions in a row.
- Outside the window: 403 `{"error":"Enter a code to continue.","stepUp":true}`. The UI prompts for a code and **retries the action automatically**.
- **Five wrong step-up codes over the session's life end the session** (a right code doesn't reset the count). The admin must sign in again with password and code.
- Step-up shares `mfa:per-account`. Accepted trade-off: a stolen cookie can burn that allowance once (15 min) but can't repeat it, because the fifth wrong code kills the session.
- The window belongs to one session. Sign-out, password or MFA change, revocation and role change all close it.

**Every privileged write needs a reason:**

- 10–500 characters after trimming, counted as characters, not bytes. Line breaks and tabs are allowed.
- **Refuse control characters** (C0, DEL, C1), Unicode format characters (bidi overrides like U+202E, zero-width characters, BOM) and U+2028/2029. A reason must not be able to hide or visually rearrange what the audit record says.
- The CLI uses the same validator.
- **Identity-sensitive actions** (resetting a user's MFA) also require `confirmed: true`: "I've confirmed who is asking, outside the app". This guards against social-engineered resets.
- Send targets and reasons in JSON bodies with public IDs, **never an email in a URL**.
- Return 409 for already-done or not-applicable, and 404 for a missing target (which records nothing).

## 5. Audited reads and writes

- **Record every read:** list, view, search, filter, export, monitoring page, IP lookup, even viewing the admin list. The only exception is a static catalogue with no user data. The trail then shows who looked at whom and who searched for which email or IP.
- **Write each action's audit event in the same transaction as the change** (`admin.<resource>.<fact>`, naming admin, target and reason). If the audit write fails, the change rolls back. For events with no data change, the request fails with 500.
- **Record shape:**
  - a public `evt_` ID and a name checked by a CHECK constraint;
  - `actor_id`, scope columns and a JSON payload;
  - the IP column (the **only** place IP is stored);
  - the server's own `request_id`, the user agent (cut to 256), an `admin_request` flag and `created_at`.
- **The admin is named in the payload, and the scope columns stay empty for admin reads and actions.** Deleting the viewed account must not cascade-delete the record of who viewed it, and the record must not appear in the subject's own activity or data export.
- **Payloads never contain** passwords, hashes, tokens, MFA secrets or codes, internal IDs, webhook URLs or keys. A test walks every event struct's fields, and another walks every admin response for forbidden field names.
- **Tamper evidence:** every audit event is also a structured log line shipped to an immutable store (see `observability.md` and `privacy-and-compliance.md`). There is no UI or API to edit or delete events.
- **Who sees it:** any admin, viewers included, through the events page and its CSV export, both of which are audited themselves. Admin reads are their own filter group, so reviewing who-read-what is easy.
- **No auto-refresh** on admin pages: every load is an audited read, so data isn't revalidated on focus or reconnect. That keeps the trail meaningful.
- **Retention:** audit classes are kept 1 year. At 30 days, cut IPs to /24 or /48 and replace emails (including searched emails) with keyed HMAC pseudonyms; searches still match because the filter pseudonymises the query too.

## 6. Fail closed (and the one deliberate fail-open)

| Situation | Behaviour |
| --- | --- |
| admin key unset | console off (404) |
| key set, admin build missing | refuse to start |
| previous-key request while the database is down | wrong-key 404, no assets pass |
| current-key page while the database is down | serve the shell (no data), API 500 |
| no usable client IP | admin sign-in 503 |
| audit write fails | the change rolls back |
| receiver secrets unreadable (master key lost) | receivers fail closed |
| **IP block list can't load** | **fail open on purpose:** start with nothing blocked, log, and retry with backoff (1 s doubling to 30 s); keep the last good list on reload failure; skip one bad row, not the whole list. Availability of the whole app must not depend on an abuse list |

## 7. Key rotation with a previous slot

Two settings: `ROUTE_KEY` and `ROUTE_KEY_PREVIOUS`. The previous key must differ from the current one, requires the current one, and is never logged (startup says only "set").

- **The previous key never opens the door.** Sign-in under it returns the wrong-key 404. It only keeps live sessions created under it working, with a response header `X-Admin-Key-Status: previous` and a banner: "This admin address is being retired. Ask your operator for the new one before this session ends."
- **Routine rotation** (every 90 days, or when someone with the URL leaves):
  1. generate a new key;
  2. set it as current and the old one as previous, on every server;
  3. restart;
  4. hand out the new URL;
  5. after at most the absolute session lifetime (8 h), remove the previous key and restart.
- **A possibly leaked key never goes in the previous slot.** Replace it outright, which ends every session under it.
- **The same pattern applies elsewhere:** webhook signing keys get a 15-minute dual-signature overlap. Derived secrets (assets pass) always use the current key.
- The app's **master key** (seals MFA secrets, keys pseudonyms) needs its own rotation design: a key ID stored with each ciphertext, decrypt with any known key, re-encrypt lazily or in a batch. Without that, rotation locks out every MFA user. Write it down as a known gap until it's built (see `configuration-and-secrets.md`).

## 8. Bounded blunt tools

Each tool has a cap, an expiry or a blast-radius limit:

| Tool | Bounds |
| --- | --- |
| **Suspend user** | revokes sessions, reset links, challenges and (optionally) pending invitations in one locked transaction; the user's tenants keep working for other members; sign-in says "suspended" **only after the correct password and code** (a wrong password gets the generic error, so status doesn't leak) |
| **Revoke a tenant's pending invitations** | for invitation spam; members and data untouched |
| **Retry failed deliveries** | only failed ones (a retry can't resend delivered mail); bulk: one listener, last 7 days, at most 1,000; a retried link gets a fresh token; minimised events can't be retried |
| **IP blocks** | every block expires (1 h / 24 h / 7 d / 30 d, CHECK ≤ 720 h); width at most IPv4 /16 or IPv6 /32 (CHECK); refuse a range containing the admin's own IP or overlapping a trusted proxy; at most 1,000 live blocks, counted under an advisory lock; enforced as the outermost middleware from an in-memory list reloaded every 30 s; **never blocks the admin door for a request that would pass it**, so a mistaken block can be undone. For floods, use the edge |
| **Sign another admin out** | the only action that may target an admin; they stay an admin |
| **Email suppression list** | the console can **unblock** but never block; only the address owner's own link can block (an admin block would let a stolen session stop someone signing up) |
| **Exports** | CSV at most 10,000 rows with `X-Export-Truncated`; formula cells (`=`, `+`, `-`, `@`, tab, CR) prefixed with `'` to prevent CSV injection; the export is audited before the first byte |
| **Expensive reads** | 1 in flight per admin, 2 per server; 5 s deadline, 2 s per section; IP search no wider than /16 or /32; name filters expanded server-side to an `IN` list, **never `LIKE` on admin input** |
| **Comments on incidents** | 1–2,000 chars; 60/h; 1,000 per cycle; redaction leaves a tombstone |
| **All actions** | `admin-action:per-user` 60 / hour, counting every attempt past step-up (404, 409, 422 included) |

## 9. Deliberate omissions

Write these down so nobody "helpfully" adds them:

- **No impersonation ("log in as user").** It turns one stolen admin session into every account.
- **No editing** anyone's name, email or roles, and no changing another user's email.
- **No deleting accounts or tenants from the console.** Erasure and export are audited CLI compliance commands.
- **No bulk suspension, and no suspending tenants.**
- **No admin "block email"**, no resetting another admin's MFA, no granting roles, and no global session wipe in the UI.
- **Email bodies are never stored**, so the console can't show them.

## 10. Other mechanisms worth copying

- **Test-enforced invariants:**
  - every admin route is in the admin group with the key middleware;
  - every mutation names a role;
  - responses contain no forbidden fields;
  - event payloads hold nothing sensitive;
  - nothing secret reaches the logs;
  - the public bundle has no admin strings.
- **Render other users' data only through escaping.** Ban raw-HTML injection by lint everywhere, and show payloads as escaped JSON.
- **Show suspension reasons to admins only**, and tell admins that reasons are kept in the audit trail.
- **Mail diagnostics** show the driver, host and sender, never credentials.
- **Dev-mode caveat:** if the dev server serves admin code without the key checks, data must still require the key, and the real gate must run in e2e and production builds.
