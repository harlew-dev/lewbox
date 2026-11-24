---
name: testing
description: Testing strategy and rules for any stack. Covers what is worth testing, backend integration tests on a real database with factories, the coverage every endpoint must have, permission and API-key matrices, race, enumeration and log-leak tests, rate-limit tests, frontend unit tests with an API stub table, contract-drift tests, end-to-end runs, manual checks, and what to run before saying you're done. Use it when you write or review tests, add an endpoint, page, table, permission, event or rate limit, set up a test harness, fix a flaky test, or are about to report a change as finished.
---

# Testing

Tests show that the product behaves, that its security boundaries hold and that data really persists. A test that cannot fail, or that fails on every harmless change, costs maintenance and protects nothing. Skip a rule here and you get a production bug that a green suite never saw.

A project's `AGENTS.md` may record deliberate deviations from this skill; where it does, the project's deviations win.

## When to use

- Writing or reviewing any test, or setting up a test harness for a new stack.
- Adding an endpoint, permission, role, event, rate limit, table, column or page. Each comes with required tests (below).
- Fixing a bug: write the failing regression test first.
- Fixing a flaky test. Flakiness is a bug in the test or the code, never something to retry away.
- Before claiming a change is done ([Before claiming done](#before-claiming-done)).

## Philosophy

- **ALWAYS test behaviour, security boundaries and real persistence.** Test what a user or attacker can observe: status codes, bodies, what is stored, what is sent, what is logged.
- **NEVER write tests that mirror trivial code.** Asserting a CSS class, that a constant equals itself, that a getter returns its field, or that a mock got the arguments you passed it re-states the code. It breaks on every refactor and catches no bug.
- **A test MUST be able to fail.** Before you trust a test, make the bug it guards against happen (remove the lock, drop the permission check, return the internal ID) and watch it go red. A check that has never failed may be checking nothing.
- **A refactor adds no tests and changes no assertions.** The existing suite, the permission matrix above all, must stay green unchanged. If you had to edit an assertion, either the behaviour changed (so it was not a refactor) or the test was tied to the implementation (fix the test first, in a separate change).
- **A refactor that moves code proves no test was lost.** Record the list of test names before and after (e.g. `go test -list '.*'`, `vitest list`, `pytest --collect-only -q`): the same names (apart from paths), the same count passing, none skipped. Break each moved guard test (permission matrix, registry checks) once on purpose and watch it fail, because a guard whose file moved can silently stop finding the code it checks. Each step changes only names, locations and imports; logic edits wait for a separate change.
- **Bug fixes start with a regression test** that fails on the old code and passes on the fix.
- **Prefer** fewer, deeper tests through the real stack (HTTP → router → middleware → DB) over many shallow unit tests of glue code. Keep unit tests for pure logic: parsing, validation, pagination maths, formatting.

| Tier | What it proves | Real | Faked |
| --- | --- | --- | --- |
| Unit | Pure functions: parsers, validators, helpers (table-driven) | everything it touches | nothing |
| Backend integration | Endpoints end to end: auth, validation, persistence, events, limits | router, middleware, DB, queue/worker | SMTP, CAPTCHA, outbound HTTP |
| Frontend unit | Components and pages: states, roles, accessibility, requests sent | component tree, router, data layer | the HTTP API (stub table) |
| Contract drift | Client and server agree on allow-lists and names | both sources | nothing |
| End-to-end | User flows in a real browser against the built server | server binary, DB, mail catcher, browser | nothing except third parties you can't reach |
| Manual | It looks and feels right; failure screens work | everything | nothing |

## Backend integration tests

Details, harness design and examples: [references/backend-integration.md](references/backend-integration.md).

- **MUST run against the real database engine** used in production, in a throwaway schema (or database) per test: unique name, migrated from scratch, `search_path` pinned to it, and only that schema dropped at cleanup. Fakes and in-memory substitutes skip constraints, locks, collations and query plans, and those are where the bugs are.
- **Test configuration is a committed file of throwaway values** (e.g. `.env.testing`). Tests read the DB URL from that file, **never from the process environment**, so a developer's dev or prod `DATABASE_URL` cannot be used by mistake. The harness MUST refuse any host that isn't local and any database whose name isn't `*_test` or `*_test_<port>`. A test asserts the committed file parses, points at a local test DB, and holds no value for anything named like a secret.
- **NEVER skip because a dependency is missing.** Fail, and say how to fix it: `database unreachable: start it with "make db"`. In CI a skipped suite looks green while it has stopped testing anything.
- **Prefer** building the real server and router in each test (`server(t)`) and sending real HTTP requests through a helper that sets the trusted Origin, JSON content type, CSRF token and cookie and fails on an unexpected status.
- Run backend tests with the race/concurrency detector where the language has one.

## Test data: factories

Details: [references/backend-integration.md#factories](references/backend-integration.md#factories).

- **ALWAYS build data with factories**, never hand-written `INSERT`s and never setup through API calls. The exception is the endpoint under test: test registration through registration, not through a factory.
- **Set only the fields the test asserts on** (`state`/`sequence` overrides). Leave the rest random. A test that sets every field hides which ones matter, and random data finds bugs that fixed fixtures never do.
- **Randomness MUST be reproducible:** each run takes a fresh seed, a failing test prints `fake data seed N`, and `FAKER_SEED=N` replays it exactly.
- **Every table has a factory.** A test lists the migrated tables and fails for any without one. It also inserts every factory's output into a fresh schema, so a new required column fails until the factory fills it.
- **Factory output MUST pass the API's own validation** (tested). Otherwise a test can "prove" behaviour on data no user could ever create.
- Factories default to the safe state: tokens unused but stored only as a digest (the raw one kept on the factory result for the test), keys revoked unless asked, emails at `example.test`, IPs from documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`).
- Production builds MUST NOT include factories or seeders. Seeders reuse factories and refuse to run in production.

## Mandatory coverage per endpoint

Full checklist with example skeletons: [references/endpoint-coverage.md](references/endpoint-coverage.md).

Every endpoint MUST have integration tests for:

| # | Case | Assert |
| --- | --- | --- |
| 1 | Success | Status (`store` 201, `update` 200, `destroy` 204), body shape, type-prefixed public IDs |
| 2 | Validation | Table-driven: each bad value → 422 naming its `field`; unknown fields 400; wrong content type 415; hostile text (NUL, U+202E, U+200B); **nothing persisted** |
| 3 | 401 | No session, expired session, revoked session |
| 4 | 403 | No CSRF token; missing or foreign Origin; a role without the permission |
| 5 | 404 | A non-member on every tenant route; another tenant's record reached through your own tenant's path; unknown and already-deleted IDs. All give the same answer |
| 6 | No internal IDs | Walk the response JSON: no numeric `id` and no internal key anywhere |
| 7 | Persistence | Read back from the DB (or a fresh `show`), not just the response; `update` changed only what it should |
| 8 | Ordering and filters | Every sort across page boundaries, stable ties, combined filters, literal `%`/`_` in search, past the end, bad params 400 |
| 9 | Events | **Exactly one** event with the right name, actor, tenant, public IDs and request ID; **none** when the request was refused |
| 10 | Rate limit | 429 with `Retry-After` from a pre-filled counter; nothing created |

## Matrices

- **Role × route permission matrix:** one table lists every tenant route with the roles allowed. The test walks the router's **real registry** and fails when a registered route is missing from the table. Allowed roles succeed, other roles get 403, outsiders get 404. Rows that destroy their target build a fresh target per request. This is the test that stops a new route from shipping without authorization.
- **Every mutation declares a permission** (tested from the registry), and any exceptional set (e.g. routes open to unverified accounts) is asserted as an **exact** list, so adding one is a deliberate diff.
- **API keys × scope:** every key-reachable route × key kind (personal, tenant) × the owner's live role × the required scope present or absent. It fails on an unlisted route and checks that demoting the owner narrows the key at once.

Patterns: [references/endpoint-coverage.md#matrices](references/endpoint-coverage.md#matrices).

## Security tests

Details and patterns: [references/security-tests.md](references/security-tests.md).

- **Race tests MUST fail without the lock.** Fire the conflicting operations at the same time (two owners demoting each other, two final creates at quota − 1, block racing sign-up), repeat 5+ times, and assert the invariant: exactly one wins, one owner remains, the quota is exact. Comment out the lock once locally and confirm it goes red. Add a one-connection-pool test for code that might nest pool checkouts (deadlock).
- **Enumeration comparisons are byte for byte.** For taken vs free email, known vs unknown account, wrong key vs unknown route: compare status, body, cookies and headers (ignoring only per-request values such as the request ID and date), and that the same tables were written. Only the inbox may differ.
- **Nothing secret in the logs:** capture every log line while driving flows that carry passwords, tokens, codes, cookies, CSRF tokens, API keys and emails in queries, then assert none of those raw values appears. Do the same for event payloads, error reports, exports and alerts.
- **Metrics cardinality:** labels come from fixed sets (route pattern, never the raw path). A test sends random paths, methods and headers and asserts no new series appears.
- **Tokens:** stored as a digest only, used once (also under concurrency), expire, travel in bodies never URLs.

## Rate limits

- **Test limits by pre-filling the counter** (a factory for the counter row, keyed exactly like the limiter: per IP, per IPv6 /64, per account, per email) to its threshold, then send **one** request. Assert 429, a rounded-up `Retry-After`, the refusal event recorded once, and nothing created.
- NEVER loop hundreds of requests to reach a limit. It's slow, and flaky at window boundaries.
- Make sure the test's own earlier requests haven't already created the counter you pre-fill.
- Limits may be switched off in the e2e configuration (one run signs up many users from one IP), but never in tests of the limits themselves.

## Fakes and golden files

- **Fake only at external boundaries:** SMTP (an in-memory mailbox driver, with the real queue and worker still running), CAPTCHA (a fake provider, plus one test against a local HTTP server pretending to be the provider), outbound HTTP (a local test server for timeouts, redirects, oversized and non-JSON bodies).
- **NEVER mock your own database, repository, service layer or framework.** A mocked store only proves the mock works.
- **Golden files** for byte-exact output: metrics exposition, email bodies (HTML and text), CSV exports, generated clients. Regenerate them only with an explicit flag (`UPDATE_GOLDEN=1`) and review the diff like code.

## Performance budgets

Details: [references/security-tests.md#performance-budgets](references/security-tests.md#performance-budgets).

- **Hot-path middleware gets a benchmark with a written budget** (e.g. "≤ 20 µs and ≤ 3 allocations per request"). A change that exceeds it needs a reason in review.
- **Queries that must stay fast get an `EXPLAIN` test** asserting they use their index (with sequential scans discouraged, so a small test table plans like a big one).
- **Volume tests:** seed realistic volume (e.g. 1M rows) into a dev DB, `EXPLAIN ANALYZE` the real queries, and record the timings with date and engine version in the docs against a budget (e.g. every query < 2 s). A query over budget gets an index or a rollup, not a slower page.
- Anything that grows (in-memory groups, queues, label sets) has a cap, and a test proves the cap.

## Architecture and naming checks

Naming and layout rules (routes, permissions, events, rate limits, file and page names, doc links) are **ordinary tests** that fail the suite. `conventions-and-docs` owns them. When one fails, **rename your code, never loosen the check**. Never add an exception, skip or delete a check without the user's agreement. The usual shape: a pure function, a table of names it must accept and refuse, a run on the real code that fails if it finds suspiciously few names, and exceptions listed in the test with reasons.

## Organisation

- **Test names are descriptive sentences** about the behaviour: `TestTwoOwnersDemotingEachOtherLeaveOneOwner`, `it("keeps the typed email when the save fails")`. Never `TestHandler2` or `it("works")`. A failure should read as a bug report.
- **Prefer** putting backend integration tests beside the code they cover, and tests that span areas in the core's folder. Frontend tests sit beside their component (`thing.<ext>` → `thing.test.<ext>`).
- **One shared test-helpers module** (`apitest`, `test/`) holds the request helper, `signedIn`, `ownedTenant`, lookups by public ID, event assertions and the API stub. **Only tests may import it** (an architecture test enforces this), and it must not import the server's composition root, so any package's tests can use it. A helper used by one package's tests stays in that package.
- Tests never share state through module variables. Each test creates what it needs.

## Frontend unit tests

Details, stub-table design and examples: [references/frontend-unit.md](references/frontend-unit.md).

- Tests sit beside the code and run in a DOM environment. Shared setup cleans up after each test.
- **One API stub table** keyed by `"METHOD url"` (exact URL, then path without query, then `"*"`). A list of replies is used in order, last repeating. A reply can wait on a `gate()` (to assert pending states) or fail as a network error. **Unmatched requests get a 404 naming the missing stub.** The stub records what was sent (method, URL, body, CSRF token).
- **Query by role and accessible name** (`getByRole("button", { name: "Save" })`). Assert behaviour (text, `aria-*`, disabled, focus, navigation, requests sent), never styling.
- Pure helpers get table-driven tests. The typed API client gets **type-level tests** asserting wrong paths and queries fail to compile (`// @ts-expect-error` or the language's equivalent).
- **Fake timers** for debounces, delays, minimum spinner times and toasts. NEVER real sleeps.
- **Render each page per role:** what a role may not do is absent, and **no request is made** for data it may not read.
- **User-supplied text renders as text:** a `<script>` or `<h1>` in a name shows literally.
- Every data view covers **pending, empty, failure with Retry, and retry success**. Every mutation covers pending (one request however many clicks), refusal kept in the form with its field error, and success.

## Contract drift

When client and server each hold a copy of an allow-list (sort keys, filter values, permission and role names, event types, route patterns), **a frontend test reads the server's source** (or its generated schema) and asserts the two are equal. It must also assert it found a minimum number of entries, so a moved file can't make it pass by finding nothing. For keys the client sends, also add the **other direction**: a server integration test sends every client key and expects 200. **Prefer** generating the client's types from the server's schema where the stack allows. Then this test only guards what generation can't.

## End-to-end tests

Details: [references/e2e.md](references/e2e.md).

- **Run against the real built server** (production build, same binary and asset pipeline) on **its own port and its own database**, rebuilt from scratch at each run's start. Never against the dev server: it lacks the production headers, CSP and page gating.
- **Concurrent runs MUST be isolated:** a port lock is taken before building, the database is named after the port (`app_test_<port>`), and `E2E_PORT=auto` picks a free one. Two runs in one checkout share build output, so each agent uses its own worktree.
- **Unique users and data per test** (`e2e-<timestamp>-<n>@app.test`). No reliance on seed data. Clean up what you created.
- **A real mail catcher** (e.g. Mailpit) receives real SMTP; tests read codes and links through its API, finding their mail by their unique recipient.
- **Any page error or CSP violation fails the run.**
- **Wait on conditions** (URL, element, network response, retrying assertion). NEVER `sleep`.
- Save **desktop and mobile screenshots** of key screens as artifacts.

## Manual demo and failure checks

Keep a numbered manual demo script and a failure-check list in the docs, and walk them for user-visible changes. See [references/e2e.md#manual-demo-and-failure-checks](references/e2e.md#manual-demo-and-failure-checks).

## Running suites on a shared machine

Other people and agents may be running tests on the same machine, in this checkout or another worktree.

- **Before a heavy run** (full suite, e2e, production build), check what's running (test processes, test servers) and which ports you'll use. If the machine is busy, wait. Several suites at once make all of them slow and flaky. A single focused test is fine at any time.
- Start long runs in the background and wait for the result. Don't start a second copy of your own run.
- **NEVER stop, restart, reuse or kill a server, container or test run you didn't start.**
- Use automatic port selection for e2e and use the URLs and database the run reports.

## Before claiming done

- Run **every relevant suite**: unit and integration (with the DB up), the checks (lint, types, format, naming and link checks), e2e for user flows, the production build for build or composition changes, and the dependency audit for dependency or auth changes.
- **Report failures honestly:** name the failing test and the error. NEVER skip, delete, loosen or `.only` a test, add a retry, or raise a timeout to turn it green. NEVER say "tests pass" for a suite you didn't run. List what you didn't run and why.
- A failure unrelated to your change is still reported, with your evidence that it's unrelated (e.g. it fails on the base commit too).

## Pre-merge checklist

- [ ] New or changed endpoint: all 10 coverage cases, and its row in the permission matrix (and the API-key matrix if keys reach it).
- [ ] New table or required column: factory updated, the "every table has a factory" and "factories pass validation" tests green.
- [ ] Data built with factories, setting only asserted fields. Seed printed on failure.
- [ ] Any lock or uniqueness guarantee: a race test that failed with the lock removed.
- [ ] Any "same answer" promise (enumeration): byte-for-byte comparison test.
- [ ] New secret-bearing field or flow: covered by the "nothing secret in the logs" test.
- [ ] New metric label: from a fixed set, covered by the cardinality test.
- [ ] Rate limit: tested by pre-filling, 429 plus `Retry-After`, nothing created.
- [ ] Frontend: role, pending, empty, failure and retry states. No request without permission. User text renders as text. Absence assertions inside collapsed UI open it first.
- [ ] Client copy of a server allow-list: a contract-drift test.
- [ ] User flow: e2e on an isolated port and DB, no page errors or CSP violations, screenshots saved.
- [ ] Refactor: no tests added, no assertions changed, everything green. Code moves: test-name list identical before and after, each moved guard broken once on purpose.
- [ ] Every relevant suite run. Failures reported, nothing skipped or loosened.
