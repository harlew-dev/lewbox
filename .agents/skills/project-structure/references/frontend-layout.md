# Frontend layout

Read when creating or moving pages, components, hooks, the API client, types, or anything in the admin build.

## Example tree

```
web/src/
  routes.<ext>                    page registry: URL → page file (one list per build: app, admin)
  root.<ext>                      HTML document, providers, default title, error boundary
  pages/
    home/index.<ext>              /
    auth/
      login/index.<ext>           /auth/login
      login/index.test.<ext>      its test, beside it
      register/index.<ext>
      register/verify/index.<ext> a page beneath another is a deeper folder
      invitations/[token].<ext>   a record/link page
    app/
      layout.<ext>                signed-in layout route (stays mounted between pages)
      settings/index.<ext>        account settings
      [workspace]/                tenant-scoped pages (tenant = workspace, organisation, team…)
        dashboard/index.<ext>
        project/index.<ext>       list page
        project/[project].<ext>   one record's page
        settings/index.<ext>
        settings/members/index.<ext>
    admin/                        admin build only
      layout.<ext>
      [key]/users/index.<ext>
  components/
    ui/                           primitives: button, input, fieldset, dialog, confirmation-dialog,
                                  dropdown, listbox, table, badge, notice, heading, text, chart…
    layout/                       chrome: application-layout, auth-layout, sidebar-layout, admin-layout
    app/
      projects/                   feature UI shared by 2+ pages: table parts, project dialog, editor hook
      workspaces/                 avatar, create dialog, role badge, settings tabs
      mfa/                        qr-code, setup steps, code step (used by settings, login, admin door)
      admin/                      admin-only feature UI
  lib/
    api/
      client.<ext>                the one HTTP helper: fetch, CSRF header, error shape, auth redirect
      projects.<ext>              api.projects(ws).index/show/store/update/destroy; paths.projects(ws)…
      workspaces.<ext>
      admin.<ext>                 admin build only
    routes.<ext>                  page URL constants (routes.project.show(ws, prj))
    admin-routes.<ext>            admin build only
    session.<ext>                 useSession()
    workspace.<ext>               useWorkspace() — the current tenant
    loaders.<ext>                 shared route loaders
    format.<ext>                  formatDate, formatDateTime
    use-action.<ext>              mutation hook (pending, error, duplicate-submit guard)
    use-resource.<ext>            data-fetching hook over the client
    policies.server.<ext>         build-time only (marker keeps it out of browser bundles)
  types/
    projects.<ext>                domain types and constants per resource
    workspaces.<ext>
    admin.<ext>                   admin build only
  styles/
    globals.css                   imports theme + base
  test/                           test-only helpers (pick, hover, render-with-providers)
```

Page folders follow `resource-naming` (singular resource folder, `index.<ext>`, `[param].<ext>`, `[param]/edit.<ext>`, delete is a dialog never a page). Framework file-based routing (Next.js, Nuxt, SvelteKit) still gets one place listing every URL: either the explicit `routes.<ext>` or, where the framework forbids it, a test that lists the routes from the file tree and compares against `lib/routes.<ext>`.

## What goes where: decision table

| You are writing | Put it in |
| --- | --- |
| UI used by one page | that page's file, as a named local function |
| Generic UI (no domain words) used by 2+ pages | `components/ui/<kebab-name>.<ext>` |
| Page chrome (sidebars, nav, auth frame) | `components/layout/` |
| Domain UI used by 2+ pages | `components/app/<feature>/` |
| A hook or helper with no markup | `lib/<kebab-name>.<ext>` (hooks `use-<thing>.<ext>`) |
| An API call or API path | `lib/api/<resource>.<ext>` — nowhere else |
| A page URL | `lib/routes.<ext>` constant |
| A domain type or enum shared by client and components | `types/<resource>.<ext>` |
| A route loader used by several routes | `lib/loaders.<ext>` |
| A helper only tests use | `test/` |
| Anything admin-only | the admin paths listed below |

## Promoting page UI to a component

1. Page-local UI starts as `function MembersTable()` in the page file.
2. A second page needs it → move it (with `git mv` if it's a whole file, otherwise cut/paste) to the right `components/` folder, give it typed props using shared domain types, and import it from both pages.
3. Move its test with it.
4. **NEVER** pre-emptively create the component "because it might be reused".

Common mistakes:
- `components/dashboard-page.<ext>` or `components/project-header-section.<ext>` used by a single page.
- Copy-pasting a button variant instead of adding a `variant` to the existing primitive.
- A feature component in `components/ui` (it mentions projects or members → it's `components/app/<feature>`).

## Primitive rules

- Keep each primitive's established props when extending it (`<Button variant href loading>`, `<Dialog open onClose size>`). Add a `variant` for alternatives.
- Accept a class or style prop for placement/spacing only; the primitive merges it so callers can't break its look.
- Style from the project's design tokens; map variants to complete style names, never interpolated ones.
- Primitives own accessibility once: keyboard navigation, visible focus, accessible names, labels, announced errors/status, reduced motion. See `ui-patterns`.
- A third-party UI package (component kit, behaviour primitives) is imported only by `components/ui`, `components/layout`, `lib` and `root` (lint-enforced). Pages use our components, never the package directly. Component source copied into the repo from a registry lives under `components/` and counts as our components.

## The API client

```
// lib/api/projects.<ext>
paths.projects(ws).index()        -> "/api/workspaces/{ws}/projects"
paths.projects(ws).show(prj)      -> ".../projects/{prj}"
api.projects(ws).index(query)     -> GET  paths.projects(ws).index()
api.projects(ws).store(input)     -> POST paths.projects(ws).store()
api.projects(ws).update(prj, in)  -> PATCH ...
api.projects(ws).destroy(prj)     -> DELETE ...
```

- One file per resource; methods and typed paths use the API's action names (`resource-naming`).
- Only `lib/api/client.<ext>` calls `fetch`; resource files call it.
- Cache keys (SWR, TanStack Query, Vue Query, Svelte stores) are built from `paths.*` and include the user ID, so one user's cache is never shown to the next (`security-defaults`).
- **Typed paths carry their response type.** In TypeScript: `type ApiPath<T> = string & { readonly response?: T }`, so `useResource(paths.projects(ws).index())` infers `Page<Project>`. Nobody writes the type by hand, and a path can't be paired with the wrong one. The field is compile-time only (a phantom type).
- **A resource client implements only the methods the API has.** Leave an unsupported one out (or commented with a pointer to the missing endpoint), never a stub that 404s at runtime.

## The admin build

Admin code is only in: `pages/admin/**`, `components/app/admin/**`, `components/layout/admin-layout.<ext>`, `lib/admin*.<ext>`, `lib/api/admin.<ext>`, `types/admin.<ext>`.

Enforced by three guards (lint, bundler plugin, output grep), all required because each catches what the others miss: see `production-hardening/references/web-delivery.md` §5.

Libraries only admin pages use (charts, calendars) are wrapped in a primitive that only admin pages import, so they never reach the app bundle either.

## Build-time and test-only modules

- A module that reads files or parses content at build time carries the bundler's server/build-only marker (e.g. `.server.<ext>`) and is only imported by loaders/config. Build rules: `production-hardening/references/web-delivery.md` §6.
- Test helpers (`test/`, mocks, fixtures) are imported only by `*.test.<ext>` files (lint rule).
- Dev-only conveniences (seeded demo data, debug panels) are never in components; demo data comes from the backend seed.
