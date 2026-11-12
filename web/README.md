# Web console

The React app in `web/` is the Lewbox web console. Vite builds it into `web/dist`, and the Go binary embeds that folder, so `lewbox serve` serves the console, the `/v0` API and the documentation from one origin. The user-facing guide is [Use the web console](../docs/getting-started/web-console.md); this page is for people changing the app.

## Stack

| Piece | What it does |
|---|---|
| React 19 | The UI |
| `react-router` 7, declarative mode | `BrowserRouter` and `<Routes>` in `src/main.tsx` |
| Vite 6 | Dev server and production build (`web/vite.config.ts` is reused by Vitest) |
| Tailwind CSS v4 | Through `@tailwindcss/vite`; styles start in `src/styles/app.css` |
| `@harlew-dev/ui` | The component library, vendored as `web/vendor/*.tgz` |
| `@base-ui/react`, Heroicons, Motion | The library's primitives and the console's icons and transitions |
| `react-markdown`, `remark-gfm`, `github-slugger` | Documentation pages |

The UI library is a local tarball rather than a registry dependency because the Docker build context is this repository only. A published package would need network access and a version to resolve at image-build time; the tarball is copied in with `web/vendor`.

## How it is served

`make` runs the Vite build, then the Go build. The pieces are:

- `web/vite.config.ts` writes the bundle to `web/dist` and puts back `web/dist/.gitkeep`, which keeps the folder embeddable before the first build.
- `web/embed.go` embeds `all:dist` into the binary. `web.DistFS()` returns the embedded folder.
- `internal/webui/webui.go` is the top-level HTTP handler. It sends `/v0` to the API, serves files from the bundle, and answers every other `GET` with `index.html` so the single-page app owns its routes. A missing file under `/assets/` is a 404 rather than the app, so an old hashed bundle does not turn into a script with the wrong MIME type.
- `LEWBOX_WEB_DIR` serves the console from a folder on disk instead of the embedded copy. Restart the server after a build.

The handler also sets the security headers: `Content-Security-Policy` on HTML, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin` and `X-Frame-Options: DENY`. Hashed assets get a one-year immutable cache; everything else is `no-cache`.

## Development

Run the orchestrator and the Vite dev server in two terminals:

```bash
make run     # builds the console and binary, then serves on :8080
make dev     # Vite on :5173, proxying /v0 to 127.0.0.1:8080
```

Open `http://localhost:5173/`. The Vite server proxies `/v0` to the Go server, so sign-in, the API and the docs work in dev. The docs content still comes from the Go server: edit a markdown file and restart `make run`. `LEWBOX_DOCS_DIR` serves the docs from a folder on disk instead of the embedded copy, but still needs a restart after an edit.

`make dev` installs dependencies through the `deps` target first. The file target `web/node_modules/.package-lock.json` runs the install once and again only when `web/package.json` or `web/package-lock.json` changes.

## Authentication

The console never holds a token. `POST /v0/auth/session` sets an `HttpOnly` cookie and returns the user and the expiry, never a token; every later request sends the cookie. `src/lib/api.ts` is the only place that calls `fetch`, and it always uses `credentials: 'same-origin'`.

A `401` from an ordinary request dispatches `lewbox:unauthorized`. `AuthProvider` listens for it, clears the user, and `RequireAuth` sends the browser to `/login` with a `next` parameter. The sign-in, sign-up, sign-out and `/v0/me` calls opt out of that redirect. `AuthProvider` also keeps the account's organizations from `/v0/me`; `reloadOrganizations()` refreshes them after joining, leaving or renaming one.

## Two sides: portal and console

The app has two sides, routed in `src/app.tsx` (`src/main.tsx` only mounts it):

- the **portal** at `/` for customers: Home, Sandboxes, Snapshots, API keys, Usage, Team and Settings, in `src/pages/portal/`. `PortalRoute` loads the current organization, shows the pending or suspended banner, and hands `{org, can, reload}` to the pages through `usePortalOrg()`. The current organization is the one picked in the sidebar's switcher (remembered in `localStorage`), otherwise the first.
- the **console** at `/admin` for instance admins: Overview, Customers, Sandboxes, Plans, Servers, Users and Settings, in `src/pages/admin/`, `src/pages/servers/` and `src/pages/users/`. `RequireAdmin` shows anyone else the not-found page.

Instance admins land on `/admin`; one who belongs to no organization is sent there from `/` too. The old console paths (`/servers…`, `/clusters…`, `/nodes`, `/users…`, `/organizations…`) redirect under `/admin` for admins. Customers never see the words cluster, node or server: clusters are **regions**. People read **workspace** wherever the API and code say organization (`/v0/organizations`, `api.Organization`, `org`). A workspace has a plan per region (a subscription the operator assigns); with none, the portal says the provider will enable one. There is no billing anywhere: usage is shown in vCPU-hours, GiB-hours and sandbox-hours only.

`InstanceProvider` (`src/lib/instance.tsx`) loads `/v0/instance` once: the name in the sidebar and whether sign-up is open (the `/signup` page and the link on `/login`).

## Content Security Policy

The policy is `default-src 'self'`, with scripts, connections, images and fonts limited to this origin. In practice:

- no inline `<script>`, and no scripts from a CDN;
- `connect-src 'self'`, so the app can only call its own `/v0` API;
- fonts and other assets are bundled (`@fontsource-variable/inter` is imported from CSS), not fetched from a third party;
- inline `style` attributes are allowed, but style sheets and scripts from elsewhere are not.

Adding a third-party origin means changing the policy in `internal/webui/webui.go` and explaining why. Prefer bundling the asset.

## Documentation rendering

The Go server parses the embedded `docs/` folder at startup and exposes:

- `GET /v0/docs` — the nav, built from the `##` sections and table rows in `docs/README.md`;
- `GET /v0/docs/<path>` — one page as title, description, markdown, previous and next.

`src/pages/docs.tsx` fetches a page and renders it with `react-markdown` and `remark-gfm`. There is no `rehype-raw`, so raw HTML in markdown is not rendered as HTML. `src/lib/docs-markdown.tsx` supplies the heading ids (one `github-slugger` per page, the ids GitHub would produce), the headings for the `On this page` list, code blocks (the library's `CodeBlock`, with a copy button), GitHub-style alert blocks, and the link component. The typography, the page list and the `On this page` list are library components: `Prose`, `SideNav` and `TableOfContents`. `src/lib/docs-links.ts` resolves a relative markdown link: a page under `docs/` becomes an internal `/docs/...` route, a file elsewhere in the repository becomes a GitHub link, and a URL stays external.

The docs route is outside `RequireAuth`, so `/docs` is public. It uses the library's `DocsLayout` (a header bar, the page list on the left, no inset card), not the console's `SidebarLayout`; `src/components/docs-layout.tsx` fills it in and shows "Sign in", "Dashboard" (customers) or "Console" (instance admins) in the header depending on the current session.

## Add a page

1. Add the route in `src/app.tsx`: inside the `Portal` route for a customer page, inside the `/admin` route for a console page. Public pages (like the docs) go outside `RequireAuth`. Declare the page with `lazyPage(() => import('./pages/…'), 'NamePage')` so it gets its own chunk and the entry bundle stays small; only sign-in, sign-up, not-found and the layouts are imported directly.
2. Add a sidebar item in `src/components/portal-layout.tsx` or `src/components/console-layout.tsx`. Both fill in `AppShell` (`src/components/app-shell.tsx`), which holds the logo, Docs and the account menu.
   Build the page from `@harlew-dev/ui` components (`PageHeader`, `Table`, `Stat`, `ErrorNotice onRetry`, charts from `@harlew-dev/ui/chart`, …). If a piece is missing, add it to the library and re-vendor it (below) rather than writing a component in this app: `src/components/` only holds Lewbox's own wiring (layouts filled in, the account menu, status badges, the sandbox table, limit meters, the usage chart).
3. Add a typed function to `src/lib/api.ts`. It is the only module that talks to the API; return the existing interfaces or add one beside them. Routes that act on one organization take it as `org`, which `request` sends as the `Lewbox-Organization` header; sandboxes and usage also send it as `?org=`.
4. Use `useLoad` for reads (`every` refreshes in the background; `refresh()` reloads without the loading state) and `useAction` from `@harlew-dev/ui` for changes. They handle loading, errors and toasts.
5. Add a test next to the page. `src/test/render.tsx` has `stubAPI` (a routed `fetch` stub that records each call, including its `Lewbox-Organization` header; it answers `/v0/me` with the given user and organizations and `/v0/instance` with a closed instance), `renderAt` (the app's providers around your routes) and `renderApp` (the whole app at a path). `src/pages/portal/keys.test.tsx` uses them. `src/lib/api.test.ts` covers the client itself.
6. If the page is documentation, add it to `docs/README.md` so it appears in the nav, and link it with a relative `.md` path.

## Tests

```bash
make test
```

This runs `gofmt`, `go vet`, the Go tests, the web typecheck (`tsc --noEmit`) and the Vitest suite. The web tests run in jsdom with Testing Library; test files sit next to the code as `*.test.ts` or `*.test.tsx`. `make test` installs dependencies first and does not need a production console build.

## Update the component library

When you change the sibling UI checkout, re-vendor it:

```bash
make update-ui                 # UI_DIR defaults to ../ui
make update-ui UI_DIR=/path/to/ui
```

It builds the UI package, packs it into `web/vendor/harlew-dev-ui-<version>.tgz`, removes the old tarball, and runs the install that points `web/package.json` and the lockfile (including the integrity hash) at the new file. Commit the new tarball with the manifest and lockfile changes.
