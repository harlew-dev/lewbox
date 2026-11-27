---
name: structure
description: Where code goes and how it is layered, in any backend or frontend stack — the route registry, per-resource controllers grouped by URL area, thin entrypoints, handler → service/data layers, frontend primitives/layout/feature/lib/types/API-client folders, library wrappers, test placement, the admin bundle and file naming. Use when creating a file, package, module, component, page or endpoint, adding a dependency, moving or splitting code, or deciding "where does this go?".
---

# Project structure

The goal: anyone (human or agent) can predict where a piece of code lives from what it does, and every layer can be read, tested and replaced on its own. A wrong placement is copied by the next change that looks at it, so structure is enforced by tests, not by memory (`conventions` owns how those checks are written).

Names of routes, actions, pages and methods are owned by `naming`; this skill owns **where** things live and **what may import what**.

## When to use

- Adding an endpoint, controller, service, query, migration-backed model or background job.
- Adding a page, layout, component, hook, helper, type or API-client method.
- Adding a third-party dependency, or importing one somewhere new.
- Writing an entrypoint (server, CLI command, migration/seed runner, app root).
- Splitting a large file, moving code between packages, or "this needs to be shared now".
- Anything touching privileged/admin code or test-only code.

Deep detail:
- [references/backend-layout.md](references/backend-layout.md) — read when creating or moving backend packages, controllers, middleware or data-layer code; has the example tree, the request flow and per-stack mappings.
- [references/frontend-layout.md](references/frontend-layout.md) — read when creating or moving pages, components, hooks, the API client or the admin build; has the example tree and the promotion rules for components.
- [references/structure-checks.md](references/structure-checks.md) — read when setting up a new project or adding a layer/area: the tests and lint rules that enforce this skill.

## 1. One route registry

- **MUST** declare every API route in **one** registry file (e.g. `router/routes.<ext>`, like Laravel's `routes/api.php`), with its method, path, middleware chain, permission and rate limit visible on the same line or group. Page routes (URL → page file) have their own single registry on the frontend (`routes.<ext>`).
- **NEVER** declare routes in an entrypoint (`main.<ext>`, `server.<ext>`, `root.<ext>`, `app.<ext>`), in a controller file, or by decorators scattered across files when the framework offers an explicit table.
  - Why: the registry is the security review surface. A reviewer must see in one place that `POST /projects` has session + CSRF + permission + limit. Routes hidden in controllers or entrypoints are routes nobody audits.
- **MUST** register a controller's action, never an anonymous/bare handler function: `projects.store`, not `func(w, r) { … }` inline.
- **Prefer** a `group(prefix, middleware…)` helper so a group's protection is declared once (`/api/workspaces/{workspace}/*` → session → tenant membership).
- Page URL *constants* live in one module (`lib/routes.<ext>`); **NEVER** hand-write a page path string in a component. API paths live only in the API client (§5).

## 2. Controllers: one per resource, grouped by URL area

- An **area** is the URL's first segment after `/api`, named singular: `/api/workspaces/*` → `workspace`, `/api/admin/*` → `admin`, `/api/auth/*` → `account`. Rename an area if it would collide with a core package (`auth` area vs `auth` primitives package → `account`).
- Small groups of routes join the closest existing area rather than founding a new one for one or two routes (signed-out link pages live with `account`).
- **One controller per resource, one per file**, named after the resource: `endpoints/workspace/projects.<ext>` holds `Projects` with `index`, `show`, `store`, `update`, `destroy` (actions per `naming`). The file also holds that resource's input types and validation. Helpers stay private to the file/module.
- An area module **exports only its controllers**.
- **Areas NEVER import each other.** Only the registry imports areas. When two areas need the same code, **move it down**: into the HTTP core (server, middleware, JSON responses, sessions) or into a **leaf package** that imports nothing of the app (`input` parsing, `ratelimit`, `clientip`).
  - Why: cross-area imports create cycles and make "admin" code reachable from "workspace" code. Pushing shared code down keeps the dependency graph a tree you can draw.
- **A resource gets its own folder only once it has sub-resources.** `endpoints/workspace/projects.<ext>` until projects gain tasks and comments; then `endpoints/workspace/project/` holds `projects`, `tasks`, `comments`. Until then an area is one flat folder.
  - Common mistake: creating `project/index.<ext>` "for later". An empty hierarchy is noise and every reader has to open it.

## 3. Entrypoints are composition only; each package owns one concern

- **Entrypoints** (`cmd/server`, `bin/www`, `manage.py`, `main.<ext>`, the frontend `root.<ext>`) only: load config, construct dependencies, wire them together, start, and shut down gracefully. Separate entrypoints for migrate, seed, prune and operator commands each parse flags and call into packages.
  - **NEVER** put business rules, SQL, route declarations, or HTML/UI in an entrypoint.
  - The production server entrypoint **NEVER** imports factories, seeders or test helpers (enforce with a test, see references).
- **Each package/module owns one concern**, and the owner is the only place that concern is implemented:

  | Concern | Owner (example name) |
  | --- | --- |
  | Environment loading and validation | `config` |
  | DB connection, migrations runner, advisory locks, query timing hook | `database` |
  | Models and every query | `store` (the data layer) |
  | Password hashing, tokens, digests, cookies, OTP, sealing secrets | `auth` (primitives, no HTTP) |
  | Transport: server, middleware, JSON responses, request context | `http` core |
  | Controllers | `http/endpoints/<area>` |
  | The route registry | `http/router` |
  | Logging handler, request IDs, error scrubbing, metrics | `observe` — imports **nothing** else of the app |
  | Email sending and templates | `mail` |
  | Domain events, listeners, delivery worker | `events` |
  | Fake records / seed data | `factory`, `seed` (never in production paths) |
  | Serving built frontend assets | `web` |

- **Prefer** a package whose description is one sentence without "and also". If you need "and", it is two packages.
- **NEVER** name a package `base`, `util`, `common` or `helpers`. Stateless helpers are package functions; only code needing dependencies is a method on the server; export only what another package uses. Breaking import cycles and helper shape: [references/backend-layout.md](references/backend-layout.md).
- **Removing duplication:** first list each repeat site with the proposed helper and the lines it saves, ordered by value for risk; then one commit per step with behaviour unchanged. Add a shared helper when the **third** case appears, not the second. **NEVER** centralise:
  - a wrapper that hides a transaction boundary (the transaction is what makes the outbox correct; hiding it invites a second write outside it);
  - look-alikes that differ on purpose (an API guard answering 401 JSON vs a page guard redirecting before any HTML);
  - validators that each mirror their own column's CHECK;
  - a catch-all error mapper (e.g. every "no rows" → 404), which turns an unrelated missing row deep in a call into a misleading 404.

## 4. Backend layers: handler → service → data

```
request → middleware (security, session, tenant, permission, limit)
        → controller action (parse + validate input, call down, shape response)
        → [service: business rules spanning several queries/side effects]
        → store/data layer (queries, scoped by the authorised tenant)
        → JSON response
```

- **SQL lives ONLY in the data layer** (plus migration files and the few DB-level statements the `database` package owns: creating schemas, advisory locks). **NEVER** write SQL in a controller, middleware, service, listener, CLI command or test helper.
  - Why: one place to audit parameterisation and tenant scoping (`security`), one place to index-tune, one place to mock or test against a real database.
- Controllers **NEVER** reach into the database driver; they call named data-layer methods (`IndexProjects`, `FindProject`).
- Controllers reach shared dependencies through the server object they embed/receive (`h.repo`, `h.config`), not through globals.
- Request-scoped values (session, tenant, admin) are set by middleware in the request context with **private keys**, read through accessors (`Session(r)`, `Tenant(r)`). Only core middleware can set them, so a controller can't forge a session.
- **Prefer** a service layer only when a rule spans several data calls or side effects (transaction + event + email). Don't create a pass-through service per controller.
- Side effects (email, webhooks) go through events/listeners, not directly from the handler (`api`, `ops`).

## 5. Frontend layers

| Layer | Folder | Holds | May import |
| --- | --- | --- | --- |
| Page registry | `routes.<ext>` | URL → page file | pages |
| App root | `root.<ext>` | document, providers, error boundary | everything below |
| Pages | `pages/…` | one page per folder, its local UI | all below |
| Primitives | `components/ui/` | generic reused UI (button, input, dialog, table, badge, notice) | lib, types, third-party UI package |
| Layout | `components/layout/` | page chrome (application, auth, sidebar layouts) | ui, lib |
| Feature components | `components/app/<feature>/` | domain UI shared by **two or more** pages | ui, layout, lib, types |
| Lib | `lib/` | non-visual helpers, hooks, session/tenant access, loaders, formatting | types |
| API client | `lib/api/<resource>.<ext>` | every API path and call for one resource | types, the HTTP helper |
| Types | `types/<resource>.<ext>` | domain types and constants per resource | nothing |
| Styles | `styles/` | global CSS entry, element defaults | — |

- **`fetch` (or axios, `XMLHttpRequest`, any HTTP call) lives ONLY in the API client.** **NEVER** call it or write an `/api/...` string in a page, component or hook. Components call `api.projects(ws).store(input)`; cache keys use the client's typed paths (`paths.projects(ws).index()`).
  - Why: CSRF headers, error shapes, retries, challenges and auth redirects are handled once. A stray `fetch` silently skips them.
- Session and current tenant are read through one hook/store each (`useSession()`, `useWorkspace()`), never re-fetched ad hoc.
- Shared route loaders live in `lib/loaders.<ext>`.
- Pages follow the folder rule in `naming` (every page is a folder with `index.<ext>`; a record page is `[param].<ext>`).

## 6. Components exist only for reused UI

- **NEVER** create a component file for UI used by one page (`DashboardPage`, `ProjectHeaderSection`). Page-only UI stays **in the page file**, split into **named local functions** (`function MembersTable()`, `function DangerZone()`).
- When a second page needs it, **move** it: generic → `components/ui`; chrome → `components/layout`; domain → `components/app/<feature>`.
- **ALWAYS** reuse or extend an existing primitive before writing new markup or styles. **NEVER** add a second component kit beside the one the project uses.
- Alternatives are a `variant` prop (`<Notice variant="error">`), never `tone`, never one component per look (`ErrorBox`, `InfoNotice`). A wrapper that only binds data to a variant (`ErrorNotice` taking an action's `error`) is allowed (`frontend`).
- A primitive file groups a family of named exports: `fieldset.<ext>` exports `Fieldset`, `Legend`, `Field`, `Label`, `Description`, `ErrorMessage`.

## 7. Each third-party library is wrapped by exactly one module

- **MUST** import a third-party library from **one** wrapper module (or, for a component kit, only from the primitives layer, each kit part wrapped by exactly one primitive; component source copied into the repo is itself the primitives layer and needs no extra wrapper). Everything else imports the wrapper.
  - Charting library → only `components/ui/chart.<ext>`. QR library → only `components/app/mfa/qr-code.<ext>`. Markdown lexer → only a build-time module. Third-party UI package → only `components/ui/*`, `components/layout/*`.
- **MUST** enforce it with a lint rule (`no-restricted-imports` or equivalent) so the next import elsewhere fails.
- Why: swapping or upgrading the library touches one file; bundle splitting keeps a heavy library out of builds that don't need it; the CSP/security review of what it does (inline styles, `eval`, network) is done once; behaviour (focus return, keyboard) stays consistent.
- Every new dependency: record licence, pinned version, why, and which module wraps it in the frontend/backend README's Dependencies section; run the audit. Fix type errors properly, **NEVER** silence them with `any`/unsafe casts.
- **Choosing or replacing a library** — weigh, and write down in the plan:
  - maintenance (release cadence, an OpenSSF Scorecard "Maintained" score, a README that says engagement is winding down), licence, provenance (SLSA);
  - direct **and transitive** dependency counts (one swap took about 130 packages down to 5 direct), and whether it needs a provider, style engine or compatibility `overrides`;
  - whether its peer range includes your framework's current version;
  - the audit and the gzipped entry-chunk size, before and after;
  - security fit: no injected `<style>` elements and no parts that ship inline scripts (both loosen the CSP, see `ops/references/web-delivery.md` §4).
- **Migrating:** run a **spike first** on a throwaway branch with explicit go/no-go questions; keep the wrappers' public APIs stable so pages don't change; move in shippable steps, with coupled parts (all overlays) in one step; ban the old library with a lint rule whose message points at the doc; remove it and its `overrides` **last**.
- Import each part from its **own subpath** (`kit/dialog`, not the barrel) when the package is side-effect free, so builds that don't use a part (the app build vs the admin build) never bundle it.

## 8. Tests sit beside the code

- **ALWAYS** put a unit/component test next to the file it tests: `projects.<ext>` + `projects.test.<ext>`; `pages/auth/login/index.<ext>` + `index.test.<ext>`.
- A test spanning several pages or packages sits at the root of the area it spans (`pages/admin/admin.test.<ext>`).
- Test helpers shared across packages go in a dedicated test-only module (`apitest`, `src/test/`), which **only tests import**.
- End-to-end tests live in their own top-level folder (they test the whole system, not one file). See `testing`.

## 9. Privileged/admin code in its own bundle

- Admin/operator UI is a **separate frontend build** with its own entry, routes list, API client (`lib/api/admin.<ext>`), route constants (`lib/admin-routes.<ext>`) and types (`types/admin.<ext>`).
- **NEVER** import admin modules from non-admin code, statically or dynamically. Three guards enforce it (lint, bundler plugin, output grep): see `ops/references/web-delivery.md` §5.
  - Why: a shared bundle ships the admin page list, endpoint paths and header names to every anonymous visitor — a free map of the most privileged surface.
- On the backend, admin controllers are their own area; it is the **only** area allowed to query across tenants, and its routes are registered in their own registry group behind admin middleware (`security`).

## 10. Test-only and dev-only code never ships

- Factories, seeders, fixtures, test helpers, mock servers, demo data and dev-only routes **NEVER** reach the production build or binary.
- Backend: the server entrypoint does not import `factory`, `seed`, or test helpers — checked by a test that walks its imports.
- Frontend: build-time-only modules (reading the filesystem, parsing markdown) carry a marker the bundler excludes (e.g. a `.server.<ext>` suffix). How they fail the build and feed the server: `ops/references/web-delivery.md` §6.
- **NEVER** hard-code demo records in UI; demo data comes from an explicit seed command that refuses production.

## 11. File naming and size

- **Files are kebab-case**: `confirmation-dialog.<ext>`, `use-delayed-loading.<ext>`, `project-table.<ext>`. Applies to components, lib, types and test helpers; pages follow `naming` (`index.<ext>`, `[param].<ext>`, `layout.<ext>`).
  - Exception: where the language makes file names part of its semantics (Python modules must be importable identifiers; Go's `_test` suffix), follow the language and record the choice in the docs once.
  - Why: one rule works on case-insensitive file systems and never fights the import path.
- **Keep files focused.** Split business rules, persistence, HTTP boundaries and presentation into named functions and small modules. **NEVER** write a single-file app, a 1,000-line page, or a "utils" grab-bag.
- **NEVER** write dense one-liners that chain several operations, nested ternaries, or clever expressions to save lines. A named intermediate variable or function is cheaper than the next reader's confusion.
- **Prefer** splitting when a file holds two concerns, or when you need to scroll to find a function's callers in the same file.

## Pre-merge checklist

- [ ] Every new route is in the one registry, as a controller action, with its middleware visible; none in an entrypoint.
- [ ] New controller: one resource, one file, in the area matching its URL; area exports only controllers; no area imports another.
- [ ] Shared code moved down to the core or a leaf package, not imported sideways.
- [ ] No new resource folder unless the resource has sub-resources.
- [ ] Entrypoints only compose; the production entrypoint imports no factory/seed/test helper.
- [ ] SQL only in the data layer; `fetch`/HTTP only in the API client; no `/api/...` strings elsewhere.
- [ ] New component is used by 2+ pages (else it stays in the page file as a named local function); existing primitives extended rather than duplicated.
- [ ] Each third-party library imported by exactly one wrapper module, enforced by lint; dependency recorded and audited.
- [ ] Tests beside the code they test; shared helpers only in the test-only module.
- [ ] Admin code only in the admin build; app build check passes.
- [ ] Files kebab-case; no giant files or dense one-liners.
- [ ] Structure checks pass (`references/structure-checks.md`); docs describing the layout updated in the same change (`conventions`).
