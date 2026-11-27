# Routing and navigation

How pages are registered, how they get their data, and what may travel in a URL. File and folder names for pages (`pages/project/[project].<ext>` etc.) and page actions (`index`, `show`, `create`, `edit`) are defined in `naming`; this file does not repeat them.

## The route registry

Every page is registered in one file with its URL. No file-system routing, even if the framework offers it.

```
// routes.<ext>
layout("pages/app/layout", [
  route("app/:tenant/projects",          "pages/project/index"),
  route("app/:tenant/projects/:project", "pages/project/[project]"),
  layout("pages/settings/layout", [               // tabs that are pages
    index("pages/settings/index"),
    route("members", "pages/settings/members/index"),
  ]),
])
route("*", "pages/not-found/index")
```

Why: one place lists every URL a reviewer must think about; the same list feeds the route-pattern catalogue used by error reporting, the `?next=` allowlist and the server's own list of app URLs. With file-system routing a stray file becomes a public page.

- **URL builders** sit beside the registry, grouped by resource with the page actions: `routes.project.index(tenantId)`, `routes.project.show(tenantId, projectId)`, `routes.settings(tenantId)` (the tenant's edit page, named for the product), `routes.project.edit(tenantId, projectId)`. Non-resource pages keep plain names (`routes.dashboard(tenantId)`, `routes.settings`). Components never concatenate URL strings.
- **API paths** live only in the typed API client (one module per resource; `api.projects(tenantId).store(input)`, `paths.projects(tenantId).show(id)`). Nothing else calls `fetch` or writes `/api/...`. Typed paths carry their response type, so `useResource(paths.projects(t).index(q))` infers `Page<Project>` and a path can't be paired with the wrong type.
- **Fixed segments before parameters:** `app/settings` and `app/onboarding` must win over `app/:tenant/...`. Type-prefixed tenant IDs (`wsp_…`) make collisions impossible; keep it that way.
- **When adding a page:** register it, add its URL builder, add its pattern to the `?next=` allowlist if it's a signed-in page, and to the route-pattern catalogue used by client error reporting (the `testing` skill describes the test that keeps those lists in sync).

## Pages fetch their own data

Pages have **no data loaders**. They render on the first frame, show skeletons, and fetch through the resource hook (see `data-loading.md`).

```
// pages/project/index.<ext>
Index():
  tenant = useTenant()
  query  = projectQuery(searchParams)
  list   = useResource(paths.projects(tenant.id).index(indexQuery(query)))
  if no list.data: return list.loading ? <ListSkeleton label="Loading projects"> : null
  render list.data, list.error as <Notice variant="error" onRetry>, an <EmptyState>
```

Why: a loader blocks navigation until the network answers, so every click feels slow, and a failing loader fails the whole page instead of one section. Revisiting a page shows cached data at once and refreshes in the background.

**Common mistake:** "the page needs the record, so I'll load it in the loader". The page needs a skeleton, not the record.

## Loaders: session and redirects only

Loaders run before a route renders. Use them only for what a page truly can't render without:

| Loader | On | Does |
| --- | --- | --- |
| signed-in | the app layout route | loads the session and page 1 of the user's tenants **in parallel, once** (disable loader revalidation), showing a layout skeleton (e.g. React Router's `HydrateFallback`) meanwhile, never a blank screen. 401 → `/auth/login?next=<this page>`; no tenant → onboarding; API unreachable or 5xx → "service unavailable" page with the error's request ID |
| tenant home | `/app` | redirects to the newest tenant's dashboard or onboarding |
| guest | login, register | already signed in → `?next=` or `/app`; if the API is down the form still renders |
| verify-email (if used) | the verification gate | verified, or verification off → onward |

- Loaders use a **read-only client** (no CSRF token), so they can't mutate anything.
- The server should redirect signed-out requests for app URLs to login before serving HTML; the loader's 401 handling covers a session that expires later and dev servers without that gate.
- **The server gate does only that one redirect.** The `next` it adds is a hint the client still validates with `returnPath()`. Onboarding (no tenant), membership and guest-page redirects stay client-side, so the rules live in one place and the server doesn't duplicate them.
- While a navigation waits on a loader for more than 200 ms, show a thin top progress bar (and keep it ≥ 400 ms so it doesn't flash).

## Persistent layouts

All signed-in pages sit inside one **layout route** that stays mounted between pages, including tenant switches. It provides the session and current tenant through context, the sidebar, the menus and the toaster. A page renders only its content.

Why: remounting chrome on every navigation loses sidebar scroll and open state, replays animations and refetches the tenant list.

- The layout fades each new page in, keyed by **pathname** (a new search or page number is not a new page and must not re-animate).
- **A route change updates `document.title` and moves focus to the new page's `<h1>`** (`tabindex="-1"`), so a screen-reader user hears where they landed; scroll to the top on a new navigation and restore it on Back/Forward. Title order is most specific first: "Billing · Settings · Acme". Detail in [accessibility.md#route-changes](accessibility.md#route-changes).
- A tenant already in the switcher's list renders at once while its record refreshes.
- **NEVER copy the session or tenant into module-level variables.** They change on sign-out and switch; module state survives both and leaks across accounts.
- The sidebar marks an item current on its own path and its sub-paths (`/projects/prj_…` highlights Projects) with `aria-current="page"`. Pages reached through a menu (settings) mark the menu's button as selected; don't add sidebar items for settings pages.

## Tabs

| Kind | How |
| --- | --- |
| Tabs that are pages of their own (Settings: General · Members · API keys) | Child routes of a layout that renders the heading and the tab bar. The tab bar stays mounted (its underline slides); only the content below changes. Each tab is a real URL. |
| Tabs on a record page (a member: Overview · API keys · Logs) | `?tab=` on the record's URL. The first tab has no parameter; switching tabs drops the other tab's parameters. |

Each tab is visible only if the user may see it; a tab page the user can't use shows a notice and fetches nothing.

## IDs from the URL

Read a route parameter, then validate it **by the API's own rule** before using it:

```
projectId = parsePublicId("prj", params.project)   // "prj_" + 14 base62, else null
if projectId is null: render <NotFound>             // no request sent
record = useResource(paths.projects(tenant.id).show(projectId))
if record.missing: render <NotFound>                // API said 404 (or not a member)
```

Why: a malformed ID would otherwise reach the API (a 422 or 500 rendered as "something went wrong"), and old numeric IDs or hand-edited URLs should simply be "not found". Non-members get the same "not found" as missing records, matching the API's 404 (see `security`).

## Navigation state for transient data

Data that only needs to survive one navigation goes in the router's navigation state (`navigate(to, { state })`, `history.state`), not the URL:

| Use | Example |
| --- | --- |
| A value typed on one page and prefilled on the next | the email from login carried to "forgot password" |
| A message the next page shows | "Acme deleted." on onboarding after deleting the last tenant; "Your account has been deleted." on login |
| An arrival toast | `{ createdWorkspace: "Acme" }` read by the layout, which toasts "Acme created." |
| A stack the URL shouldn't carry | the newer cursors behind an Older/Newer list |
| A flow flag | `{ invitation: true }` so signing in goes on to accept the invitation |

Why: URLs land in history, server logs, referrer headers, analytics and screenshots. An email in `?email=` is personal data leaking everywhere. Navigation state is gone when the tab closes and is not shareable, which is exactly right for transient data.

## Returning after sign-in (`?next=`)

- Signed-out visits to a signed-in page go to `/auth/login?next=<path and query>`; signing in, and the login ⇄ register switch links, carry `next`.
- `returnPath(next)` accepts only a path that starts with a single `/`, stays on this origin and **matches an allowlisted route pattern** with well-formed IDs. Everything else (`//evil.com`, `/\evil.com`, `https://…`, `/auth/*`, `/api/*`, numeric IDs) falls back to `/app`. Why: an open redirect turns your login page into a phishing tool (see `security`).
- `next` is only ever used for a page navigation, never to submit anything.
- **Add a pattern to the allowlist for each new signed-in page.**

## Links

- **Navigation is a link.** Buttons that go somewhere take `href` and render the router's link; rows link through an overlay. `navigate()` from code only after an action (a save that opens the new record).
- Sortable headers and page links keep the scroll position (`preventScrollReset` or equivalent).
- A link to another build or a full reload target uses a document navigation explicitly.

## Status pages and errors

- **Not found:** a friendly page inside the layout ("This page has wandered off.") linking home. The catch-all route and every `missing`/invalid-ID case render it; the server answers unknown URLs with HTTP 404.
- **Service unavailable:** Retry, and "Reference: req_…" when the failure carried a request ID.
- The root error boundary shows one of those two and reports the crash. Client error reports carry the **route pattern** (`/app/:tenant/projects/:project`), never the path or query, which may hold tokens, searches and emails.

## Public pages

Public pages (home, docs, policies) may be prerendered at build time. They are built without a session, so they hold no user data; a "Log in" link switches to "Dashboard" once the browser finds a session. Which pages to prerender, their cache headers and the SPA trade-offs (a missing record renders "not found" with HTTP 200): `ops/references/web-delivery.md` §1.

## Before you finish

| Pattern | Fix |
| --- | --- |
| `fetch(` or a `"/api/` string outside the API client | A typed path in the API client |
| A URL built by concatenation (`/app/${id}`) in a component | The route's URL builder |
| A `pages/` file with no `route(...)` entry, or file-system routing | One explicit registry entry |
| A loader that loads a page's record | Render the page and its skeleton; fetch through the resource hook |
| `useEffect`/`onMounted` fetching a page's data | The resource hook (`data-loading.md`) |
| `window.location` or `<a href>` without the router for an in-app page | The router's link or `navigate()` |
| A click handler that goes somewhere on a `div`, `span` or `button` | A link with `href` |
| A URL parameter used without `parsePublicId` | Validate it, then render "not found" without a request |
| `?email=`, `?token=` or another transient value in the query | Router navigation state |
| `next` used without `returnPath()` | Same-origin path plus an allowlisted route pattern |
| A module-level `session`, `tenant` or user object | Context |
| `document.title` unchanged after navigation, or focus left on the old page | Set the title and focus the new `<h1>` |
| A page transition keyed by search params | Key by pathname |
| An error report carrying the path or query | The route pattern |
| A new signed-in page missing from the `?next=` allowlist and pattern catalogue | Add both |
