# Testing the guarantees

Read this when writing tests for any auth flow, or when a review says "the responses are identical". General test structure, race tests and fakes are in `testing` (`security-tests.md`); this file is the auth-specific toolkit. **A "same answer" promise that isn't asserted byte for byte doesn't exist.**

## Helper 1: `sameAnswer(a, b)`

Compares two raw responses: status, **body bytes**, and **every header** except the request ID (after checking each has a well-formed one).

```
sameAnswer(name, got, want):
  assert got.status == want.status
  assert got.bodyBytes == want.bodyBytes               # bytes, not parsed JSON
  for r in [got, want]: assert matches(r.header("X-Request-ID"), /^req_[0-9A-Za-z]{14}$/)
  assert headersWithout(got, "X-Request-ID") == headersWithout(want, "X-Request-ID")   # names, values, order of values
```

- **Never** compare parsed JSON, key sets, or "status and error field". Field order, whitespace, a trailing newline, an extra field and `Content-Length` are all observable.
- **Cookies:** a flow cookie with a random value differs legitimately. Use a variant `sameAnswerExceptCookieValues` that replaces each `Set-Cookie` value (and only the value) with a placeholder, then compares names, attributes and `Max-Age`. Freeze the clock so `Expires` matches.
- **Values that legitimately differ** (the typed address echoed back, a newly created resource's public ID, timestamps): substitute them with placeholders in both bodies **before** the byte comparison (`replace(bodyA, emailA, "<email>")`). Substituting is explicit and narrow; comparing key sets is neither.
- In Go: `httptest.ResponseRecorder` + `Header().Clone()`; in Node: supertest/`fetch` with `res.headers` entries and `await res.arrayBuffer()`; in Python: `client.post(...)` with `resp.content` and `resp.headers.items()`; in PHP: the framework's test response `getContent()` and `headers->all()`.

## Helper 2: tables touched, instead of timing

Timing tests are flaky and catch nothing small. Assert the **structure** of the work instead: the same tables, in the same order, for both inputs, and never the table that would leak.

```
tablesDuring(run):
  log = []
  install query hook: on each query, append every table named after FROM / INTO / UPDATE / JOIN
  run()
  return log

a = tablesDuring(() => post("/register", {email: NEW}))
b = tablesDuring(() => post("/register", {email: TAKEN}))
assert a == b
assert "users" not in a
```

Hook points: Go Bun `QueryHook` / pgx tracer; Node Prisma `$on("query")`, Knex `on("query")`, Drizzle logger; Python SQLAlchemy `before_cursor_execute`; PHP Laravel `DB::listen`; or Postgres `log_statement = all` in the test database as a last resort.

- Run the listener separately (it is *supposed* to differ): assert request-time tables only.
- For sign-in, the expected list is: the hash fetch from `users`, the event insert, the lock/failure upsert, for both known and unknown addresses.

## Helper 3: the dummy-hash path

You can't test the timing, so test that **the work happened**: inject a counting password verifier (or wrap the real one) and assert it ran exactly once with the dummy hash for an unknown address, and exactly once with the real hash for a wrong password.

```
verifier = countingVerifier(real)
post("/sessions", {email: UNKNOWN, password: "x"})
assert verifier.calls == [(DUMMY_HASH, "x")]
post("/sessions", {email: KNOWN, password: "wrong"})
assert verifier.calls[-1] == (knownUser.hash, "wrong")
```

Also assert: the dummy hash's parameters equal the current parameters (parse the PHC string), and a stored hash with older parameters is rehashed after a successful sign-in.

## Helper 4: what the inbox got

A fake mailer that records messages. Assert the **only** difference between the two flows is in the recorded messages, and that both text and HTML parts carry the same link or code. Run the listener explicitly (drain the job queue) so "no mail before the worker" is also asserted.

## Per-flow comparison tests

Name each after the property it protects. Each compares the inputs in the right-hand column with `sameAnswer` and `tablesDuring`.

| Test | Compare |
| --- | --- |
| `sign-in looks the same for an unknown address` | unknown vs wrong password vs malformed email vs 129-byte password; plus the dummy-hash assertion |
| `sign-in hides account state until full proof` | wrong password for active, suspended, locked, unverified, MFA-on accounts and an unknown address; MFA-on: right password for active vs suspended vs locked (`{mfaRequired}` identical), wrong codes identical |
| `the lock is invisible to guessers` | drive a real and an unknown address past the lock threshold (seed the failure count, or advance the clock past `login:per-email` windows): every 401 before, at and after it identical; the right password then reveals the lock (MFA off) |
| `sign-up looks the same for a taken email` | new vs taken vs blocked: register, check, resend, 4 wrong codes + the 5th; tables never include `users` |
| `reset request looks the same for every email` | unknown vs active vs suspended vs locked: 202 bodies and headers; one event each; mail only for eligible accounts, only after the worker |
| `email change looks the same for every destination` | free vs taken vs blocked: POST, GET after the listener, wrong-code PATCH |
| `inviting looks the same for every address` | registered vs unregistered vs blocked: 201 (ID and address substituted), the listing afterwards |
| `tenant 404s are the same` | non-member vs missing vs malformed tenant, per route; missing vs malformed vs another tenant's child resource |
| `API key failures are the same` | every 401 class; another tenant's path vs unknown route; out-of-scope vs missing project |
| `the admin door is invisible` | every admin route × {no key, wrong key, near miss, admin off, outside allowlist} vs `/nowhere` |
| `rate limits don't depend on existence` | pre-fill a per-email counter for a real and an unknown address: identical 429 bodies and `Retry-After` |
| `the CAPTCHA says nothing about accounts` | two IPs at the same count, known vs unknown address: identical 428 bodies |
| `signed-in checks don't move anonymous counters` | after 10 signed-in password checks, the anonymous `login:per-email` counter for that address is untouched (and vice versa) |
| `sign-up spam can't lock sign-in` | exhaust `register:per-email`; sign-in for that address still answers normally |

## Other required tests

- **Races map to the right status:** every concurrent pair (two completions of one reset link, two accounts claiming one address, a block racing a sign-up, an accept racing a suspension) is fired at the same moment on separate connections, repeated 5+ times, and asserts the documented status for the loser (422, 409, 401), **never 500**.
- **The CAPTCHA token-URL guard:** see `known-pitfalls.md`.
- **Listener caps:** the account-exists, email-change-requested and lock notices stop at their cap, silently, recording one crossing.
- **Tell-tale events stay hidden:** `invitation.sent` absent from the tenant log; `email_change.sent` absent from the personal export; no token, code or hash in any event payload.
- **Fresh proof is server-chosen:** for each sensitive action, an MFA account sending only the right password gets 422 on `code`; a non-MFA account's code is ignored.
- **Route allow-lists:** the unverified-account allow-list and the unmetered recovery routes are asserted by exact count from the route table.
- **Production config:** refuses to start with verification off, test CAPTCHA keys, or a missing app key.
