# Frontend unit tests and contract drift

Read this when you write or review component, page, hook or client tests, build the frontend test harness for a new stack, or add a client-side copy of a server allow-list. The examples use Testing Library-style queries, which exist for React, Vue, Svelte, Solid and others. Translate the calls to your framework.

## Layout and setup

- Tests sit **beside the code**: `components/project-table.<ext>` → `components/project-table.test.<ext>`. Moving or deleting the component moves or deletes its test.
- They run in a DOM environment (jsdom, happy-dom). One shared setup file unmounts rendered trees and restores stubbed globals after **every** test, so test files never repeat it and no test leaks into the next.
- Shared helpers live in one test-only module (`src/test/`): the API stub, `testSession(overrides)`, `testTenant(role)`, a router wrapper, `openDangerZone()`. Production code never imports it, and a check enforces that.
- The frontend test runner has its own config, without the app framework's build plugins, so tests don't depend on route generation or server rendering.
- **Stub the browser APIs the DOM environment lacks, once, in the shared setup:** jsdom has no `PointerEvent`, `ResizeObserver`, `matchMedia` or `Element.getAnimations`, and component kits (dialogs, menus, selects, toasts) call them. Stubbing per test file means the first file that forgets crashes in an unrelated-looking way. Put interaction helpers the kit needs (e.g. `pick(option)` for a select, `hover(el)`) in the test-only module, so tests don't each rediscover how the kit wants to be pressed.
- **The per-test timeout sits well above the setup's wait timeout** (e.g. waits time out at 5 s, tests at 15 s). On a slow machine the test then fails on the thing it waited for ("button Save not found"), not on a bare "test timed out" that names nothing. Write the reason beside the setting.

## The API stub table

One helper fakes the HTTP API for every test. Tests never hand-roll `fetch` mocks. Only the transport's own tests use raw responses.

```
api = stubApi({
  "GET /api/tenants/tnt_1/projects?page=2": { body: page2 },       # exact URL wins
  "GET /api/tenants/tnt_1/projects":        { body: page1 },       # then path without query
  "PATCH /api/projects/prj_1": [                                   # replies used in order,
    { status: 422, body: { error: "Too long", field: "name" } },   # the last one repeats
    { body: updated },
  ],
  "POST /api/projects": { body: created, gate: g = gate() },       # held until g.release()
  "GET /api/me":        { network: true },                         # fails as if offline
  "GET /api/export":    { text: "a,b\n1,2", headers: { "Content-Type": "text/csv" } },
  "*":                  { body: {} },                              # optional catch-all
})

api.sent("PATCH")  # → [{ method, url, body, csrf, headers }]
```

| Behaviour | Why |
| --- | --- |
| Keyed by `"METHOD url"`: exact URL, then path without query, then `"*"` | one readable table per test; query-specific answers when they matter |
| A list of replies is used in order, the last repeating | refusal then success, or first load then refresh, without callbacks |
| `gate()` holds a reply until released | assert the pending state (spinner, disabled button, `aria-busy`, one request only) deterministically, with no timers |
| `network: true` rejects like a dropped connection | the offline/unreachable path differs from an HTTP error and needs its own UI |
| **Unmatched requests get 404 naming the missing stub** | an unexpected request is visible in the failure instead of hanging or silently succeeding |
| `sent()` records method, URL, body and CSRF token | assert what was sent: the body, the token only in the body (never the URL), the CSRF header |

**Asserting "no request":** `expect(api.sent()).toEqual([])` after rendering a page for a role that may not read the data, or a malformed link that must not be checked.

## Query by role and accessible name

```
screen.getByRole("button", { name: "Save" })
screen.getByRole("link", { name: "Page 2" })
screen.getByRole("searchbox", { name: "Search projects" })
screen.getByRole("dialog", { name: "Delete project" })
```

- If an element can't be found by role and name, it's probably inaccessible. Fix the markup, don't fall back to `data-testid`.
- Prefer `findBy…` (it waits) over manual waits. Use `queryBy…` only to assert absence.
- **Open collapsed UI before asserting absence.** A closed disclosure, tab panel or danger zone hides its contents from the accessibility tree, so `queryByRole("button", { name: "Leave workspace" })` returns nothing whether or not the button rendered: the assertion passes vacuously. Open the container first through one shared helper (`openDangerZone()`), then assert the button is absent. The same applies to presence: open first, then `getBy…`.
- Drive with user-level events (`userEvent.type`, `click`, `keyboard("{Escape}")`), not by calling handlers.

## Assert behaviour, not styling

Assert: visible text, `aria-current`, `aria-sort`, `aria-busy`, `aria-invalid`, `aria-describedby` content, `disabled`, focus position after open/close, the URL after navigation, callbacks fired, requests sent.

Never assert: class names, colours, spacing, snapshot dumps of markup. The exception is a class that *is* the behaviour (e.g. a responsive "hidden on small screens" class with no other observable effect in the DOM environment). Keep that rare and say why in the test.

## What every component and page test covers

| Area | Cases |
| --- | --- |
| Data views | loading (skeleton, after any deliberate delay), empty (and empty-for-this-filter, with Clear filters), loaded, **failure with Retry**, Retry succeeding, failure during refresh **keeping the loaded data** |
| Mutations | pending (`gate()`): button disabled or spinner, **one request however many clicks**; refusal: the field error on its field, typed values kept, dialog stays open; success: toast/notice, list refreshed, dialog closed |
| Per role | render as each role: actions a role may not take are **absent** (not just disabled); **no request is made** for data the role may not read |
| User text | names, descriptions and payloads containing `<script>alert(1)</script>` and `<h1>x</h1>` render **as text** |
| Navigation | links carry the right URL; `?next=` kept; state in the URL (filters, sort, page) read on load and written on change; bad URL values dropped |
| Session | 401 → login with `next`; 403 "verification required" → verify page; no tenant → onboarding; API unreachable → outage page |
| Secrets in the UI | not in the DOM until revealed; copy works while masked; focus returns on hide |
| Accessibility | focus moves into dialogs and back on close; errors announced (`role="alert"`/status regions); controls named by their visible label |

## Table-driven pure helpers

Pagination maths, ID parsing, URL-state parsing, formatters and filter compression get table tests:

```
cases = [
  ["prj_1a2B3c", { type: "prj", ok: true }],
  ["prj_",       { ok: false }],
  ["PRJ_1a2b",   { ok: false }],
  ["usr_1a2b",   { ok: false, reason: "wrong type" }],
]
test.each(cases)("parsePublicId(%s)", (input, want) => expect(parsePublicId(input, "prj")).toMatchObject(want))
```

## Type-level tests

When the API client is typed (paths, queries, bodies), assert that **wrong usage fails to compile**:

```
// @ts-expect-error: unknown sort key
api.projects.index(tenant, { sort: "colour" })
// @ts-expect-error: path param is a project ID, not a tenant ID
api.projects.show(tenantId)
```

The type checker running in CI makes these real tests. Without them, a widened type silently accepts anything. Use the language's equivalent where it isn't TypeScript.

## Fake timers

- Debounced search (e.g. 300 ms), delayed spinners (show after 200 ms, keep for at least 400 ms), toasts and "Copied" tooltips that clear after N seconds, polling, retry backoff.
- Advance the clock explicitly. **NEVER** `await sleep(…)` in a unit test: slow and flaky.
- Assert the timer is cleared on unmount (nothing happens after the component is gone).

## Routing and data-layer specifics

- Components that render links need a router wrapper (an in-memory router).
- Prefer rendering pages through the framework's route stub **with their real loaders**, so loader redirects and errors are tested with the page.
- Test the data layer's policy itself: a 4xx read is not retried automatically (Retry asks again), a 5xx is retried a bounded number of times, the cache is per user (signing in as someone else never shows the previous user's data), and mutations show the API's refusal message.
- Run root error boundaries in the framework's strict/dev mode, so double-invoked effects can't double-report.

## Contract drift: tests that read the server's source

When the client keeps a copy of something the server decides, a test compares the two **from the server's actual source**:

| Client copy | Compared with |
| --- | --- |
| sortable columns, filter values | the server's sort/filter allow-list |
| role and permission names, what each role may do | the server's permission table |
| event types and groups shown in filters | the server's event catalogue |
| client route patterns used in error reports | the server's list of client routes, and the router config |
| admin list sort keys | the admin handlers' allow-lists |

```
src = readFile("../api/internal/store/projects.<ext>")
serverSorts = extractAllowList(src, "projectSorts")       # a regex or a parser
expect(serverSorts.length).toBeGreaterThanOrEqual(3)       # found something real
expect(new Set(clientSorts)).toEqual(new Set(serverSorts))
```

- **Always assert a minimum count.** A file moved or renamed must fail the test, not make it compare two empty lists.
- **Check both directions.** The frontend test pins the client's list to the server's source; a **server integration test** also requests every key the client can send (each sort key, both directions, each filter value) and expects 200. The first catches a list edited on one side; the second catches a key both lists agree on but the server's parser or query still refuses.
- **Prefer** a generated shared artifact (OpenAPI, JSON schema, generated types) when the stack supports it, and test that the generated file is up to date (regenerate in CI and diff). Source-reading tests then cover only what generation can't.
- The failure message says which side to change and where.

## Common mistakes

- `getByText("Save")` on a div with a click handler. It passes while the control is unreachable by keyboard and screen readers. Query by role.
- Testing that a disabled button *looks* disabled. Assert `disabled` (or `aria-disabled`) and that clicking sends nothing.
- Rendering only as owner. The bugs live in the viewer and member renders.
- A stub with `"*"` returning `{}` for everything, which hides the request the page should never have made. Prefer explicit keys. Use `"*"` only for unrelated background calls.
- Reaching into component state or hook internals instead of the rendered output.
