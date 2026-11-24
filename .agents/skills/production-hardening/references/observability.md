# Observability

How the server says what it is doing **without ever saying what a request carried**. Three rules:

1. **Nothing slows a request.** Observability has a measured per-request budget (e.g. about 20 µs and 3 allocations for request ID, access line and metrics). A change that exceeds it needs a reason in review.
2. **No request is written to the database.** A request's line goes to the log. The database gets per-minute sums per route, so storage depends on how many routes exist, never on traffic.
3. **Personal data and secrets stay out by construction**, with redaction and scrubbing as a backstop rather than as permission.

## Structured logs

**One logger, installed first.** Every entrypoint (server, migrate, prune, admin CLI) installs the same handler before anything else, reading the log settings first, so even a refused configuration is logged in the requested format.

| Setting | Default | Values |
| --- | --- | --- |
| `LOG_FORMAT` | `json` in production, `text` elsewhere | `json`: one object per line (`time`, `level`, `msg`, then attributes) for any log collector |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `ACCESS_LOG` | `all` | `all`; `errors` (4xx/5xx, requests over 1 s, aborted ones), for log stores that charge by size; `off`. Anything else refuses to start |

**Levels:**

| Level | Use for |
| --- | --- |
| `debug` | health-probe access lines, client-gone aborts |
| `info` | access lines, audit lines, startup summary, `mail sent` |
| `warn` | slow queries, a shared rate limit reached, a provider rejecting a token, a failed metrics write, a dropped error group |
| `error` | 500s, panics, final delivery failures, provider misconfiguration, anything someone must look at |

- **Where:** the server logs to **stdout**. Platform drivers ship it on, and the app sends logs nowhere itself. The framework's own complaints (broken connections) go through the same handler as warnings.
- **Hand-run commands** log to stderr through the same handler but print their own output (previews, results, why they stopped) as plain lines, so `LOG_LEVEL=warn` can't hide why a command failed.
- **Audit lines need `info`.** If the level is above `info`, say so at startup **at `error`**, so no level hides the warning (see "Audit lines" below).

### Never log what a request carried

**NEVER** log a request's path, query string, headers, cookies or body:

- paths hold reset, invitation and block-link tokens and the admin path key;
- query strings hold searches and email addresses;
- headers hold session cookies, CSRF tokens and API keys.

Log the **route pattern**, the record's public ID, or the error instead.

### The access line

One line per request (`msg: "request"`), written once the response is complete:

| Field | Holds |
| --- | --- |
| `requestId` | always the server's own ID |
| `method` | a standard HTTP method, or `OTHER`, so a client can't fill the line with invented ones |
| `route` | **the pattern that matched**, `GET /api/projects/{project}`, or a fixed label: `page /app/*`, `asset`, `page 404`, `unmatched`, `ip blocked`. **Never the path or the query** |
| `status` | the status sent; `0` if the request was aborted before any answer |
| `durationMs` | whole milliseconds (an integer avoids float formatting costs) |
| `bytes` | response body size |
| `user` | the public user ID, `admin` for an admin session, or empty |
| `ip` | the resolved client IP (see `configuration-and-secrets.md`) |
| `proxyRequestId` | only when a trusted proxy sent its own `X-Request-ID` in a safe shape |
| `aborted` | `true` only when the request was cut off |

- Health probes log at `debug`, so they don't fill the log at the default level.
- The IP and user ID are personal data: **keep logs 30 days at most** and restrict who can read them. Audit lines are the exception and are kept a year.
- Both formats must escape what they write, so no value (a route, a proxy's ID) can break a line or forge a new one.

### Redaction: the backstop

The log handler replaces with `[redacted]`:

- the value of any attribute whose key, or any group around it, contains (case-insensitive) `password`, `pass`, `token`, `secret`, `cookie`, `authorization`, `session`, `csrf`, `code`, `otp`, `mfa`, `key`, `digest`, `hash` or `email`, however deeply nested. This is broad on purpose: `bypass` and `passed` get redacted too;
- any value that is a request, a header map, a cookie, a URL or URL query values, whatever its key.

Allow-list the access line's keys, the request IDs and a panic's `panic` and `stack` keys, so redaction can't blank them. A test lists every key the code logs.

Redaction can't catch a secret inside a value logged under an innocent key (an error message, a map). So don't log such values.

## Error scrubbing

Error text routinely contains user data:

- a database type error echoes the value it couldn't parse;
- a unique-violation detail prints `Key (email)=(ada@example.com)`;
- an SMTP refusal names the recipient;
- a JSON parse error quotes the body.

So **ALWAYS** log errors through a helper (e.g. `errorAttr(err)`) that runs the message through `scrub()`, and **NEVER** log a raw error object. Apply the same scrubber to the error-group table.

`scrub(message)` first makes the text one line: control characters become spaces, format characters (bidi, zero-width, soft hyphen) are removed, and invalid UTF-8 is replaced. Then it replaces, **in this order**:

| What | Becomes |
| --- | --- |
| text a JSON parser quotes | `"…"` |
| database detail: `Key (…)=(…)`, `Failing row contains (…)` | `=(…)`, `contains (…)` |
| URLs (`scheme://…`), query strings, fragments | `<url>`, `?<query>`, `#<query>` |
| email addresses (also `%40`, full-width `＠`, quoted local parts) | `<email>` |
| base64/base64url runs: 32+ chars always; 20+ when they mix cases or digits | `<token>` |
| your app's public IDs (`usr_…`, `req_…`), UUIDs | `<id>` |
| IPv4/IPv6 (with port, zone) | `<ip>` |
| quoted strings (straight, curly, guillemets, backquotes) | `"…"` |
| 16+ base32 chars (TOTP secrets); 16+ hex digits (digests); recovery-code shapes | `<token>`, `<hex>` |
| any other number, including non-ASCII digits | `<n>` |

- Token runs go **before** IDs, IPs and numbers. Otherwise a random token that happens to contain an ID-like piece gets split into two halves too short to catch.
- **Keep what says what broke:** names after `constraint`, `relation`, `column`, `index`, `table`, SQLSTATE codes, and identifiers like `int64` or `projects_tenant_id_fkey`.
- **Bounded:** read at most 4,000 bytes and output at most 1,000 characters (16,000 in and 8,000 out for stacks), so hostile input can't make the regexes slow. Test this with adversarial input.
- **Stacks:** keep function names, file names and line numbers only; argument values become `(…)`. For browser stacks keep `at name (file.js:line:col)` without the script's URL path or query, and show `<page>` for inline frames.
- **Browser-supplied text** also loses bare host names and paths, which someone could plant for an admin to follow.
- **Test it:** feed the scrubber every shape above and check none survive. Scrub 20,000 random tokens, keys and codes and check none survive. Check that errors differing only in scrubbed values produce the same fingerprint.
- It's still a backstop: **don't build error messages from what a user typed**.

## Request IDs

- **Generate at the outermost middleware**, so pages, assets and API calls all get one: `req_` plus about 80 bits of `crypto`-random base62.
- **Always yours. NEVER adopt a client's or proxy's `X-Request-ID` as the ID.** Most CDNs and proxies pass a client's own header through unchanged, so an adopted ID could be chosen by an attacker to pin their activity on someone else's log lines and events. Log a trusted proxy's ID separately as `proxyRequestId`, and only if it matches `^[A-Za-z0-9._-]{1,64}$`. Ignore it otherwise.
- **Return it** in `X-Request-ID` on every response.
- **Put it on every log line** written during the request: log through the request context and let the handler add `requestId`. Store it on every event row (validated shape, empty for CLI and background work), so an audit row leads straight to its log lines.
- **Put it in every 500 body:** `{"error": "Something went wrong. Please try again.", "requestId": "req_…"}`. The UI shows "Reference: req_…" so users can quote it to support.

## Errors and panics

- **One internal-error helper** writes the generic 500 with the request ID, logs the scrubbed error at `error` (`msg: "request failed"`, with the route) and reports it to the error collector. The error text **never** reaches the client.
- **A client that went away is not an error.** If the request's own context was cancelled (tab closed), write nothing, mark the request `aborted`, log at `debug`, and report nothing. A **deadline exceeded** (your request timeout, e.g. 10 s) is still a failure, and is also counted in `http_timeouts_total`. On a write, the change may have committed even though the client saw an error.
- **Panic/exception recovery** goes just inside the security-headers middleware, so the 500 keeps its headers:
  - log at `error` (`msg: "request panicked"`, route, value, stack), report it, and answer the generic 500 with the ID;
  - **drop anything the handler set before the panic**, such as a cookie;
  - **if the response had already started** (status written or a flush), you can't send a 500. Cut the connection instead and mark the request `aborted`, so the client never gets a truncated body that looks complete.
- Add a second, outer recovery at the very edge (request-ID middleware) for panics in the middleware itself, so nothing reaches the runtime's default panic log.
- **Panic values are dangerous:** a value formatted before logging bypasses redaction. Log a request, header, cookie or URL as `[redacted]`. Scrub a string or error, then cut it to 512 bytes (scrub first, so a cut can't leave half a token). For anything else, log only its type.
- **Background workers** handle panics and errors the same way: log them scrubbed, store them scrubbed in the delivery's `last_error`, retry, and report once the work has failed for good.

## Grouped errors

Instead of a third-party error tracker (or alongside one, if you scrub before sending), keep an `app_errors` table:

| Column | Holds |
| --- | --- |
| `fingerprint` (unique) | SHA-256 of source + route + **scrubbed** message, so failures differing only in data share a row |
| `source` | `request`, `panic`, `worker`, `background`, `client` |
| `route` | route pattern, listener or job name. Never a path |
| `message`, `stack` | scrubbed; at most 1,000 / 8,000 characters |
| `count`, `first_seen_at`, `last_seen_at` | |
| `last_request_id`, `last_version` | the latest occurrence's request ID and release |
| `resolved_at` | set by an admin; **a new occurrence clears it**, so a regression shows as open |

- **Never in the request's way:** reporting is a map update under a mutex. An in-memory collector flushes every 10 s, **only when there is something**, as one multi-row upsert (`count = count + n`).
- **Bounded:**
  - at most 1,000 groups between flushes, with the least recently seen evicted;
  - at most 100 groups from browsers, and a browser group can only evict another browser group, so users can never push out server errors;
  - dropped occurrences are logged as a count.
- **Database down:** put the groups back in the collector and retry next tick. The log is complete either way.
- **Bad data in one group never blocks the others:**
  - validate against the table's CHECKs first; a NUL byte can't be stored at all;
  - if the batch still fails with a data error, retry each group alone and drop those refused for their data.
- **Prune** groups not seen for 30 days.

### Errors from the browser

`POST /api/client-errors`:

- **Signed in only**, because an anonymous write endpoint is a free way to fill a table.
- It is CSRF-checked, **unmetered by the write budget** (an error loop must not lock the user out of saving), and limited by `client-error:per-user` (30/h) and `client-error:global` (500/h).
- **Body:** at most 4 KiB, with `message`, `stack`, `route` and `release` only. 400 for unknown fields. Answer 202.
- **`route` must be one of the frontend's known route patterns**, or it is stored as `unknown`, so a browser can't mint labels. Keep the server and client route lists equal with a test.
- `release` is kept only if it matches your version format. Otherwise store it empty, so nobody can leave words in it.
- Fingerprint on the first 120 scrubbed characters, so varying the tail of a long message doesn't create new groups.
- **Client side:**
  - report what the root error boundary catches, plus `error` and `unhandledrejection` events. Not 404s, not API errors (the server already has a record of a 5xx), not network failures, not opaque cross-origin `Script error.`;
  - send each distinct error once per page load, at most 10;
  - send the **route pattern, never the URL**;
  - stop sending after a 429 or 503;
  - swallow the reporter's own failures.
- Label these groups **User-reported** in the admin UI, and never render their text as links.

## Metrics

**In-process registry** of counters, gauges and fixed-bucket histograms, recorded with atomic adds and no locks. Recording into an existing series allocates nothing.

| Metric | Kind | Labels |
| --- | --- | --- |
| `http_requests_total` | counter | `route`, `status` (`2xx`, `3xx`, `4xx`, `5xx`, `aborted`) |
| `http_request_duration_ms` | histogram | `route` |
| `http_timeouts_total` | counter | `route` |
| `errors_total` | counter | `source`, `route` |
| `rate_limit_refused_total` | counter | `limit` (`<name>:challenge` / `<name>:block`) |
| `quota_refused_total` | counter | `quota` |
| `password_hash_busy_total` | counter | — |
| `db_query_duration_ms` | histogram | `operation` (`select`, `insert`, `update`, `delete`, `other`) |
| `db_slow_queries_total` | counter | `table` |
| `db_pool_open`, `_in_use`, `_idle`, `_wait_count`, `_wait_ms` | gauges | — |
| `events_recorded_total` | counter | `name` |
| `deliveries_total` | counter | `listener`, `outcome` (`delivered`, `retried`, `failed`) |
| `delivery_lag_ms` | histogram | `listener` |
| `deliveries_pending` | gauge | — |
| runtime: threads/goroutines, heap, memory, GC | gauges | — |
| `build_info` | gauge = 1 | `version`, runtime version |

- **Fixed label sets.** Every label value comes from a set the code names: a route pattern (never a path), a limit, quota, listener or event name, a status class, an operation, a table. **NEVER** use a user ID, IP, email, path or error message as a label.
- **Series caps as a second line of defence:** at most 100 series per metric and 300 routes. Past the cap, or for any value that isn't a plain label (control characters, over 200 bytes, invalid UTF-8), count under `unmatched`. Test it: send thousands of requests with random paths, invented methods and tokens in paths, and assert none of those values appear in `/metrics`.
- **Histogram buckets (ms):** 5, 10, 25, 50, 100, 250, 500, 1,000, 2,500, 5,000, 10,000, then +Inf. A p95 reading of "10 s" means "10 s or more". Document saturation like that.
- **An aborted request counts as `aborted`, never as a status class.** It got no answer, and a client that left isn't a server error. Its duration is still observed.
- 429s are counted in `4xx`. If you also want them separately, add a dedicated column; don't double-count them in a dashboard.

### Per-minute aggregates in the database

If you want a built-in dashboard without running Prometheus:

- **Once a minute,** each server writes the minute just ended **in one statement**: a `request_minutes` row per route with traffic (requests, 4xx, 429, 5xx, timeouts, aborted, duration sum, bucket counts), a `metric_minutes` row per changed series and per gauge, and its heartbeat.
- **Ten requests or ten million write the same rows.** Rows are bounded by construction: routes plus series, at most about 1,200 a minute. A test sends 10,000 requests and finds one row.
- **Insert-only, never retried:** if the write fails, drop that minute (log a warning). Totals stay in memory for `/metrics`, and a database outage costs one attempt a minute.
- **Hourly rollup** sums the minutes across servers into `request_hours` / `metric_hours`:
  - it runs under a **non-blocking advisory lock**, so one server does it; others skip their turn;
  - each hour is summed in a short transaction, 5 minutes after it ends, so every server's last minute is in;
  - it catches up at most 72 hours per run, and deletes minutes older than 48 hours in batches, never deleting one it hasn't summed;
  - a hard ceiling deletes minutes older than 7 days even if summing keeps failing.
  - Keep hours 90 days.
- **Use the database's clock** (`now()`) for every cutoff, never the app server's. Drop a minute more than 5 minutes ahead of the database clock, so a skewed server can't sum an hour that hasn't happened.
- A switch (`METRICS_STORE=off`) writes the heartbeat only.

### `/metrics` for an external scraper

- **NEVER on the public port.** Run a separate listener (e.g. `127.0.0.1:9090`) serving only `GET /metrics`, with its own timeouts.
- **Token:** with `METRICS_TOKEN` set (32+ characters), require `Authorization: Bearer`. Compare in constant time on digests, so even the token's length doesn't show. Production refuses to start when it binds anything but a literal loopback or private address without a token. That includes `0.0.0.0`, `::`, public addresses and host names, even `localhost`, because a resolver could point it anywhere.
- Use the Prometheus text format, `Cache-Control: no-store`, and a golden-file test. Each process reports itself since its own start, so scrape every server.

## Slow queries without SQL text

- Time every query with a database hook. Record `db_query_duration_ms` by operation.
- Log a query slower than `SLOW_QUERY_MS` (default 500, 0 = off, max 60,000) at `warn` (`msg: "slow query"`) with `requestId`, `operation`, `table`, `durationMs`, the calling data-layer method (read from the stack only for slow queries) and the scrubbed error if it also failed. Count it in `db_slow_queries_total{table}`.
- **NEVER log the query text.** ORMs interpolate values (emails, digests, tokens) into it. Use the database's own normalised statement stats (e.g. `pg_stat_statements`) for query analysis. Show only fully normalised statements: no literals, DML only. Test that a lookup by email logs neither the email nor any SQL.

## Health: liveness vs readiness

| Route | Answers | Used by |
| --- | --- | --- |
| `GET /api/health/live` | 200 while the process runs; **never touches the database** | orchestrator liveness. A database outage must not restart every server |
| `GET /api/health` | 200 when the database answers; **503** "Database unavailable"; **503** "Shutting down" once draining | load balancer / readiness, external uptime checks |

Neither is rate limited by the app; both log at `debug`.

## Graceful shutdown order

On SIGTERM, each step has its own share of the time:

1. **Readiness → 503 immediately**, but **keep serving for `SHUTDOWN_DELAY`** (5 s in production; 0 if the platform has its own pre-stop hook). The delay is what makes the 503 useful: the load balancer polling every few seconds removes the server before connections start being refused.
2. **Drain (10 s, shared):** stop accepting connections and finish in-flight requests. Stop the metrics listener. Let background queues and the event worker finish their current batch (unfinished deliveries are retried when their lease ends). Cancel and wait for the rollup.
3. **Final writes (5 s):**
   - stop the minute loop;
   - record `server.stopped` (1.5 s at most);
   - write the partial minute's metrics (1.5 s at most);
   - stop error reporting and flush the error groups **last** (2 s at least), so failures in earlier steps are kept.
   - Nothing retries; with the database down, each step gives up at its own deadline.
4. Set the container's stop timeout to **`SHUTDOWN_DELAY` + 20 s**. Docker's default of 10 s is too short and kills the final writes.

## Server start/stop events and the instance table

- `server_instances` has one row per running process: a fresh public ID per start, hostname, version (VCS revision, `-dirty` if uncommitted), `started_at`, `last_seen_at` (moved by each minute's write) and a **configuration summary**. A server silent for 2 minutes is gone; delete its row after a day.
- **The summary never holds a secret's value, part or digest.** It says which settings are set (`true`/`false`) and their non-secret values. Build it by copying fields one by one, never by walking the config struct, so a new secret can't slip in. Test it with a config full of known secrets: no 8-character run of any secret, raw, base64 or as a SHA-256, may appear.
- **Events:**
  - `server.started` (`server`, `version`);
  - `server.stopped` (`server`, `reason`: `signal` or `failed`).
  - A release shows as starts with a new version; a **crash loop shows as starts without stops**.
  - If the database refuses `server.started`, start anyway and let the heartbeat add the row.

## Audit lines (tamper evidence)

Every audit-class event (`auth.*`, `admin.*`, `server.*`, `prune.*`, `alert.*`) is also logged at `info` as its row is inserted (`msg: "audit"`): event ID, name, actor/user/tenant public IDs, **IP cut to /24 or /48**, time and request ID. **Never the payload.** Route these lines to an immutable store kept a year (see `privacy-and-compliance.md`). A row altered or deleted in the database then disagrees with a record nobody can change.

## Non-blocking logs

Logging is synchronous: if the log driver falls behind, requests wait. Use a non-blocking driver with a bounded buffer that drops lines rather than stalling (Docker: `--log-opt mode=non-blocking --log-opt max-buffer-size=4m`).

## Tests that keep this honest

- **Nothing secret in the logs**, an integration test that:
  - hits token pages, the admin path, admin API calls with the key header, requests with session and CSRF cookies, passwords and tokens in bodies, emails in queries, step-up codes and admin reasons;
  - asserts no log line holds any of them;
  - asserts each request has exactly one access line with the expected route and user.
- **Request IDs:** always new and yours; in the header, the log and the event; a trusted proxy's ID only as `proxyRequestId`; a forged one nowhere.
- **Recovery:** a 500 with the ID and no panic value; cookies dropped; headers kept; after a flush, the connection cut and `aborted`.
- **Panic values are redacted** (`panic(request)`, `panic(headers)`, a long message holding a token).
- **Liveness 200 and readiness 503** with the database down; readiness 503 while draining.
- **Bounded series**, no allocation while recording, a Prometheus golden file, slow queries logged without values, shutdown within budget even with the database down.
