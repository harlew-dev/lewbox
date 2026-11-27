# Rate limits and quotas

A rate limit slows how fast something happens. A quota caps how many rows can exist at once. You need both: a rate limit of 100 projects an hour still allows 876,000 a year, and only a quota stops that.

## Four layers

| Layer | Applies to | Counted by | Example |
| --- | --- | --- | --- |
| **Budget** | every signed-in request, automatically | user | `api:per-user` 300 writes / 15 min; `api-read:per-user` 1,000 reads / 5 min |
| **Feature limit** | each route that creates rows, sends mail or checks a secret | user, IP, email or global | `project:per-user` 100 / hour; `auth:per-ip` |
| **Quota** | each table that grows per user or per tenant | the parent row | 1,000 projects per tenant; 20 tenants owned per user |
| **Concurrency cap** | CPU-heavy work such as password hashing | per process | 4 hashing slots, then 503 |

The page and anonymous floods (static pages, the SPA shell, health probes) belong to an **edge limiter** (CDN, WAF or proxy), not the app.

```
request → origin/content-type checks → session + CSRF
        → budget (api:per-user | api-read:per-user)
        → permission check
        → feature limits: per-subject first, then shared
        → body validation → CAPTCHA (if challenged) → side effects
        → store: quota check inside the insert's transaction
```

## Naming: `<feature>:<counted-by>`

`counted-by` is one of `per-ip`, `per-account`, `per-email`, `per-user`, `per-key`, `per-tenant` (written as your tenant noun, e.g. `per-workspace`) or `global`. The `naming` skill owns the vocabulary. A level is appended for events and metrics: `auth:per-ip:challenge`, `register:global:block`.

- **ALWAYS** declare every limit in one registry file (e.g. `limits.<ext>`). A test lists them and checks every name matches `^[a-z-]+:(per-[a-z]+|global)$`.
- **NEVER** invent a new counted-by kind ad hoc. Agree it first and add it to the naming skill. An existing name may fit with a comment: the "revoke other sessions" limit counts **per session** but is still named `sessions-revoke:per-user`.

## Who is counted

| Situation | Count by | Why |
| --- | --- | --- |
| Signed in | the **user ID** | Counting by IP lets one user lock out a whole office behind one NAT |
| Signed out (login, sign-up, reset request) | the **client IP** | No identity yet |
| IPv6 client | its **/64** | A single host usually controls a whole /64 and could rotate the low bits forever |
| IPv4 client | the address | |
| Mail sent to a typed address | the **email**, trimmed and lower-cased | Caps mail to one inbox, whoever asks |
| Password guessing | the typed **email** (`login:per-email`, anonymous sign-in only) as well as the IP; signed-in password checks by **user ID** (`password-check:per-user`) | Credential stuffing spreads across IPs; password spraying spreads across accounts. Separate counters: see below |
| Second-factor codes | the **user ID**, from any IP | 5 per 15 min: each guess covers about 3 of a million TOTP codes, so an attacker with the password reaches about 0.14 % a day |
| Protection for everyone at once | `global` (a single shared key) | Caps a distributed flood (sign-up spam, reset mail) |

The client IP must come from the connection, or from a proxy header **only when the connection comes from a trusted proxy** (see `configuration-and-secrets.md`). If you trust any header from anyone, an attacker sets a fresh `X-Forwarded-For` on every request and gets fresh limits forever.

## Order: after CSRF, before side effects, per-subject before shared

- **After the CSRF/Origin check.** A forged cross-site request is refused before it counts, so another site can't spend a victim's budget and lock them out.
- **Before any side effect.** Every limit and the CAPTCHA check run before password hashing, inserts or queuing mail. A 428 or 429 changed nothing, so the client can safely send it again.
- **Per-subject limits before shared ones.** A client already refused by its own per-IP limit must not also spend the `global` budget that everyone else needs. Stop at the first refusal.
- **Shared limits count only once the body is valid.** Otherwise malformed junk drains the budget everyone shares.
- **Count successes too.** Fixed windows count every attempt. Counting only failures lets an attacker interleave one success to reset the count.

Example chains (each stops at the first refusal):

```
sign-in:       auth:per-ip → login:per-email
sign-up:       auth:per-ip → register:per-ip → register:per-email → register:global
reset request: auth:per-ip → password-reset:per-ip → password-reset:per-email → password-reset:global
invitation:    invitation:per-user → invitation:per-email → invitation:global
```

## Email-keyed limits must not reveal accounts

Any limit keyed by an email someone typed (`password-reset:per-email`, `register:per-email`, `invitation:per-email`) **MUST** count every address alike, registered or not, and refuse with the same status, message and timing. If only registered addresses were counted, the 429 would become an account-existence oracle.

**Separate counters per flow.** A per-email counter that an anonymous caller can hit **MUST NOT** be shared with any other flow or moved by any signed-in action. Anonymous sign-in counts only `login:per-email`; sign-up only `register:per-email`; signed-in password and code checks count by **user ID** (`password-check:per-user`, `mfa:per-account`). Otherwise:

- the owner's own signed-in activity shows up in a prober's 429 or `Retry-After` (an activity oracle), and
- sign-up spam for an address keeps the victim locked out of **sign-in**.

**`Retry-After` leaks too.** With windows that start at the first hit, `Retry-After` tells a prober when someone else first touched that address. Use **aligned windows** (`floor(now / length)`) for anonymous per-email limits, so every address gets the same `Retry-After` at a given moment, and keep the 429 text generic (never "This email address has had too many…"). The residue (a 429 says someone probed this address this window) is accepted and documented; it never says whether an account exists. The `auth` skill has the full reasoning.

When you need a cap that only makes sense for real accounts (for example "You already have an account" notices, at most 5 a day per account), count it **after the response**, in the background listener that would send the mail. Above the cap it silently sends nothing and records the crossing once. Allow this exception only when nothing important is held back. Never apply it to codes or security mail.

## Levels: challenge and block

Every limit has two optional levels, each written `Above(n)` or `Never`. **NEVER** use a bare number where `0` could mean "off" or "zero allowed". A test lists every limit's levels.

| Level | Above it | Response |
| --- | --- | --- |
| `challenge` | the request needs a solved CAPTCHA, on routes that opt in | **428** |
| `block` | every request is refused | **429** (per-subject) or **503** "busy" (shared/global) |

- **The challenge level is always below the block level.** Real people get a check (usually with nothing to click) long before anyone is refused. Example: `auth:per-ip` challenges above 5 / 15 min and blocks above 60.
- **The route decides whether a challenge can be shown, not the limit.** A route opts in by naming a challenge action (`login`, `register`, `password_reset`, `invitation`). Without one, the challenge level is skipped and only the block level applies.
- **A route that counts a shared limit with a challenge level MUST name an action.** Otherwise nothing could ask it for a token and its requests would get past the challenge level for free. Enforce this at runtime: answer 503 and log an error, so the mistake fails the route's first test instead of waiting for a flood.
- **Unsolved requests must never block a shared limit.** Above a global limit's challenge level, its block level counts only requests that **passed** a challenge, in a second counter `<name>:passed` whose window ends with the main one's. Otherwise bots that never solve anything, from any number of IPs, push `register:global` to 503 and lock every real person out. Per-IP limits keep counting unsolved requests: a flood from one address only reaches its own block level.
- With CAPTCHA off, challenge levels are skipped (but still recorded) and block levels still apply.

Details of the challenge flow are in `captcha-and-bot-defence.md`.

## Responses

- **429**, "Too many requests. Try again in 12 minutes." (or "Too many attempts…" for auth limits). `Retry-After` gives the seconds left in the window, rounded up. The message rounds the same remaining time to human units: "a minute", "12 minutes", "an hour", or whole hours past one hour.
- **503** with the shared limit's own message ("Sign-up is busy. Try again later.") and `Retry-After`. Use 503, not 429, because the client itself did nothing wrong.
- **428** for a challenge: `{"error": "Complete the check to continue.", "challenge": {"provider": "...", "siteKey": "...", "action": "login"}}`.
- The client shows the API's message as it is and **never retries a 4xx automatically**. Auto-retrying a 429 burns more of the budget.

## Recording: one event per window, plus a metric for every refusal

- **Event:** `rate_limit.reached`, written **only by the request that first crosses a level in a window**, with `limit` (`<name>:challenge` or `<name>:block`), `user` and `tenant` when known, `window`, `resetsAt`, and the IP in the event's IP column. A flood therefore writes one row per limit per level per window, not one per refused request, so it can't flood the audit table. `Never` levels are never recorded.
- **Exactly one crosser:** the counter's atomic increment returns this request's hit number (`UPDATE … SET hits = hits + 1 RETURNING hits`, or Redis `INCR`), so under concurrency exactly one request sees `n + 1`.
- **Metric:** `rate_limit_refused_total{limit="<name>:<level>"}` for **every** refusal. Its label values are bounded by the limits declared in code. Count a request refused at a challenge level once, whatever else it counts against. Also add `quota_refused_total{quota=...}` and `password_hash_busy_total`.
- **Log:** a warning the first time a shared limit refuses in a window. A global limit being hit means a feature is effectively off for everyone, which is an alert-worthy signal (see `monitoring-and-alerting.md`).

## Metered by default, opt out only with a reason

- **ALWAYS** apply the budget inside the session middleware itself, so every signed-in route, including ones added later, is metered without anyone remembering to do it.
- Opting out ("unmetered") needs a written reason next to the route, and the route then **MUST** have limits of its own. Valid reasons:
  - **Recovery routes:** logout, "sign out other sessions", change password, turn MFA off, regenerate recovery codes. Someone holding a stolen session could otherwise spend the owner's budget and stop them from recovering. These keep their auth limits (`auth:per-ip`, `password-check:per-user`, `mfa:per-account`) and the hashing slots. Revoking sessions gets its own limit counted **per session**, so a stolen session using up its own allowance can't stop the owner's session.
  - **Self-reporting endpoints** that the page calls on its own, such as browser error reports. A page stuck in an error loop would otherwise use up its user's write budget and leave them unable to save anything. Bound these with `client-error:per-user` and `client-error:global` instead.
- **Machine API traffic** (API keys) has its own **block-only** budgets: it never spends browser budgets and is never challenged, because a script can't solve a CAPTCHA.

## Quotas: under a lock, answered with 422

- **MUST** check inside the same transaction as the insert, after locking the parent row (`SELECT … FROM tenants WHERE id = $1 FOR UPDATE`), then `COUNT(*)`, then insert. Without the lock, two concurrent creates at 999 both see 999 and both succeed.

  ```sql
  BEGIN;
  SELECT id FROM tenants WHERE id = $1 FOR UPDATE;
  SELECT count(*) FROM projects WHERE tenant_id = $1;  -- refuse if >= 1000
  INSERT INTO projects (...) VALUES (...);
  COMMIT;
  ```

- **422** with a plain message and **no `field`**, because no form field is wrong: "A tenant can have up to 1,000 projects." The UI shows it in the create dialog.
- Count everything that will occupy a slot. Members **plus live invitations** count against the member quota, so 100 pending invitations can't overshoot it on acceptance. Acceptance itself fails only when the tenant is already full without that invitation.
- Lowering a quota later keeps the existing rows and refuses only new ones. An integrity check (see `monitoring-and-alerting.md`) reports tenants strictly over quota.
- Quotas stop **creating**, not being given. Being made an owner by someone else isn't refused at the cap.
- Seeders and fixtures insert directly and aren't held to quotas.
- Quotas also bound expensive reads. "Download my data" can stay synchronous because 20 tenants × 1,000 projects × 10,000 events is a known ceiling.

| Typical quota | Max | Locked row |
| --- | --- | --- |
| Tenants a user owns | 20 | the user |
| Projects per tenant | 1,000 | the tenant |
| Members + live invitations per tenant | 100 | the tenant |
| Live invitations per tenant | 50 | the tenant |
| Live API keys | 100 per tenant / 50 personal | the tenant or user |

## Storage

| Store | Use for | Trade-off |
| --- | --- | --- |
| **Database** table `rate_limits(key, hits, resets_at)` or Redis | writes, sign-in, anything that sends mail, all shared/global limits | exact across servers; one write per counted request |
| **In-process memory** | high-volume reads only | no write per request, but each of N servers counts separately, so a user gets N× the budget. Fine for load protection, wrong for security limits |

- **Fixed windows.** A key's count starts at its first hit and resets when its window ends. They are simple and exact. The known weakness is up to 2× the limit across a window boundary, which is acceptable at these sizes. **Exception:** anonymous per-email limits use windows aligned to the clock, so `Retry-After` doesn't reveal when the first hit happened (see above).
- **Key with an HMAC, not a plain hash:** `<name>:<HMAC-SHA256(rateLimitKey, subject)>`, with `rateLimitKey` derived from the app key (HKDF, its own label), where the subject is the IP or /64, the normalised email, or the public user ID. A shared limit uses the bare name. A plain SHA-256 of an email or IPv4 address is reversed by hashing a list of candidates, so a dump would show who tried to sign in; with the HMAC the table holds nothing reversible, and it needs no retention entry beyond "until the window ends". Rotating the app key resets the counters, which is fine.
- **Cleanup:** a periodic job (e.g. every 15 minutes) deletes ended windows. An integrity check warns when ended windows linger more than 30 minutes.
- Sweep in-memory counters at most once a minute, and cap the number of anonymous keys (e.g. 100,000 per process). At saturation, new subjects get 503 with `Retry-After` rather than unbounded memory.

## Switches

- `RATE_LIMITS=off` exists only for end-to-end tests that sign up many users from one IP. It still keeps quotas and hashing slots, logs a warning, and **production refuses to start with it**.
- `PER_IP_LIMITS=off` is only for a proxy that cannot pass the client address at all. Per-account, per-email, per-user and global limits still apply. Production refuses it when a client-IP header **is** configured, since you'd be giving up the limits for nothing.

## Not limited in the app (push to the edge)

Health and liveness probes, static assets and the SPA shell, answers served from memory, and a wrong admin path key (no query is made, and guessing 128+ bits is hopeless). Put the probes behind an edge limiter if they are ever public.

## Starting values (from a previous production app; tune with real traffic)

| Limit | Value |
| --- | --- |
| `api:per-user` / `api-read:per-user` | 300 / 15 min; 1,000 / 5 min |
| `auth:per-ip` | challenge > 5, block > 60 / 15 min (every password, code and token-link check shares it) |
| `login:per-email` | 10 / 15 min, aligned windows; anonymous sign-in; the admin door's sign-in shares it (reachable only with the route key, so sharing reveals nothing to outsiders) |
| `password-check:per-user` | 10 / 15 min; signed-in password proofs (deletion, export, MFA setup, password change) |
| `mfa:per-account` | 5 / 15 min, plus 5 wrong codes end one sign-in challenge |
| `register:per-ip` / `:per-email` / `:global` | 10/h; 3/h; challenge > 200, block > 1,000/h |
| `password-reset:per-ip` / `:per-email` / `:global` | 10/h; 3/h; challenge > 50, block > 100/h |
| `invitation:per-user` / `:per-email` / `:global` | 20/h; 5/h; challenge > 100, block > 200/h |
| `register-code:per-email` | 10 / day for anonymous sign-up codes, plus 5 wrong tries end one code |
| `email-code:per-user` | 10 / day for signed-in verification and email-change codes, plus 5 wrong tries end one code |
| `export:per-user` | 5 / day, counted only once the password is right, so typos don't spend it |
| `admin-action:per-user` | 60 / hour; every attempt past step-up counts, including 404/409/422 |
| `client-error:per-user` / `:global` | 30/h; 500/h |

## Adding a limit to a new endpoint

1. **Signed in:** the budget is automatic. Opt out only for recovery or self-reporting routes, with a comment.
2. **Creates rows or sends mail:** declare `<feature>:per-user` in the registry and wrap the route with it, next to its permission, so one line in the route table shows everything that guards it.
3. **Grows without bound:** add a quota in the store's transaction under a row lock, and map it to 422.
4. **Sends mail to a typed address:** add `<feature>:per-email`, counted for every address alike, and shared with no other flow.
5. **Signed out:** per-IP first, then any `global` limit with a "busy" message, counted only once the body is valid.
6. **Challenge level:** set `Challenge: Above(n)` and opt the route in with an action, only if its page has no token in its URL, including in `?next=` (see the token-URL guard in `captcha-and-bot-defence.md`).
7. **Test:** pre-fill the counter to the limit (and the `:passed` counter for a shared limit with a provider), send one request, and assert the status, a `Retry-After` within the window, exactly one `rate_limit.reached` event and the metric incremented. Add the route to the documented limits table.
