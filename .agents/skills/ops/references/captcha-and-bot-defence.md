# CAPTCHA and bot defence

A CAPTCHA is a **pressure valve on a rate limit**, not a gate on every form. Normal traffic never sees one. When a limit is under pressure, real people are asked to prove they are human, which is usually invisible, instead of being refused. Bots are slowed without locking everyone else out.

## Where challenges sit

- **Below the block level.** Each limit's `challenge` level (`Above(n)`) is lower than its `block` level (see `rate-limits-and-quotas.md`). Example: `auth:per-ip` challenges above 5 attempts in 15 minutes and blocks above 60.
- **The decision depends only on the IP and the counts**, never on whether an email has an account. Otherwise the challenge itself reveals which accounts exist. A per-email count (e.g. a challenge level on `login:per-email`) qualifies only because every typed address is counted alike.
- **Only on routes that opt in** by naming an action, and only on routes whose page has **no token in its URL**:

| Challenge | Never challenge |
| --- | --- |
| login, sign-up (and "resend code", if its page holds a cookie rather than a URL token), password-reset **request**, sending an invitation (for its global limit) | the reset-link page and its submit, the invitation-accept page, email-block links: their URL holds a secret token that the provider's third-party script would be able to read |
| | signed-in password, MFA and deletion routes: already behind a session, `password-check:per-user` and `mfa:per-account` |
| | the admin door, every GET, the machine API (`/api/v1/*`): scripts can't solve challenges, so give them block-only limits |

Once the widget script is loaded on one page, it stays loaded in an SPA until a full reload. So **no token page may trigger loading it**, and a route that loads it must not navigate client-side to a token page expecting a clean context.

**The token-URL guard (MUST).** Challengeable pages can still carry a token: sign-in and sign-up run at `/auth/login?next=/auth/invitations/<token>`. A previous implementation loaded the provider's script there, with the live token in `location.href`. Three layers:

1. **Server, routes:** token pages and their API routes are never challengeable.
2. **Server, HTML:** when a page's path **or query** (decoded, including `?next=`) contains a token-page path, omit the provider's origins from that response's CSP, so the browser can't load the script even if the client is wrong.
3. **Client guard:** before loading the script on a 428, `mayLoadChallengeScript(location)` refuses if the path or any query value (URL-decoded until stable) matches a token-page pattern, or if this SPA session has shown a token page. On refusal, `location.replace()` to the clean page with a fixed notice ("Complete the check, then open your link again"). Never park the token in storage or history state: the script can read those too.

Test all three: the guard against token paths and encoded/double-encoded `?next=` values; an end-to-end test that a 428 on a `?next=<token page>` URL makes no request to the provider's origin; the CSP of such a page lacks the provider. The `auth` skill's known-pitfalls reference has the background.

## The flow

```
POST /api/auth/login            → 428 {"error":"Complete the check to continue.",
                                       "challenge":{"provider":"turnstile","siteKey":"0x4AAA…","action":"login"}}
(browser lazy-loads the provider script, renders the widget, gets a token)
POST /api/auth/login            → 200 / 401 / 429
X-Challenge-Token: <token>
```

- The site key is public and comes in the 428, so the web build needs no CAPTCHA setting at build time.
- The browser resends **once** with the token in a header (`X-Challenge-Token`). If the token is invalid, expired, reused or oversized (over 2,048 bytes), the answer is another 428, "The check failed. Try again."
- **One token per request:** verify it once and memoise the result for that request. A sign-up past both `auth:per-ip`'s and `register:global`'s challenge levels spends one token, not two.
- **Every check runs before any side effect** (hashing, inserts, queuing mail), so a 428 changed nothing and is safe to resend. The resend counts against the limits like any other request: a challenged login costs two attempts.
- **Above the block level a valid token doesn't help:** 429, or 503 for a shared limit.
- **Unsolved requests never push a shared limit to its block level.** Only requests that passed a challenge count toward a global limit's block level, in a `<name>:passed` counter. Once that level is reached, an unsolved request gets the 503 immediately, without a check it couldn't use.

## Server-side verification (MUST)

Client-side widgets prove nothing on their own. **ALWAYS** verify the token server-side:

1. POST `secret`, `response` (the token) and `remoteip` (the resolved client IP) to the provider's verify endpoint.
2. **Hardened HTTP client:** a 5 s timeout, read at most 64 KiB of the response, and **never follow redirects** (treat a redirect as "unavailable"), so the secret can't be sent anywhere else. Don't use a proxy from the environment.
3. **Require:**
   - `success == true`;
   - `hostname` equal to your app origin's host, so a token solved on someone else's site using your public site key is refused;
   - `action` equal to the route's action, where the provider supports it, so a token solved on sign-up can't be spent on login.
   - Single use and expiry (Turnstile: 300 s) are enforced by the provider. You need no table for them.
4. **Classify the provider's error codes:**

| Codes | Meaning | Answer | Log |
| --- | --- | --- | --- |
| `missing-input-secret`, `invalid-input-secret`, `bad-request`, `internal-error`, network failure, timeout, redirect | your configuration or the provider's outage | **503** "Verification is unavailable. Try again shortly." | error |
| `timeout-or-duplicate`, `invalid-input-response`, any other | about the token | **428** "The check failed. Try again." | warn |

5. Log the error codes and nothing else: **never the token or the secret**. If your log redaction blanks keys containing `code`, log them under a key such as `siteverifyErrors` that won't be redacted.
6. **Fail closed:** while the provider can't be reached, every request above a challenge level gets 503. Below the challenge level the provider is never contacted, so its outage doesn't touch normal traffic.

## Provider-agnostic design

Handlers, limits, the 428 shape and the client dialog never mention a vendor. One small interface does:

```
interface CaptchaProvider {
  name(): string                       // "turnstile" | "hcaptcha" | "recaptcha"
  siteKey(): string                    // public, sent in the 428
  verify(ctx, token, action, ip): ok | Rejected | Unavailable
  cspOrigins(): { scriptSrc: [...], frameSrc: [...], connectSrc: [...] }
}
```

Adding a provider means one server implementation, one case in the factory and the `CAPTCHA_PROVIDER` setting, and one client widget adapter (load script, render, return token). Nothing else changes. Tests use a fake provider.

| | Cloudflare Turnstile | hCaptcha | Google reCAPTCHA |
| --- | --- | --- | --- |
| Verify endpoint | `https://challenges.cloudflare.com/turnstile/v0/siteverify` | `https://api.hcaptcha.com/siteverify` | `https://www.google.com/recaptcha/api/siteverify` (Enterprise uses a separate assessments API) |
| Action binding | `action` in the response | none in the free tier: rely on hostname plus separate site keys per form if needed | v3: `action` plus a `score` (refuse below about 0.5); v2: none |
| Script / CSP | `challenges.cloudflare.com` (script-src, frame-src) | `hcaptcha.com`, `*.hcaptcha.com` (script, frame, style, connect) | `www.google.com`, `www.gstatic.com` (or `www.recaptcha.net`) |
| Published test keys | site `1x…`, `2x…`, `3x…`; secret `1x…AA` passes, `2x…AA` fails | site `10000000-ffff-ffff-ffff-000000000001`, secret `0x000…000` | v2 test pair beginning `6LeIxAcTAAAAA…` (always passes, shows a warning) |
| Privacy | no cookies for tracking; usually no consent needed | privacy-oriented | Google processes visitor data: disclose it, and check consent rules in your region |

Re-check these values against each provider's current documentation when implementing. They change.

## Test keys refused in production

- Outside production, default to the provider's always-passing test keys, so local development and e2e tests never call the real service. At startup, log what that means (`captcha: test keys, every check passes`, or `… every check fails` for the blocking keys).
- **Production MUST refuse to start** with any published test site key or secret (match their known patterns), and with the example file's placeholder values.
- Test-key responses can't satisfy your real checks (Turnstile's name `example.com` and carry no action). Accept them as they are **only when your own configured secret is a test secret**. A testing-mode answer to a real secret gets the normal checks and fails.
- `CAPTCHA_PROVIDER` must be set explicitly in production: a provider, or `off` on purpose. With `off`, challenge levels are skipped (still recorded as events) and block levels apply, with a startup warning. The admin config page and the configuration warnings list "CAPTCHA off in production".

## CSP and third-party exposure

- The CAPTCHA provider is usually the **only** third-party script the app allows. Add its origins to the app pages' CSP **only while a provider is configured**, and never to the admin build.
- Load the script **lazily, only when a 428 arrives**, not on every page load. That keeps the third party off every page and keeps it from seeing ordinary traffic.
- List the provider as a processor in the privacy notice (see `privacy-and-compliance.md`).

## Bot defence beyond CAPTCHA

- **The edge first.** Page floods, scraping and volumetric attacks belong to the CDN/WAF (bot management, managed challenges, rate rules). The app's limits are the layer that works everywhere; the edge is cheaper per request.
- **Lock the origin to the CDN** (firewall to its IP ranges, or a tunnel), or attackers bypass the edge by hitting the server's own address.
- **Exclude the machine API from browser challenges** at the edge (e.g. match `/api/` but not `/api/v1/`). Protect it with keys, block-only limits and edge rules that count and block, never challenge.
- **IP blocks** are an admin tool for a few abusive addresses: bounded width and lifetime (see `admin-and-privileged-actions.md`). For floods, block at the edge.
- **Future option:** a listener on `rate_limit.reached` for `:global:challenge` could enable a pre-made, disabled edge rule for the route under attack, with an API token scoped to only that zone's WAF.

## Metrics and tests

- Every 428 (and every 503 while the provider is unreachable) counts in `rate_limit_refused_total{limit="<name>:challenge"}`. Every 429 or 503 above a block level counts as `<name>:block`.
- **Tests:**
  - below the level, nothing is challenged and the provider is never called;
  - above it, 428 with the right action;
  - a valid token passes and is verified once per request;
  - an invalid, expired or oversized token gets 428 again;
  - provider down gets 503 (fail closed);
  - a wrong hostname or action is rejected;
  - a shared limit can't be blocked by unsolved requests;
  - a shared-limit route without an action gets 503 plus an error log;
  - the provider's error codes survive log redaction while the token and secret never appear;
  - production refuses test keys;
  - no page whose URL (path or `?next=`) holds a token can load the provider script (the token-URL guard above).
