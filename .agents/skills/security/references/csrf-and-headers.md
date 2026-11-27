# CSRF, Origin, headers, CSP and request caps

Read with `SKILL.md` sections 4 and 5.

## Why four checks, not one

- **SameSite cookies** stop most cross-site requests from carrying the session, but not same-site attackers (a sibling subdomain), older browsers, or top-level `Lax` navigations.
- **Exact Origin** stops cross-origin form posts and fetches from any site, including subdomains. Exact means string equality with the configured origin (`https://app.example.com`), not "ends with", not a regex, not the request's Host.
- **`Sec-Fetch-Site: cross-site`** is the browser's own statement; refusing it costs nothing and catches misconfigured origins.
- **JSON content type** forces a CORS preflight for any cross-origin fetch; HTML forms cannot send `application/json`. With CORS off, the preflight fails.
- **CSRF token** covers the signed-in case even if one of the above is bypassed (a browser bug, a same-origin HTML injection that can't read the token).

## Middleware (pseudocode)

```
func browserSecurity(req, next):
  if req.method in {GET, HEAD, OPTIONS}: return next(req)

  if req.header("Origin") != config.appOrigin:          return 403 "Forbidden."
  if req.header("Sec-Fetch-Site") == "cross-site":      return 403 "Forbidden."
  if hasBody(req) && mediaType(req.header("Content-Type")) != "application/json":
                                                        return 415
  return next(req)

func requireSession(req, next):
  session = findSession(digest(cookie(req)))            # 401 if missing/expired
  if req.method not in {GET, HEAD, OPTIONS}:
    if !constantTimeEqual(req.header("X-CSRF-Token"), session.csrfToken): return 403
  ctx.session = session
  return next(req)
```

- A DELETE with a body (e.g. `{name, password}` to delete a tenant) needs the JSON content type too.
- `mediaType` parses `application/json; charset=utf-8` correctly; don't string-compare the whole header.
- Order: request ID, panic recovery, timeouts, size caps, security headers, then (by route group) browser security or machine security, then session/credential, then tenant/permission.
- The **route group** selects browser or machine security. Never branch on "does it have an Authorization header": an attacker adds one to skip CSRF.

### Client side

- One API client attaches `X-CSRF-Token` on every mutation from the in-memory session context. Signed-out calls (login, sign-up) send none.
- When the server rotates the session (password change, email change), it returns a new CSRF token and the client replaces its session context, or the next mutation fails.
- Route loaders/server-side data fetchers use a read-only client with no token.
- Nothing else in the codebase calls `fetch` or builds API paths (`frontend`, `structure`).

## Response headers

| Header | API | HTML pages |
| --- | --- | --- |
| `Cache-Control` | `no-store` | `no-store` for the app shell and any page with user data; `no-cache` for session-free prerendered pages; `public, max-age=31536000, immutable` only for content-hashed assets (full table: `ops/references/web-delivery.md` §3) |
| `X-Content-Type-Options` | `nosniff` | `nosniff` |
| `X-Frame-Options` | `DENY` | `DENY` |
| `Referrer-Policy` | `no-referrer` | `strict-origin-when-cross-origin` |
| `Content-Security-Policy` | `default-src 'none'; frame-ancestors 'none'` (optional, JSON only) | see below |
| `Strict-Transport-Security` | production: `max-age=63072000; includeSubDomains` | same |
| `Permissions-Policy` | - | deny what you don't use: `camera=(), microphone=(), geolocation=(), payment=()` |
| `X-Request-ID` | server-generated, every response | same |

## CSP

```
Content-Security-Policy:
  default-src 'self';
  script-src 'self' 'sha256-AbC...=' 'sha256-XyZ...=';
  style-src 'self' 'unsafe-inline';
  img-src 'self' data:;
  font-src 'self';
  connect-src 'self';
  object-src 'none';
  base-uri 'none';
  form-action 'self';
  frame-ancestors 'none'
```

- **Script hashes per HTML file**, computed once from the built HTML, so a new inline script works automatically and an injected one doesn't. How to compute them and fail closed: `ops/references/web-delivery.md` §4.
- **NEVER `'unsafe-inline'` or `'unsafe-eval'` in `script-src`.** If a library needs `eval`, replace the library.
- `style-src 'unsafe-inline'` is a tolerated, documented exception when UI libraries set inline styles (animation and positioning libraries do). Style injection is far less dangerous than script injection; prefer to remove it if your stack allows.
- **No third parties.** Self-host fonts, icons and scripts. A required exception (a CAPTCHA provider, say) adds **one exact origin** to `script-src`/`frame-src`, only while it's configured, only on pages that need it, never on token pages or admin pages, and is written in the docs.
- Test the CSP against the production build (end-to-end), because dev servers usually serve without it.

## Request caps and timeouts

| Cap | Default | Notes |
| --- | --- | --- |
| Body | 16 KiB | Wrap the reader (`http.MaxBytesReader`, `express.json({limit})`, `client_max_body_size`). Raise per route with a reason (uploads go elsewhere) |
| Headers | 16 KiB | Server config (`MaxHeaderBytes`, Node `maxHeaderSize`) |
| Request deadline | 10 s | A context/deadline every DB query and outbound call inherits |
| Server timeouts | read-header, read, write, idle all set | Defaults are often "infinite"; slowloris holds connections open |
| Outbound calls | explicit timeout, no redirects to private addresses | `ops` for SSRF rules on webhooks |
| JSON | reject unknown fields; validate every length | 400 for unknown fields, 422 for invalid values (`api`) |

Every list endpoint has a maximum page size; every free-text field has a maximum length. Nothing a client sends is unbounded.
