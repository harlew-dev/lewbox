---
name: security-defaults
description: The security rules every line of application code follows by default, in any stack - sessions, passwords, CSRF/Origin, headers and CSP, enumeration resistance, authorisation, fresh proof for sensitive actions, API keys, emailed codes and links, redirects, injection/XSS, logging, caching user data, production config and dependency hygiene. Load before adding or changing an endpoint, a sign-in or account flow, a permission, a token or secret, a cookie, a redirect, a log line, a template or a config variable, and before claiming any security property.
---

# Security defaults

These are defaults, not features: they apply to every endpoint, page and job without anyone asking. Most rules exist because an attacker only needs the one place you forgot.

A project's `AGENTS.md` may record deliberate deviations from this skill; where it does, the project's deviations win.

Out of scope here (see `production-hardening`): rate limits and quotas, CAPTCHA/challenges, admin dashboards and admin step-up, monitoring and alerting, privacy, retention and compliance. Naming of permissions and events is owned by `resource-naming`; status codes and error bodies by `api-design`; how to test these rules by `testing`.

## When to use

- Adding or changing any endpoint, especially a mutation or anything signed out.
- Touching sign-in, sign-up, sessions, passwords, second factors, password reset, email verification or email change.
- Adding a permission, role, tenant-scoped resource or anything that reads "who is asking".
- Creating a token, code, API key, or any secret shown to a user.
- Building a link, redirect, `?next=` or a page that reads the URL.
- Writing SQL, HTML/email templates, log lines, error messages or caches.
- Adding a config variable or a production default.
- Writing docs or a PR description that says the app "has" a security feature.

## Reference files

| File | Read when |
| --- | --- |
| `authentication` skill | Building sessions, login/logout, password storage, password change/reset, second factors, email change, or any flow that creates or ends a session (moved out of this skill) |
| [references/tokens-and-codes.md](references/tokens-and-codes.md) | Creating any random token, emailed link, short verification code, or deciding hash vs HMAC vs encryption for a stored secret |
| [references/csrf-and-headers.md](references/csrf-and-headers.md) | Writing the request security middleware, the API client's CSRF handling, response headers, CSP, or request size/time caps |
| [references/authorisation.md](references/authorisation.md) | Adding a role, permission, tenant-scoped route, membership change, or any write that depends on who may do it |
| [references/api-keys.md](references/api-keys.md) | Adding API keys, personal access tokens, machine routes, scopes, or idempotency keys |
| `authentication` skill | Any signed-out endpoint that takes an email/username/ID, or any response that could differ for "exists" vs "doesn't" (moved out of this skill) |
| [references/output-logging-and-caching.md](references/output-logging-and-caching.md) | Writing SQL, templates, redirects, `?next=`, notices from the URL, log lines, error handling, or client/server caches |

## 1. Secrets at rest: store digests, compare in constant time

- **ALWAYS** generate tokens from the OS CSPRNG (`crypto/rand`, `secrets`, `crypto.randomBytes`, `random_bytes`), at least **256 bits**, encoded base64url. NEVER `Math.random`, UUIDv1/v4-as-secret, timestamps or incrementing IDs.
- **NEVER** store a bearer secret (session token, reset/invite link token, API key) in plaintext. Store **SHA-256 of the whole token** and look up by digest. A database leak then yields nothing usable.
- **Short codes** (6-digit email codes, recovery codes) MUST be stored as **HMAC-SHA256 under a server key**, not a plain hash: there are only 10^6 six-digit codes, so a plain hash is reversed instantly. See [tokens-and-codes.md](references/tokens-and-codes.md).
- **Secrets you must read back** (TOTP seeds) MUST be encrypted (AES-256-GCM) under a key derived from the app key, with the owning row's internal ID as associated data, so a value copied to another row fails to decrypt.
- **ALWAYS** compare secrets, digests and codes in constant time (`hmac.Equal`, `crypto.timingSafeEqual`, `hash_equals`, `secrets.compare_digest`). NEVER `==`.
- Every secret and code is **single-use where possible, short-lived, and attempt-capped** (e.g. 5 wrong tries, then the code/challenge is deleted).

## 2. Sessions

Owned by the `authentication` skill (`references/sessions.md`): storage, cookies, CSRF token storage, rotation and revocation. The non-negotiables, so nobody misses them:

- **MUST** be opaque server-side sessions (32 random bytes, only the SHA-256 digest stored) in a `__Host-`, `HttpOnly`, `Secure`, `SameSite=Lax` cookie, with idle and absolute expiry. No JWTs for browser sessions: they can't be revoked.
- **Rotate** on sign-in and every privilege or credential change; logout deletes the row, not just the cookie.
- **NEVER** put session or CSRF tokens in `localStorage`, `sessionStorage`, URLs, logs or analytics.

## 3. Passwords

Owned by the `authentication` skill (`references/sign-in-mfa-and-lock.md`): Argon2id parameters, length rules, the dummy hash, rehashing, hashing concurrency. Two rules that apply beyond auth flows:

- **NEVER** log, store, echo or put in an event a plaintext password, even a wrong one.
- A wrong *current* password while signed in is **422 with `field`**, never 401: clients treat 401 as "session expired" and sign the user out.

## 4. CSRF, Origin and CORS

For every mutation (anything except GET, HEAD, OPTIONS), browser routes MUST check, before the handler runs:

| Check | Rule | Fails with |
| --- | --- | --- |
| Origin | `Origin` header **exactly equals** the configured app origin (scheme+host+port). Missing Origin on a mutation = refuse | 403 |
| Fetch metadata | `Sec-Fetch-Site: cross-site` = refuse | 403 |
| Content type | `Content-Type: application/json` (DELETE without a body exempt). Forms can't send JSON cross-site without a preflight | 415 |
| CSRF token | Signed-in mutations send `X-CSRF-Token`: an independent random token issued with the session, compared in constant time | 403 |

- **NEVER** enable CORS for the browser API. One origin serves pages and API. NEVER `Access-Control-Allow-Origin: *` with credentials, and NEVER reflect the request's Origin.
- **NEVER** make GET change state.
- The CSRF token lives in memory (the session context), returned by login/sign-up/`me`; the API client attaches it automatically. Not in storage or URLs.
- Machine routes (bearer API keys) use a different, explicitly selected middleware branch: see section 9.

Detail and middleware pseudocode: [csrf-and-headers.md](references/csrf-and-headers.md).

## 5. Headers, CSP and request caps

- **API responses:** `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, and a server-generated `X-Request-ID`.
- **HTML pages:** CSP with `script-src 'self'` plus the **SHA-256 hashes** of the page's own inline scripts, computed from the built files at startup or build. **NEVER `'unsafe-inline'` or `'unsafe-eval'` for scripts.** `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'none'`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`, HSTS in production.
- **NEVER load third-party resources** (CDN scripts, fonts, analytics, embeds). Self-host fonts and assets. A third-party script runs with full access to your users' sessions; a third-party font or image leaks every page view. Any exception is one named origin, added to CSP only while configured, and documented.
- The app shell and any page holding user data is `no-store`; only content-hashed assets get long cache lifetimes.
- **Caps:** request body (e.g. **16 KiB** default, raise per route only when needed), headers (e.g. 16 KiB), a per-request deadline (e.g. **10 s**), server read/write/idle timeouts. Unbounded input is a denial of service.
- 500s answer a generic message plus the request ID, never the error.

## 6. Enumeration resistance

Owned by the `authentication` skill: nothing an outsider can observe (status, body, headers, cookies, timing, emails, rate limits, CAPTCHA, DB writes, events, UI copy, redirects) may reveal whether an account, address, tenant or record exists, or its state, until they prove ownership. Read it before building any signed-out endpoint that takes an email, ID, token or key. The rule that applies to every route here:

- **Records:** missing, malformed ID, another tenant's, and "not a member" are the **same 404**, with no query for a malformed ID. 403 is only for "you are a member but your role lacks this permission". Test byte for byte, not status only.

## 7. Authorisation

- **Every mutation route names its permission** (`<resource>.<action>`, per `resource-naming`) at registration. The router **MUST refuse to start** (panic/throw at boot) if a tenant-scoped mutation is registered without one. A route every member may do names an explicit "any member" permission, so "forgot" and "everyone" never look the same.
- **One grants table** maps role to permissions. Nothing else encodes "admins can...". Unknown roles, scopes and modes grant **nothing** (fail closed: `default: return false`).
- **Tenant and user come from the authenticated context, never the request.** Load the tenant by joining through the caller's membership in one query; put tenant and role in the request context. NEVER accept `tenantId`, `userId`, `ownerId` or `role` from a body or query to decide access. Every query takes the tenant ID from context.
- Child records are looked up **by tenant AND public ID**. A project ID from another tenant (even one the caller belongs to) is a 404.
- **Recheck inside the transaction under locks.** A write that depends on authority (role changes, removals, leaving, invitations, deletes, key management) locks the tenant row (`SELECT ... FOR UPDATE`), re-reads the actor's role and the target, then acts. The middleware check is a fast path; the in-transaction check is the truth. Use one global lock order (e.g. user, then tenant, then children by ID) or you will deadlock.
- Invariants are checked under the same lock: "a tenant always has an owner" (409 on the last owner leaving/demoting), "an admin may only assign roles below admin", "an invitation can never grant owner".
- The API returns the caller's computed `permissions` with the tenant; the UI hides controls with them. The UI never keeps its own copy of the rules, and hiding is never the enforcement.
- Unguessable public IDs are not access control. Every route checks membership.

Detail (grants table, route registration, lock order, matrix test): [authorisation.md](references/authorisation.md).

## 8. Sensitive actions and secrets shown once

- **Fresh proof in the same request** for: deleting the account or a tenant, changing password or email, exporting personal data, creating or broadening API keys, viewing or regenerating recovery codes, disabling a second factor (password **and** code).
- **The server chooses the factor** from the locked account row: second factor enabled means an authenticator/recovery code; otherwise the current password. The client cannot pick the weaker one; a correct password with no code when a code is required is refused.
- Proof is checked **inside the action's request and transaction**, never by a separate "verify" endpoint whose success is remembered. (A UX pre-check endpoint is fine, but the action checks again.)
- High-blast-radius deletes add a typed confirmation (the tenant's current name), rechecked under the lock.
- A wrong proof is **422 on the field**, not 401, and a refused action leaves earlier state usable (a pending request, an unused code).
- **Secrets shown once:** a created API key, a TOTP seed, recovery codes. Returned only in the creating response, with `Cache-Control: no-store`. The client keeps them in the dialog's component state only; never in a global data cache, `localStorage`, URL, toast, log, event, error report or export. Navigating away or closing discards them. There is no "show again" endpoint.

## 9. API keys and personal access tokens

- **Format:** `<app>_<kind>_<publicId>_<secret>` (e.g. `acme_pk_apk_8kQ2mZ1xT4bLwN_<43 base64url chars>`). The prefix makes leaked keys greppable by secret scanners; the public ID identifies the key in UIs and logs without revealing it.
- Store **SHA-256 of the complete credential** only; compare in constant time. Revocation clears the digest at once. No positive cache of valid keys.
- Sent only as `Authorization: Bearer <key>` over HTTPS. Never in URLs, bodies, cookies or a custom header alias.
- **Scopes** use the same permission names as roles; one key binds to one tenant; unknown scopes fail closed. A personal key's authority is **saved scopes intersected with the owner's live membership and role**, re-evaluated on every request.
- **The route decides** browser security (session + Origin + CSRF) or machine security (bearer + scope), never the presence of an `Authorization` header. Cookies have no authority on machine routes; keys have no authority on browser routes. Never fabricate a human session for a key.
- Key management is browser-session only, behind CSRF and fresh proof for creation/broadening. A key can never manage keys.
- **Expiry is explicit** at creation (finite or "never", never omitted); it can be shortened, never extended. **Quotas** on live keys per tenant and per user. Rotation is create, deploy, revoke.
- **Idempotency-Key** on creates: caller-generated, validated (e.g. 16-128 safe chars), scoped to credential + tenant + operation, kept 24 h, stored as a digest. Same input = replay the original response; different input = 409. Never log it.
- Writes re-authorise inside their transaction; revocation or demotion that commits first wins.

Detail: [api-keys.md](references/api-keys.md).

## 10. Emailed codes and links

- **Codes** (email verification, email change): 6 digits from the CSPRNG, **HMAC-SHA256 under a key derived from the app key**, 15-minute lifetime, 5 wrong tries then the code dies. Bound to the browser/session that started the flow (found by a cookie, never by email). Never in a URL, event or log; shown in the email as text, not a link.
- **Link tokens** (password reset, invitation, unsubscribe/block): 256-bit, SHA-256 digest stored, short life (reset: **30 minutes**, single use; invitation: 7 days, single use), the newest replaces the older one.
- A link token may appear in **its own page's path** (and in a `?next=` pointing at that page). It travels to the API **only in a JSON body**, never an API URL. Pages that hold one load nothing third-party and send a strict referrer policy.
- **Possession of a link proves possession of the link, not of the inbox.** Invitation links never verify an email.
- Send email from a background job after the response, so delivery work and timing never leak account existence.

Detail: [tokens-and-codes.md](references/tokens-and-codes.md).

## 11. Links, redirects and notices

- **Build every absolute link** (emails, redirects, OAuth callbacks) from the **configured app origin**, NEVER the request's `Host`/`X-Forwarded-Host`. A poisoned Host header otherwise mails your users a reset link to the attacker's domain.
- **`?next=` is an allowlist**, not a blocklist: accept only a path that starts with a single `/` (reject `//host`, `/\host`, anything with a scheme), stays on our origin after URL parsing, and matches a known signed-in route pattern with well-formed IDs. Everything else falls back to the default page. Use it only for navigation, never to submit anything.
- **Nothing from the URL is ever displayed.** A `?notice=` key maps to a fixed message in code (`account-exists` -> "You already have an account..."); unknown keys show nothing. Never render `?error=`, `?message=` or a name from the query.
- Pass state between auth pages (a typed email) in navigation state or the body, not the query string.
- Signed-out requests to signed-in pages redirect to login **before any HTML is sent**.

## 12. Injection and XSS basics

- **SQL:** every value through placeholders/bound parameters (`?`, `$1`) via the query builder. NEVER concatenate or interpolate input into SQL text. Identifiers that vary (sort columns, directions) come from a fixed map keyed by an allowlisted input value. `LIKE` input escapes `%`, `_` and `\`.
- **HTML:** use the framework's autoescaping templates/JSX. **NEVER cast to "safe"**: `dangerouslySetInnerHTML`, `v-html`, `{@html}`, `innerHTML`, `template.HTML`, `|safe`, `{!! !!}`, `mark_safe`. If rich text is a product need, sanitise server-side with an allowlist sanitiser and test it.
- **Email:** HTML templates with autoescaping (e.g. Go `html/template`, never `text/template` for HTML); escape every name; **never put user data in mail headers** (header injection; misleading subjects). Fixed subjects.
- **URLs in output:** only `https:`/relative links you built; never render a user-supplied URL as `href` without scheme validation.
- **Strict input:** reject unknown JSON fields (400), validate lengths, trim, and reject invisible/control/bidi formatting characters in names that other people will see.
- No shell, `eval`, or dynamic `require/import` with input.

## 13. Logging and errors

- **NEVER log request contents**: path, query string, headers, cookies or body. They hold link tokens, CSRF tokens, API keys, searches and emails. The access log records the **matched route pattern** (`/api/projects/{project}`) or a fixed page label, never the raw path.
- **Errors are scrubbed** before logging or storing: database and SMTP errors quote typed values and emails. Log through one helper that scrubs (`errorAttr(err)`), never the raw error. Scrubbing is a backstop: NEVER build an error or panic message from a secret.
- Public 500s say only "Something went wrong" plus the request ID. The request ID is always server-generated, never taken from the request.
- Never log or emit in events: passwords, hashes, tokens, codes, keys, TOTP seeds, recovery codes, Idempotency-Keys.
- Client-side error reports send the route pattern, never `location.href`.

Detail: [output-logging-and-caching.md](references/output-logging-and-caching.md).

## 14. Server-rendered and cached data

- **No user data in server-rendered or prerendered HTML** unless that response is `no-store` and per-user. Prerendered/static pages are built without a session.
- **NEVER cache user data in module-level/global variables** on the server or the client. A module-level variable in a server process is shared by every request; in an SPA it outlives sign-out.
- **Every client cache key includes the user ID** (e.g. SWR/TanStack Query key `[path, userId]`), so one account never sees another's cached data on a shared browser.
- On sign-out or account deletion, navigate away then **clear the whole client cache**.
- API responses and the app shell are `no-store`. CDN/proxy caches never cache authenticated responses.

## 15. Production configuration: refuse to start when insecure

The server validates config at boot and **exits with a message naming the setting** when production is insecure. Never "warn and continue". Examples:

- App origin not `https://`; database TLS not verifying the certificate (`sslmode=verify-full`); missing or short app key.
- SMTP without TLS; a dev/log mailbox (it stores working links); debug or demo seed enabled.
- Placeholder legal/template content still marked as boilerplate.
- A feature that depends on another that is off (e.g. email verification on with mail off).
- Destructive dev commands (`migrate-fresh`, reset) refuse when the environment is production.

Secrets come from the environment/secret store, never the repo. Never log config values that are secrets, or their digests.

## 16. Dependency hygiene

- **NEVER silence the type checker** with `any`, `as unknown as T`, `@ts-ignore`, `# type: ignore`, `interface{}` casts or equivalent to make a dependency "work". Fix the version or the types; types are part of the security boundary (they're what stops a raw ID or unsanitised string from reaching a sink).
- **Preserve lockfiles**; commit them; never delete and regenerate one to dodge a conflict.
- Run the dependency audit on any dependency or auth change. Every compatibility override (`overrides`, `resolutions`, `replace`) is written down with why.
- New dependencies on the request path or in auth need a reason; prefer the standard library for crypto.

## 17. Claims

- **Don't claim a security feature exists until it is implemented and tested.** No "supports SSO", "encrypted at rest" or "passkeys" in docs, UI copy, PRs or README until the code exists and a test proves the boundary.
- Keep a short "Not implemented" list in the security docs (e.g. passkeys, OIDC, staff overrides), so readers don't assume.
- Each rule above that a regression could silently break gets a test named after the property (e.g. `TestSignUpLooksTheSameForATakenEmail`, `TestEveryMutationNamesAPermission`). See `testing`.

## Pre-merge checklist

- [ ] Every new mutation is behind Origin + Sec-Fetch-Site + JSON + CSRF (browser) or bearer + scope (machine), selected by route.
- [ ] Every tenant-scoped mutation names a permission; the router would refuse to boot without it; the role x route matrix test has a row for it.
- [ ] Tenant and user come from the auth context; no access decision uses a body/query ID, owner or role.
- [ ] Authority-dependent writes recheck under a lock inside the transaction, in the global lock order.
- [ ] Missing, malformed, other-tenant and non-member are the same 404; no response differs for "exists" vs "doesn't".
- [ ] New tokens: CSPRNG, >= 256 bits, digest stored, constant-time compare, expiry, single use. Short codes: HMAC, attempt cap.
- [ ] Sessions rotate on privilege change; no token in storage or URLs.
- [ ] Sensitive actions take fresh proof chosen by the server, checked in the same transaction.
- [ ] Secrets shown once: `no-store`, component state only, never logged/cached/exported.
- [ ] Links built from the configured origin; `?next=` through the allowlist; nothing from the URL rendered.
- [ ] SQL uses placeholders; no "safe" casts in templates; unknown JSON fields rejected; caps on body/headers/time.
- [ ] No request contents or raw errors in logs; 500s are generic with the request ID.
- [ ] No user data in prerendered/server-rendered output or module-level variables; cache keys include the user.
- [ ] New config validated at boot; production refuses insecure values.
- [ ] No `any`/unsafe casts added; lockfiles intact; audit run.
- [ ] Docs updated; nothing claimed that isn't tested. Rate limits for the new route checked against `production-hardening`.
