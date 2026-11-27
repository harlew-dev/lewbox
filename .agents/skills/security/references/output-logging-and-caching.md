# Injection, output, redirects, logging and caching

Read with `SKILL.md` sections 11 to 14.

## SQL

```
-- RIGHT: bound parameter
db.select().from("projects").where("tenant_id = ? AND name ILIKE ?", tenantId, "%" + escapeLike(q) + "%")

-- WRONG: interpolation, even of "safe" values
db.raw("SELECT * FROM projects WHERE name = '" + name + "'")
db.raw(`ORDER BY ${req.query.sort}`)
```

- Values always go through placeholders, via the query builder. Raw SQL strings are reserved for statements the builder can't express (advisory locks, schema DDL) and migrations, and even those take placeholders.
- **Identifiers can't be parameters.** Map input to a fixed set: `sorts = {"name": "name ASC, id ASC", "-created": "created_at DESC, id DESC"}`; unknown key = 400.
- `LIKE`/`ILIKE` search escapes `%`, `_` and the escape character, so a search for `%` matches a literal percent sign and can't force a full scan of everything.
- Integer and ID inputs are parsed and shape-checked before reaching the query.

## HTML and templates

| Stack | Autoescaped (use) | "Trust me" sink (never with user data) |
| --- | --- | --- |
| React / Solid | `{value}` | `dangerouslySetInnerHTML`, `innerHTML` ref |
| Vue | `{{ value }}` | `v-html` |
| Svelte | `{value}` | `{@html}` |
| Go | `html/template` | `template.HTML`, `template.URL`, `text/template` for HTML |
| Laravel / Blade | `{{ $value }}` | `{!! $value !!}` |
| Django / Jinja | `{{ value }}` | `\|safe`, `mark_safe`, `Markup` |

- Never cast to "safe" to fix a rendering glitch. If you need rich text, sanitise with an allowlist sanitiser on the server, store the sanitised form, and test hostile input.
- `href`/`src` from data: allow only relative paths or `https:` URLs you validated; `javascript:` and `data:` URLs execute.
- Don't build HTML strings by concatenation anywhere (including `insertAdjacentHTML`, `document.write`).
- Names other users will see: reject control, invisible and bidirectional formatting characters (they spoof names and reorder text).

## Email

- Render HTML from autoescaping templates; escape every user-supplied name (tenant name, inviter name).
- **No user data in mail headers.** Subject and From are fixed strings; header injection via newlines in a name is a real attack, and a user-chosen subject makes your domain send their phishing.
- Show the actor's address (the inviter's email) in the body, so a misleading tenant name can be judged.
- Send from a background job, never the request handler.

## Links and redirects

```
# build links from config, never the request
resetUrl = config.appOrigin + "/auth/forgot-password/" + token     # RIGHT
resetUrl = "https://" + req.host + "/auth/forgot-password/" + token # WRONG: Host header is attacker-controlled
```

### `?next=` allowlist

```
func returnPath(next):
  if !next or !next.startsWith("/") or next.startsWith("//") or next.startsWith("/\\"): return DEFAULT
  url = parseUrl(next, base = config.appOrigin)
  if url.origin != config.appOrigin: return DEFAULT
  if !returnRoutes.any(pattern -> pattern.matches(url.pathname)): return DEFAULT  # well-formed IDs only
  return url.pathname + url.search
```

- `returnRoutes` lists signed-in page patterns (with ID shapes like `tnt_[0-9A-Za-z]{14}`), plus explicitly named exceptions (an invitation page with the exact token shape). It excludes `/auth/*`, `/api/*`, onboarding and anything else that could loop or act.
- **Add a pattern for each new signed-in page**, or users lose their place after sign-in.
- `next` is used only for client-side navigation; it never selects an action or submits a form.
- Login's "Sign up" link and the sign-up page's "Sign in" link carry the same validated `next`.

### Displaying URL content

- Map `?notice=<key>` to fixed strings in code. Unknown keys render nothing.
- Never render query text as a message, a heading, a name or a prefilled sensitive field. Pass a typed email between auth pages in navigation state, not the URL.

## Logging

- **Never log**: request path, query string, headers, cookies, body, form values. Link tokens live in paths, CSRF tokens and keys in headers, emails and searches in queries and bodies.
- The **access line** has: method, **matched route pattern** (`/api/tenants/{tenant}/projects/{project}`) or a fixed page label (`page /auth/*`), status, duration, request ID. Never the raw path.
- Log through **one handler** installed for every command, with the request context so every line carries the request ID.
- **Errors:** log via a helper that scrubs (emails, tokens, long digit runs, credential prefixes) before writing: `log.error("save failed", errorAttr(err))`, never `log.error("save failed", "error", err)`. Stored errors are scrubbed the same way.
- Scrubbing is a backstop. Never build an error, exception or panic message from a secret, a password, or a typed value.
- **Panics/uncaught exceptions** are recovered into the generic 500; their value is logged only through the scrubbing helper.
- **Request IDs** are server-generated (`req_` + random); an incoming `X-Request-ID` is ignored (it would let a client forge log correlation).
- **500 body:** `{"error": "Something went wrong. Please try again.", "requestId": "req_..."}`. The detail is in the log under that ID.
- Slow-query logs record timing and the statement shape, never values.
- **Client error reports** send the route pattern, never `location.href`; strip quoted JSON from parse-error messages; scrub on receipt; any page showing them labels them user-reported and never links them.
- Metric labels and similar come only from fixed sets, never from request values (`ops`).

## Caching user data

| Place | Rule |
| --- | --- |
| Server module-level / global variables | **Never** user or tenant data. They're shared across every request and user |
| Server-rendered / prerendered HTML | Built without a session; contains no user data. Signed-in content renders client-side or in a `no-store` per-user response |
| HTTP caches, CDN | API and app shell `no-store`; authenticated responses never cacheable by shared caches |
| Client data cache (SWR, TanStack Query, Apollo, Pinia store) | Key includes the user ID: `[path, userId]`. Cleared entirely on sign-out and account deletion (navigate away first, then clear) |
| Client module-level variables | Never the session, CSRF token or user data; read them from context/providers |
| Browser storage (`localStorage`, `sessionStorage`, IndexedDB) | Never tokens or secrets; avoid personal data |
| One-time secrets | Component state of the dialog that shows them, nowhere else |

After a destructive action, clear the deleted record's cache entry without refetching (so Back shows "not found", not stale data) and revalidate the lists that contained it.
