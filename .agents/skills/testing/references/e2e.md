# End-to-end tests, manual demo and failure checks

Read this when you set up or change the e2e harness, write a browser test for a user flow, debug a flaky e2e test, run e2e beside other runs, or do the manual checks before handing over a user-visible change. Examples use Playwright terms; Cypress, WebdriverIO and others have equivalents.

## The server under test

- **The real production build:** the e2e harness builds the app exactly as it ships (server binary or container with the built frontend) and starts it itself. The dev server lacks the production security headers, CSP, server-side page gating, prerendered pages and the 404 behaviour for unknown URLs, so e2e on the dev server misses whole classes of bugs.
- **Its own database,** named after its port (`app_test_<port>`), dropped and migrated from scratch at every start. Hand-made accounts on it vanish at the next run.
- **Never reuse a server already listening on the port,** because it may be someone else's run. Offer an explicit opt-in (`E2E_REUSE=1`) for iterating against a test server you started yourself.
- **Settings come from the committed test config,** never the developer's `.env`, however the runner is started. How the file is loaded and per-run keys are generated: `tooling` (`references/env-files.md`).
- **Rate limits off** for the main e2e server (one run signs up many users from one IP), quotas still on. A dedicated spec starts a second server with limits on to test challenges and 429s.
- **Second servers for other settings:** a flow needing other configuration (feature flag on, verification required) starts another instance of the same build on a second reserved port, sharing the run's database when it needs the same data. Each step uses its own browser context, because cookies are shared across ports.

## Isolation for concurrent runs

Ports, locks, per-port databases, the preflight and clash messages: `tooling` (§7 and `references/test-databases-and-parallel-e2e.md`). For the tests themselves:

- Use the URL and database the run prints (`E2E run: app http://localhost:3104 · database app_test_3104`). Never guess or hard-code them.
- The mail catcher is shared by every run, so tests find their own mail by unique recipient ([Mail](#mail)).

## Test data

- **Unique users per test:** `e2e-<worker-start-timestamp>-<counter>@app.test`. A retried test restarts its worker, so never share data between tests through module variables. Each test sets up what it needs.
- **No reliance on seed data.** Create it through the UI when the UI is under test, through the API (with the page's own Origin and the CSRF token from `/api/me`) when it's setup, and directly in the run's database (`psql` on the run's URL) only for what no user action can create (granting admin, memberships beyond a quota, planted failures).
- **Clean up** what the test created (`afterAll`, by its unique users) through the API or the run's database. Never touch rows you didn't create.
- Sign-ups go through the real verification flow, reading the code from the mail catcher. Helpers: `mailedCode(to)`, `mailedLink(to)`, `signUpThroughApi(request, account)`.
- TOTP codes are computed in the test from the setup key the page reveals, using the standard library's HMAC. No extra dependency.

## Mail

- The server sends **real SMTP** to a local catcher (e.g. Mailpit). Tests read messages through its HTTP API (`search?query=to:"…"`, newest first) by unique recipient.
- Assert the **plain-text and HTML parts carry the same link**, the right headers (`List-Unsubscribe` where required), and that nothing arrives when nothing should (a blocked address: wait for the delivery to be marked done, then assert the inbox is empty).
- Watch it live in the catcher's web UI while debugging.

## Failing the run on page errors and CSP violations

```
page.on("pageerror", e => errors.push(e))
page.on("console", m => { if (m.type() === "error" && /Content Security Policy/.test(m.text())) errors.push(m) })
afterEach: expect(errors).toEqual([])
```

Also assert the response headers of the main pages (CSP with script hashes and no `unsafe-inline`, the security headers), and that the prerendered HTML has its title, description and stylesheet.

## Waiting

- **Wait on conditions:** `await expect(page).toHaveURL(/\?q=alpha/)`, `await expect(row).toBeVisible()`, `await page.waitForResponse(r => …)`, `await expect(async () => …).toPass()`.
- **NEVER `waitForTimeout`/`sleep`.** A fixed sleep is too short on a busy machine and wasted time everywhere else.
- Debounced inputs: after `fill()`, wait for the URL or request the debounce produces before the next step, or a refresh fires against the previous query.
- When matching requests, account for query strings: a glob on `/projects` doesn't match `/projects?page=2`. Use a regex like `/\/projects(\?.*)?$/`.
- Search boxes are `searchbox` by role (`type="search"`), not `textbox`.
- **Collapsed UI (danger zones, accordions):** first assert that Tab skips the closed section's buttons, then open it through one shared helper (`openDangerZone(page)`) before acting or asserting absence. "Button X is not there" passes trivially while the section is closed, so it proves nothing.

## Screenshots and traces

- Save **desktop and mobile** (e.g. 390 px wide) screenshots of each key screen and dialog as test attachments, and to a directory named by an environment variable when the caller wants them. Hide or blur credentials before capturing.
- Keep traces for failed tests (`trace: "retain-on-failure"`), replayable step by step with network and DOM snapshots. Optional video for the whole run (`E2E_VIDEO=1`) and headed mode for watching live.

## What the e2e suite covers

User flows, not every edge case (the integration and unit tests own those):

- Registration → verification code → onboarding → first tenant's dashboard.
- Create (pending state, no duplicate submit), edit, reload (persisted), delete from a confirmation dialog.
- Lists: search, filter, sort, paginate. State in the URL. Reload, back/forward and a deep link (`?page=2`) restore it. Past the end.
- Not found for unknown records and for other tenants. Read-only views for lower roles.
- Sign-out, sign-in, returning to `?next=` (another site's URL refused), session revocation from another browser.
- Signed-out access to a signed-in URL: server redirect (302) before any app HTML.
- Password reset and invitations through the real inbox. A link used twice is refused.
- Mobile layout: navigation, search, paging and editing usable at 390 px.
- No page errors or CSP violations anywhere.

## Running the e2e suite

- Install the browser through the runner (`npx playwright install chromium`). Allow an override path for a system browser. A browser that can't launch should fail every test within seconds, with the reason in the error context.
- Run with automatic port selection beside other runs: `make test-e2e E2E_PORT=auto`.
- Shared-machine etiquette from the SKILL applies: check what's running, don't overload the machine, never touch servers you didn't start.

## Manual demo and failure checks

Keep a **numbered manual demo** in the project's testing doc. Walk the relevant steps for every user-visible change and capture screenshots for the handover. A generic skeleton:

1. Sign in with the demo account. You land on the newest tenant's dashboard, with correct counts.
2. Create a record: the button shows a spinner and blocks double submits, and the new record shows first.
3. On the list: search, filter, sort by column headers, page through (seed volume data first). The URL keeps the state. Reload and back/forward restore it.
4. Open a record, edit it, reload: the change persisted.
5. Account settings: change your name (the account menu updates), sign out other sessions, sign out.
6. Create a second tenant (it opens empty) and switch between them.
7. Register another account through the emailed code. It can't see the first account's tenant (its URL shows not found). Signing up again with the same email shows the same page, and the inbox gets "you already have an account" instead of a code.
8. Signed out, open a signed-in URL: you land on login and return there after signing in.
9. At 390 px: navigation, search, paging and editing all work.
10. Any feature-specific flows (admin actions, CAPTCHA with the provider's test keys, feature flags on and off).

**Failure checks** (also in the doc, walked when touching data loading, errors or sessions):

- Run the API and frontend separately, then stop only the API. A signed-in reload shows the "can't reach the server" retry screen. Restart the API and Retry recovers.
- A 500 shows "Reference: req_…" in the UI, and the same ID appears on the server's failure log line and access line.
- During a background refresh failure, the page keeps its data and shows an error with Retry.
- An expired session redirects to sign-in, then back.
- Slow navigation shows a progress bar, lists show skeletons, and mutations show spinners with submission disabled.

**Volume check** for pages over growing tables: seed at volume, `EXPLAIN ANALYZE` the page's queries, and record the timings against the budget ([security-tests.md#performance-budgets](security-tests.md#performance-budgets)).
