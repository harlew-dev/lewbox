# Security, concurrency, leak and performance tests

Read this when a change adds a lock, a uniqueness or quota guarantee, a "same answer for everyone" promise, a secret-bearing field, a log line, a metric, a rate limit, an outbound call, or a query on a growing table. What to protect and how is owned by `security` and `ops`. This file is how you prove it.

## Race tests

A lock, transaction or unique constraint that no test can break is a guess.

```
test "two owners demoting each other leave one owner":
  repeat 5 times:
    tenant = tenantWithOwners(a, b)
    barrier = startTogether(2)
    parallel:
      barrier.wait(); send(PATCH member b → admin, as a)
      barrier.wait(); send(PATCH member a → admin, as b)
    assert countOwners(tenant) == 1
```

- **Fire the conflicting operations at the same moment** (a barrier or start channel), on **separate connections**, and **repeat 5+ times**, because one run can pass by luck.
- **Assert the invariant**, not a specific winner: exactly one sign-up wins, one owner remains, the quota ends exactly at its limit (two creates at 999 of 1,000 make one), a token works once.
- **It MUST fail without the lock.** Comment out the lock (row lock, advisory lock, `SELECT … FOR UPDATE`, unique index) locally, run it, and see red. Record this in the test's comment: `// fails without the tenant row lock`. If it stays green, the test doesn't exercise the race.
- **Deadlocks:** run opposing operations together (two co-owners deleting their accounts, delete racing a role change) and assert both finish within a timeout.
- **One-connection pool:** run a flow with the pool capped at one connection. Code that checks out a second connection while holding the first hangs here instead of in production under load.
- **Authority re-checked after waiting:** a write that waited on a lock must re-check permissions afterwards. Revoke the session, key or role while the write waits, and assert nothing was written.
- Use the non-failing request helper in goroutines and collect statuses. Assert in the main test thread.

## Enumeration: byte-for-byte comparisons

Wherever the design promises the same answer (sign-up with a taken or free email, password reset for a known or unknown email, wrong admin key vs unknown route, a blocked or new address):

```
a = send(POST /api/auth/register, {email: takenEmail,  …})
b = send(POST /api/auth/register, {email: freshEmail,  …})
assert a.status == b.status
assert a.body   == b.body                                  # bytes, not parsed-and-compared
assert headersExcept(a, ["Date", "X-Request-ID", "Set-Cookie"]) == headersExcept(b, …)
assert cookieNames(a) == cookieNames(b) and cookieAttributes(a) == cookieAttributes(b)
assert tablesWritten(a) == tablesWritten(b)                 # same rows created (e.g. a pending sign-up for both)
# only the inbox differs: one "confirm your email", one "you already have an account"
```

- Compare **raw bytes**. Comparing parsed JSON misses field order, whitespace and a trailing newline, all of which an attacker can diff.
- Compare **side effects** too: a pending row created for one and not the other is observable later (e.g. through a resend endpoint's behaviour).
- Hidden resources: an unknown path, a wrong key, a near-miss key, a missing pass and "feature off" all return **exactly** the generic 404, for GET and HEAD, with conditional and `Range` headers too.
- Timing is part of the answer. Where a known account does expensive work (password hashing), the unknown path does equivalent work. Test that the code path runs (e.g. the hasher is called), not wall-clock time, which is flaky.

## Nothing secret in the logs (and anywhere else)

```
capture = captureAllLogLines()
secrets = [password, resetToken, mfaCode, sessionCookie, csrfToken, apiKey, adminKey,
           email (when sent in a query string), reason text of an admin action]
drive every flow that carries one of them (sign-in, reset page, invitation page,
      API key calls, admin calls, a search with an email in ?q=)
for line in capture: for s in secrets: assert s not in line
assert every request produced exactly one access line with its route pattern (not the raw path)
```

Extend the same idea to every place data leaves the request:

| Sink | Test |
| --- | --- |
| Logs | the capture above, plus panics: the panic value, headers or URL in a panic are redacted, and a 500 body has no stack or exception text |
| Event payloads | no token, code, password or raw key. Tokens appear in no event at all |
| Error reports | scrubbed before storage: known secret shapes (keys, tokens, emails, credentials in lower-case or digit-only form, credentials cut off at an input boundary) replaced whole |
| Data exports | walk the JSON for secret-named keys, and search the text for the real secret values and **other users'** data |
| Alerts and webhooks | signal names and numbers only: no address, IP, name, link or key |
| Config summaries | no secret, digest or unlisted field |
| Slow-query logs | normalised statements, no parameter values |

Seed the flows with **real-looking distinct values**, so a substring match finds a leak instead of colliding with something harmless.

## Metrics cardinality

- Labels come from **fixed sets**: route patterns (`/api/tenants/{tenant}/projects`), a closed list of methods (`OTHER` for the rest), status classes, limit names.
- `TestClientsCantMintSeries`: send random paths, unknown methods, odd headers and random query strings, and assert the number of series didn't grow.
- Storage of request metrics scales with routes, never traffic: 10,000 requests to one route are one row per minute.
- `/metrics` output matches a **golden file**, and refuses a wrong or missing token.

## Rate limits

- **Pre-fill, don't loop:** create the counter row (or fill the in-memory counter through the middleware) at the threshold, keyed exactly as the limiter keys it, then send one request.

  ```
  rateLimits.forLimit("auth:per-ip", "192.0.2.10").exhausted().createOne(…)
  resp = send(POST /api/auth/login, from="192.0.2.10")
  assert resp.status == 429
  assert retryAfterWithin(resp, 1..900)                     # seconds, rounded up
  assert onlyEvent("rate_limit.reached")
  assert nothing created
  ```

- Make sure the test's earlier requests haven't already created the counter, or you'll pre-fill a different window.
- **Keying:** one IPv6 /64 shares a budget, separate IPv4 addresses don't. Behind a trusted proxy, the forwarded address is counted. From an untrusted connection, a changing `X-Forwarded-For` doesn't reset the budget.
- **Order and spending:** limits apply in order (per user, then per email, then global). A request refused by an earlier limit spends no later one. Forged requests (no CSRF token) aren't counted. Unknown emails count like known ones (or the limit becomes an oracle).
- **Global limits** answer 503 rather than 429 where designed. Challenge levels (CAPTCHA) answer 428, verify one token once, and a 428 creates nothing.
- **Concurrency:** 100 concurrent requests against a limit of 50 let exactly 50 through and record the crossing once.

## Fakes at external boundaries

| Boundary | Fake | Still real |
| --- | --- | --- |
| SMTP | an in-memory mailbox driver that records each message (to, subject, text, HTML, headers) | the queue, the worker, templates, the listener deciding to send |
| CAPTCHA | a fake provider for the flow tests; a local HTTP server imitating the provider for the client tests | the challenge levels and limiter |
| Outbound HTTP (webhooks, verifiers) | a local HTTP test server | your client: timeouts, redirects **not** followed, oversized and non-JSON bodies, signing |

- Test the client against the fake server for failure modes: hung server (times out), redirect (not followed, target never called), 500, malformed JSON, an endless body (bounded read), and that it sends exactly the documented fields.
- Read links and codes from the recorded message's **text and HTML parts**, and assert both carry the same link.

## Golden files

- For byte-exact output: metrics exposition, rendered emails, CSV exports, generated code, a security-headers snapshot.
- Stored beside the test (`testdata/<name>.golden`). Regenerate only with an explicit switch (`UPDATE_GOLDEN=1`), never automatically when they differ, and review the diff in the change.
- Strip non-deterministic values (timestamps, random IDs) before comparing, or inject fixed ones.

## Performance budgets

### Benchmarks with a budget

```
BenchmarkRequestOverhead:
  handler = middlewareStack(noopHandler)    # request ID, access log to a discarding writer, metrics
  for i in 0..N: handler.serve(request)
  # budget: ≤ 20 µs and ≤ 3 allocations per request
```

- Write the budget in the docs beside the benchmark. A change that exceeds it needs a reason in review.
- Use allocation counts as well as time. They're stable across machines, while time isn't.

### `EXPLAIN` tests

```
plan = explain(db, query, discourageSeqScan = true)    # in a rolled-back transaction
assert plan uses index "events_tenant_created_idx"
assert plan has no "Seq Scan on events"
```

- For queries on tables that grow without bound: audit events, logs, erasure and retention jobs, per-tenant lists.
- Discouraging sequential scans makes a ten-row test table plan like a ten-million-row one.

### Volume checks with a budget

- A seeder fills a **development** database at realistic volume (e.g. 1,000,000 events, mostly recent, with bursts).
- Run the real queries with `EXPLAIN (ANALYZE, BUFFERS)` and record each timing in the docs with the date and engine version, e.g. "list 0.6 ms, counts 88 ms, 7-day counts 1.2 s, all under the 2 s budget".
- A query over budget gets an index, a rollup table or a narrower default window, never a slower page or a longer timeout.

### Bounds

Every in-memory collection, queue or cache that grows with input has a cap and a test filling it past the cap: the oldest is evicted, or new entries are refused, and memory stays bounded.
