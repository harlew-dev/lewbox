---
name: ops
description: Everything that takes an app from working to safe and operable in production, in any stack. Covers rate limits and quotas, CAPTCHA and bot defence, logs, metrics and error tracking, monitoring and alerting, the admin console and privileged actions, privacy, retention and GDPR/SOC 2 evidence, transactional email, configuration and secrets, deployment and migrations, building and serving the web frontend, and incident response. Use when adding an endpoint that creates rows, sends mail or checks a secret; when adding logging, metrics, alerts, health checks, an admin feature, a table holding personal data, an email template, a config setting or a scheduled job; when preparing a release or first deploy; or when handling an incident.
---

# Production hardening

Working code is half the job. The other half:

- it survives abuse;
- it tells you what it's doing without leaking what users sent;
- it alerts a human when it's broken;
- it gives staff tools that can't be turned against users;
- it keeps only the data it needs;
- it refuses to start misconfigured.

This file is the map: each area's non-negotiables, and when to open its reference. The references hold the mechanisms, numbers and examples.

Related skills: `security` (sessions, CSRF, headers, CSP, password hashing, input validation), `naming` (names for limits, events and permissions), `api` (error shapes, status codes), `testing` (how the tests below are written), `structure` (where the admin build and CLI live).

## When to use

- Adding an endpoint that **creates rows, sends email, checks a password or code, or is reachable signed out**: see rate limits and CAPTCHA.
- Adding **logging, an error path, a metric, a health check or a background job**: see observability.
- Adding **a dashboard tile, an alert or an outbound webhook**: see monitoring.
- Adding **anything staff can see or do** about other users: see admin.
- Adding **a table or column holding personal data**, a retention rule, export or erasure: see privacy.
- Adding **an email template**, or anything that triggers mail: see email.
- Adding **an environment variable or secret**: see configuration.
- **Preparing a release, a migration, the first deploy or scaling out**: see deployment.
- **Something is on fire**: see incident response.
- Changing **the frontend build, bundler or container config, page/asset serving, CSP hashing, the admin bundle or the dev proxy**: see web delivery.

## 1. Rate limits and quotas

Four layers:

- a **budget** on every signed-in request (applied automatically by the session middleware);
- **feature limits** on routes that create rows, send mail or check secrets;
- **quotas** on how many rows can exist;
- **concurrency caps** on CPU-heavy work such as password hashing.

Page floods belong to the edge.

- **MUST** name limits `<feature>:<counted-by>` (`auth:per-ip`, `project:per-user`, `register:global`) and declare them in one registry, checked by a test.
- **MUST** count signed-in traffic by **user**, signed-out traffic by **IP** (IPv6 by its **/64**), mail triggers by **email**, and floods by `global`. NEVER count signed-in users by IP, because one office would lock itself out.
- **MUST** count **after** the CSRF/Origin check (so other sites can't spend a victim's budget) and **before** any side effect (so a 428/429 changed nothing). Per-subject limits come before shared ones, and a request stops at the first refusal.
- **MUST** count email-keyed limits for every address alike, registered or not, so a 429 never reveals an account.
- **MUST** write levels as `Above(n)` or `Never`, never a bare number. The challenge level (428, CAPTCHA) is always below the block level (429, or 503 "busy" for shared limits). Unsolved requests must never push a shared limit to block.
- **ALWAYS** send `Retry-After` (time left in the window). Record **one** `rate_limit.reached` event per limit per level per window, and increment a metric for **every** refusal.
- **Metered by default.** Opting a route out needs a written reason (recovery routes, self-reporting endpoints) and its own limits.
- **Quotas** are checked inside the insert's transaction **under a row lock** on the parent and answered with **422** and no field.
- Store security counters in the database or Redis (exact across servers). Keep in-memory counters for reads only. Key them `<name>:<sha256(subject)>`, so the table holds no personal data.

→ Read `references/rate-limits-and-quotas.md` before adding or changing any limit or quota. It has the full order, response shapes, storage, starting values and a step-by-step recipe.

## 2. CAPTCHA and bot defence

CAPTCHA is a pressure valve on a rate limit, not a gate on every form.

- **MUST** challenge only above a limit's challenge level, below its block level, and only on routes that opt in with an action (login, sign-up, reset request, invitation).
- **NEVER** challenge a page with a token in its URL (reset, invite, block links), because the third-party script would read it. Never challenge GETs, the admin door or the machine API.
- **MUST** verify server-side:
  - check `success`, that `hostname` equals your origin's host, and that `action` equals the route's action;
  - use a 5 s timeout, no redirects, and a capped response read;
  - verify one token per request, before any side effect.
- **MUST fail closed:** while the provider is unreachable, requests above the challenge level get 503. Below the level, the provider is never contacted.
- **Provider-agnostic interface** (Turnstile, hCaptcha, reCAPTCHA): `siteKey`, `verify(token, action, ip)`, `cspOrigins`. Load the widget lazily on a 428 only.
- **Production MUST refuse published test keys.** `CAPTCHA_PROVIDER` must be set explicitly, to a provider or `off`.
- Floods, scraping and page abuse go to the edge (WAF, bot rules). Lock the origin to the CDN, and exclude the machine API from challenges.

→ Read `references/captcha-and-bot-defence.md` when wiring a provider, a 428 flow, the client widget or edge bot rules.

## 3. Observability

Never let observability slow a request, never write one row per request to the database, and never log what a request carried.

- **MUST** use structured logs: JSON in production, one handler installed first in every entrypoint, `info` by default, written to stdout.
- **NEVER** log paths, query strings, headers, cookies or bodies. Log the **route pattern** (`GET /api/projects/{project}`), public IDs and scrubbed errors. Redaction by key name is a backstop, not permission.
- **MUST** scrub error text before logging or storing it: emails, IPs, tokens, IDs, quoted values, numbers and URLs become placeholders. **NEVER** log a raw error object or SQL text.
- **MUST** generate your own request ID at the outermost middleware (never adopt a client's or proxy's), return it in `X-Request-ID`, put it on every log line and event, and show it in every 500 body.
- **MUST** recover panics/exceptions into a generic 500 with the ID, dropping what the handler set. If the response already started, cut the connection instead. A client that went away is not an error.
- **Group errors** by fingerprint (source + route + scrubbed message) in a bounded in-memory collector, flushed every 10 s. A new occurrence reopens a resolved group. Browser errors arrive signed-in only, with their own limits, and can never evict server errors.
- **Metrics** use **fixed label sets** (route pattern, limit name, status class: never IDs, IPs or paths) and **series caps** (100 per metric, 300 routes, overflow to `unmatched`). Store per-minute aggregates, rolled up hourly by one leader. `/metrics` goes on a private listener with a token.
- **Slow queries** are logged with operation, table, duration and caller, **never the SQL text**.
- **Liveness** never touches the database. **Readiness** does, and returns 503 while draining.
- **Graceful shutdown order:** readiness 503 → keep serving `SHUTDOWN_DELAY` → drain → final writes (`server.stopped`, last metrics, error flush last). Stop timeout = delay + 20 s.
- **ALWAYS** record `server.started` (with version) and `server.stopped`. A crash loop shows as starts without stops.

→ Read `references/observability.md` when touching logging, error handling, metrics, health checks or shutdown.

## 4. Monitoring and alerting

- **MUST** define each signal **once** (name, unit, window, warn and critical, minimum sample, measuring query, drill-down) and read that definition from both the dashboard and the alert rule, so they can't disagree.
- **MUST** damp alerts:
  - a hold duration (`for`) before opening;
  - one open alert per signal;
  - a 1 h cooldown that makes repeats quiet;
  - per-signal (3/h) and global (5 per 10 min, then digest) email caps.
  - Assume someone is raising signals on purpose.
- **Lifecycle:** opened → escalated (in place) → recovered (confirmed over 10 minutes, still open) → resolved **by a human only**; relapse and recurrence reopen. Critical alerts get reminders every 4 h until acknowledged. **NEVER auto-resolve.**
- **MUST** have an **external uptime check** (2+ regions, plus TLS expiry) **and** a **heartbeat** to a dead man's switch sent after each successful evaluation. A dashboard can't report its own outage.
- **One evaluator** across N servers via a non-blocking advisory-lock leader, with unique indexes as a backstop.
- **Receivers are opt-in** (no default recipients, and an empty selection means nothing). Notifications carry **fixed text and numbers only**: no links, emails, IPs, names or IDs.
- **Webhooks are signed:** `t=<ts>,v1=HMAC(key, "t.body")`, a key ID, and a stable delivery ID, with a 15-minute dual-signature overlap on rotation.
- **MUST** guard every outbound request against **SSRF:** https only, no userinfo, the resolved IP checked at dial time (private, loopback, link-local, CGNAT and metadata ranges blocked), no redirects, short timeouts, a 4 KiB read.
- **Integrity checks** ("things that should never be true": an ownerless tenant, over quota, stalled jobs, unknown event names, servers out of step) run hourly and fail from one row.
- **Time budgets:** 5 s per page and 2 s per section. A slow part is "unavailable", never shown as OK. Move slow signals to rollups.

→ Read `references/monitoring-and-alerting.md` when adding a signal, alert, dashboard page, webhook or any outbound HTTP call. It ends with a signal catalogue to copy.

## 5. Admin and privileged actions

Every capability a stolen admin session has, an attacker has.

- **The layered door:**
  - a **hidden path key** (it hides, it doesn't authenticate; every miss is the ordinary 404);
  - an optional IP allowlist or zero-trust proxy;
  - a **separate admin session** (`SameSite=Strict`, 15 min idle / 8 h max, bound to the key);
  - **password + MFA at every admin sign-in**;
  - a **step-up window** (5 min, a fresh code) and a **written reason** (10–500 chars, no control or bidi characters) for every write.
- **Roles are granted out of band** (CLI only, never UI or API). The default is a read-only `viewer`; `operator` acts. Enforce roles at route registration: an unwrapped mutation refuses to start.
- **NEVER act on yourself or on another admin** (except signing them out). Suspending, resetting MFA for or revoking an admin is CLI only.
- **MUST audit every read**, not just writes. Write the action's audit event in the **same transaction** as the change. Name the admin in the payload, with scope columns empty, so deleting the subject doesn't delete the record.
- **Fail closed** everywhere except the IP block list, which fails open on purpose so availability doesn't depend on it.
- **Key rotation with a previous slot:** the previous key keeps existing sessions only and never opens sign-in. A leaked key is replaced outright.
- **Bounded blunt tools:** every block expires and has a maximum width, bulk retries are capped, exports are capped and defused against CSV injection, and expensive reads have deadlines. **Deliberate omissions** (no impersonation, no editing user data, no console deletion) are written down.
- **Ship admin code in a separate build**, never in the public bundle. Enforce that in the build.

→ Read `references/admin-and-privileged-actions.md` before building any staff-facing page, action, CLI command or role.

## 6. Privacy and compliance

- **MUST** keep a **data map**: one row per table with its personal data, purpose, retention and who sees it. A **test fails** when a table is missing from it.
- **MUST** make every retention age a **registered prune target**, scheduled daily. Each run records `prune.completed` as evidence, and monitoring warns when a target is overdue or not running.
- **Tiers:** audit events 1 year; other events 90 days; logs 30 days; immutable audit log lines 1 year; ephemeral rows until they expire.
- **Minimise:** truncate IPs to /24 or /48 after 30 days, and replace emails with **keyed HMAC pseudonyms** (not plain hashes, which can be reversed by hashing guesses). Store counts over names.
- **Erasure** runs in one transaction: delete the account, cascade its events, redact its email in surviving events through indexed updates, and refuse when it would orphan a shared tenant. **Export** is synchronous JSON built from **per-section field allowlists**; admin activity and address-only events are excluded. Write the **known gaps** down.
- **Opt-in notifications only. Cookies strictly necessary**, so no consent banner, until someone adds analytics.
- **Two database roles:** the runtime role has no DDL, and immutable off-host audit lines are the tamper evidence.
- Map controls to GDPR, SOC 2 and ISO 27001 for auditors. Never claim certification.

→ Read `references/privacy-and-compliance.md` when adding a table, a column with personal data, a retention rule, an export or erasure path, a cookie or a third-party script.

## 7. Email delivery

- **MUST** send from **background listeners** on recorded events, never in the request handler. Retry with backoff, and design for harmless duplicates (a fresh link replaces the old one).
- **MUST** rate-limit **the request that triggers mail** (`<feature>:per-email`, the same for every address), **never the send**. Security mail is never limited. Notifications are opt-in.
- **MUST** use an **autoescaping** HTML template engine. Never mark data as safe. Subjects are **fixed strings in code**. Recipients are bare addresses with **no CR/LF**. No header is built from user data.
- **Bodies:** mail a stranger can trigger contains **nothing the requester chose** (a code only). Use one call-to-action link built from the configured origin, never the Host header.
- **Block list:** mail that can reach non-users carries a block link and `List-Unsubscribe`. Every send checks the block list first, and the console can unblock but never block.
- **HTML and plain text** for every email. **No remote images** (they act as tracking pixels): inline `cid:` PNGs only. **Render every template at startup**; a broken one stops the server.
- Verify the sending domain with SPF, DKIM and DMARC before the first email.

→ Read `references/email-delivery.md` before adding a template or anything that causes an email.

## 8. Configuration and secrets

- **MUST refuse to start** when production is insecure: no HTTPS origin, unverified database TLS, an example or test key, a dev-only switch on, a contradictory pair, or a critical feature neither explicitly on nor explicitly `off`.
- **Secrets come only from a secret store.** Never put them in the image, repository, bundle, logs, errors or config pages. Error messages name the setting, never the value.
- **Summaries say "set / not set":** booleans for secrets, built field by field (never by reflection), and tested against planted secrets.
- **Both-or-neither:** `CLIENT_IP_HEADER` + `TRUSTED_PROXIES`, SMTP username + password, CAPTCHA site + secret key.
- **Client IP:** believe proxy headers only from trusted proxies, read `X-Forwarded-For` from the right, refuse `0.0.0.0/0`, and resolve once per request.
- **Back the master key up with the database**, use the same key on every server, and derive purpose keys with HKDF. Design rotation in (key IDs on ciphertexts) or list its absence as a known gap.

→ Read `references/configuration-and-secrets.md` when adding any setting or secret, putting the app behind a proxy, or planning key rotation.

## 9. Deployment

- **One immutable image** with every entrypoint (`api`, `migrate`, `prune`, `admin`). It runs as non-root, holds no secrets, and **never migrates on startup**.
- **Release order:** run migrations, **then** roll the code. Every migration must work with the old code still running (**expand/contract**). Never rename or drop in the release that stops using something. **Fix forward**; roll back code, not schema. Migrations are append-only once real data exists.
- **Two DB roles** (owner for migrate, runtime without DDL), with the database unreachable from the internet.
- **N instances, no sticky sessions:** all shared state in the database or a shared store, with once-per-fleet jobs behind advisory-lock leaders.
- **Proxy/CDN:** the app sits on a private interface, the origin is locked to the CDN, secrets are kept out of proxy logs, and nothing private sits in a shared cache.
- **Health:** readiness for the load balancer, liveness for the orchestrator. **Non-blocking log driver.** Stop timeout = delay + 20 s.
- **Destructive commands preview by default** and act only with `--confirm`. Dangerous ones are refused in production.

→ Read `references/deployment.md` for the release procedure, the role SQL, the shared-state table and the full security checklist.

## 10. Incident response

**Detect → triage → contain → scope → notify → record → review.**

- **Note the time you became aware.** GDPR gives you **72 h** from then to notify the authority, and "without undue delay" for high-risk notices to people. Templates are in the reference.
- **Contain** with pre-built tools: suspend, sign out everywhere, revoke all sessions, IP block, rotate keys, roll back code.
- **Preserve evidence:** pause prune and minimisation jobs, export events before retention catches up, and trust off-host audit lines if the database may be altered.
- Every breach goes in the **breach register**, notified or not. Hold a **blameless review** within two weeks.

→ Read `references/incident-response.md` when on call, when writing the project's runbook, or when deciding which containment tools to build before launch.

## 11. Web delivery

- **Prerender only session-free public pages**; everything else is one SPA shell. **Embed the build in the server** (or serve it from the same origin) so UI and API ship as one version.
- **Cache per page type:** hashed assets `public, max-age=31536000, immutable`; prerendered HTML `no-cache`; the SPA shell and the API `no-store`; unknown pages get the shell with 404.
- **CSP hashes come from the built HTML, per file, once**, and a file that can't be parsed **fails the build or startup**.
- **The admin UI is a separate bundle**, and the app build fails on a leak through three guards: lint, a bundler plugin, and a grep of the output.
- Build-time-only modules carry a marker that keeps them out of browser bundles. The dev proxy keeps `Origin` unchanged, and dev servers bind to 127.0.0.1.

→ Read `references/web-delivery.md` when changing the frontend build, the bundler or container config, how pages and assets are served, CSP hashing, the admin bundle or the dev proxy.

## Production-readiness checklist

**Abuse**

- [ ] Every signed-in route is metered by default. Each unmetered route has a written reason and its own limits.
- [ ] Every route that creates rows, sends mail or checks a secret has a `<feature>:<counted-by>` limit, counted after CSRF and before side effects.
- [ ] Email-keyed limits treat every address alike. No response or timing reveals whether an account exists.
- [ ] Every unbounded table has a quota checked under a row lock (422).
- [ ] 429/503 carry `Retry-After`. `rate_limit.reached` is written once per window, and a refusal metric is counted.
- [ ] CAPTCHA is wired with server-side verification (hostname + action), fails closed, never appears on token pages, and production refuses test keys.
- [ ] Edge protection covers page floods. The origin is locked to the CDN, and the machine API is excluded from challenges.

**Visibility**

- [ ] JSON logs on stdout with no paths, queries, headers, bodies or SQL. Errors are scrubbed. Request IDs appear on every line, event and 500.
- [ ] Panics are recovered. Error groups are bounded and pruned. Browser errors are limited and labelled.
- [ ] Metrics have fixed labels and series caps. `/metrics` is private and token-protected.
- [ ] Liveness and readiness are separate, the shutdown order is tested, and the stop timeout is set.
- [ ] `server.started` and `server.stopped` are recorded. The config summary holds no secrets.

**Alerting**

- [ ] Signals are defined once and shared by the dashboard and alerts, with min samples and drill-downs.
- [ ] Alerts are damped (hold, cooldown, caps, digest), resolved only by humans, and contain no PII or links.
- [ ] An external uptime check runs from 2+ regions, plus a TLS expiry check and a heartbeat to a dead man's switch. All have been tested by actually stopping the servers.
- [ ] Webhooks are signed. Outbound requests pass the SSRF guard. Integrity checks run hourly.
- [ ] At least one person is subscribed to critical alerts.

**Staff access**

- [ ] The admin console sits behind a hidden key, a separate session, MFA, step-up and a reason. Roles are granted by CLI only.
- [ ] Every admin read and write is audited in the same transaction. Admins can't act on themselves or other admins.
- [ ] Admin code is absent from the public bundle. Admin paths are out of proxy logs and shared caches.
- [ ] Deliberate omissions are documented. The admin list is reviewed quarterly.

**Data**

- [ ] The data map covers every table, enforced by a test. Processors and cookies are listed.
- [ ] Every retention age is a prune target, scheduled daily, alerting on failure, with `prune.completed` visible.
- [ ] IPs are truncated and emails pseudonymised (keyed HMAC) at 30 days.
- [ ] Erasure and export work, use field allowlists, and are tested for leaks. Known gaps are written down.
- [ ] Audit lines are shipped to immutable storage for a year. Other logs are kept 30 days with restricted access.

**Email**

- [ ] Mail is sent from listeners with retries. Limits apply to triggers, never sends. Security mail is never limited.
- [ ] Templates autoescape, subjects are fixed, recipients are CR/LF-checked, there are no remote images, both parts exist, and templates are rendered at startup.
- [ ] Block list and `List-Unsubscribe` are in place on mail to non-users. SPF, DKIM and DMARC are published.

**Configuration and deploy**

- [ ] Production refuses insecure config. Secrets come from the store only, are identical across servers, and the master key is backed up with the database.
- [ ] `CLIENT_IP_HEADER` + `TRUSTED_PROXIES` are correct for the actual proxy chain.
- [ ] Migrations run before code and follow expand/contract. They are append-only, and the team fixes forward.
- [ ] Two DB roles. The database is private and uses verified TLS.
- [ ] Multiple instances work without sticky sessions. Once-per-fleet jobs are leader-elected.
- [ ] Destructive commands preview by default. Dev-only commands are refused in production.
- [ ] The web build is embedded (or same-origin). Cache headers follow the page type. CSP hashes fail closed. The app build's admin-leak guards pass, in the container build too.

**People**

- [ ] The incident runbook has been filled in for this deployment and rehearsed. A breach register exists. Contacts are current.
- [ ] The privacy notice names processors, retention and how to request export or erasure. Policies are reviewed and dated.
