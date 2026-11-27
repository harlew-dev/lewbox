# Web delivery: building and serving the frontend

Read when changing the frontend build, the bundler config, the container build, how the server serves pages and assets, the CSP pipeline, the admin build, or the dev proxy. It ties together what `security` (headers, CSP policy), `structure` (where admin and build-time code lives) and `frontend` (routing) each touch.

The shape this assumes: a client-rendered app (SPA) whose public pages are prerendered at build time, served by the backend from the same origin as the API.

## 1. Prerender only public pages

- **Prerender** the pages that need full HTML for search engines and first paint: home, docs, pricing, policies. Every other URL gets one **SPA shell** (e.g. `__spa-fallback.html`, `index.html`) that renders the root and loads the page in the browser.
- **NEVER prerender a page that needs a session.** Prerendered HTML is built without one, so it holds no user data; signed-in state is fetched after load (a "Log in" button becomes "Dashboard"). See `security` (no user data in prerendered output).
- Why not per-request server rendering: it brings back a second runtime (e.g. Node beside the backend) and the server-side authenticated fetches that leak one user's data into another's response. Choose it only when a page truly needs per-request content.
- CSS that a UI library injects at runtime must be **collected into the prerendered HTML at build time**, or the page is unstyled until scripts run.
- Write the trade-offs down in the docs, because they surprise people:
  - a **missing record renders "not found" with HTTP 200**: the shell was already sent before the browser learned the record is gone (unknown URLs still get 404, §3);
  - prerendered pages change only on rebuild;
  - every navigation to an uncached page shows skeletons, not data.

## 2. One origin, the build inside the server

- **Prefer embedding** the built frontend in the server binary (`//go:embed`, `include_bytes!`, a packaged resource directory), or at least serving it from the **same origin** as the API.

  | | Embedded | Folder on disk | CDN / static host |
  | --- | --- | --- | --- |
  | Deploy | one artefact | binary + folder that must match | binary + separate upload |
  | UI ↔ API version | always match | can drift | can drift; cached HTML calls a newer API |
  | CORS | none | none | a second origin to secure |

- Why: the typed API client and the handlers change together. Shipping them as one unit removes a class of version-mismatch bugs, and one origin means no CORS (`security`). A CDN can still cache the immutable assets in front.
- The build is copied into a **gitignored** folder the backend embeds. A **committed placeholder** (`.gitkeep` or a stub `index.html`) lets the backend compile and its tests run without a frontend build.
- Remove tooling files from the installed build (e.g. the bundler's `.vite` manifest folder) and **never serve dot-files**: build manifests the server reads at startup are named `.something.json` for exactly that reason.
- **Startup fails if the shell is missing** in an environment that expects it, and if an admin key is configured without the admin build (`admin-and-privileged-actions.md`). A half-deployed build is worse than none.
- In the container build, the **web stage runs the leak guard (§5)** after building, so a leak fails the image build. The server stage copies each build into its own embed folder; the runtime stage holds only the binaries and runs non-root (`deployment.md`).

## 3. Cache headers per page type

Pages get **their own header middleware**, separate from the API's: assets must be cacheable, the API must not.

| Path | Status | `Cache-Control` | Why |
| --- | --- | --- | --- |
| Content-hashed assets (`/assets/*`) | 200 | `public, max-age=31536000, immutable` | The name changes when the content does, so caching forever is safe |
| Prerendered public HTML | 200 | `no-cache` | Shared and session-free, but must revalidate so a deploy shows at once |
| Other public files (favicon) | 200 | `no-cache` | Same name across deploys |
| SPA shell for app and auth pages | 200 | `no-store` | Behind a session gate; never kept by a shared cache or the back button |
| Unknown page URL | **404** | `no-store` | Serve the shell with status 404; the app renders its not-found page |
| Unknown `/api/*` | 404 | `no-store` | The API's own 404, never the HTML shell (a client expecting JSON must not get a page) |
| Admin build files | 200 | `private, …, immutable` | **Never `public`**: a shared cache would hand admin code to anyone (`admin-and-privileged-actions.md`) |

- The common mistake: one `no-store` middleware for everything (assets re-downloaded on every visit), or one `immutable` rule for everything (users stuck on old HTML after a deploy).
- Signed-out requests for app URLs are redirected to login **before** the shell is served, so anonymous visitors never download it. The redirect's `next` is a hint the client validates (`frontend`).

## 4. CSP hashes from the built HTML, failing closed

The policy itself (directives, no `'unsafe-inline'` scripts, third-party rules) is in `security/references/csrf-and-headers.md`. The pipeline:

- Compute the hashes **from the built HTML, once**: at build time (written to a manifest) or when the server loads the embedded build at startup. **Never per request.** For each HTML file, tokenise it (a real HTML tokeniser, not a regex), take every inline `<script>` without `src`, and hash its exact text (SHA-256, base64), as the browser does.
- **Each file gets its own policy** (`script-src 'self' 'sha256-…' …`), sent with that file. A file with no inline scripts gets plain `script-src 'self'`.
- **Fail closed:** if an HTML file can't be read or parsed, the build or startup **fails**. Never fall back to a looser policy or serve a policy computed from nothing, which would block the app or, worse, be widened by hand to "fix" it.
- Why hashes, not nonces, for prerendered HTML: the bytes never change after the build, hashes need no per-response rewriting, and a new inline script is allowed automatically after a rebuild while an injected one isn't.
- Stop libraries injecting `<style>` elements where they offer a switch, and avoid library parts that ship inline scripts; both force a looser policy.
- Test it: a changed inline script produces a new hash; every HTML response carries its own file's policy; the end-to-end suite on the production build sees no CSP violations. Make the page server take a file-system interface (`fs.FS`, a virtual FS) so unit tests use an in-memory build.

## 5. Main app and admin as separate bundles

Admin (or any privileged) UI is a **second build from the same package**, not a second package (which duplicates dependencies and the lockfile). Where admin code lives is `structure`; how admin assets are gated is `admin-and-privileged-actions.md`.

- One switch picks the build (e.g. `WEB_BUILD=app|admin`), read in **one helper** that every config imports, and an **unknown value throws**, so a typo fails every step instead of silently building the app.
- The admin build has its own asset base (e.g. `/admin-assets/`), so the server can gate its files apart from `/assets/`; no prerendering.
- **Three leak guards on the app build, all required**, because each misses what the others catch:

  | Guard | Catches | Misses |
  | --- | --- | --- |
  | **Lint**: a restricted-imports rule on admin paths, plus a syntax rule for dynamic `import()` and a ban on `import()` of a template literal | the import at edit time | `../` paths, extensions, aliases it doesn't know |
  | **Bundler**: a plugin that fails the app build if any bundled module id is an admin module | every import form (alias, relative, `..`, extension, dynamic) | pasted strings |
  | **Output grep**: after building, case-insensitive literal grep of every output file, **binary files included**, for the admin URL prefix, the admin API path, the admin header name and the admin asset base | anything that isn't a module, e.g. a pasted URL | nothing it's told about |

- The admin module list exists in two tools (lint config and bundler helper): cross-reference them by comment, and test the bundler guard against each import form. The grep script has its own self-test (every marker, any case, text and binary file) run by the static checks.
- Run the grep **before** copying the build into the server, in the build script and in the container build.
- Links from the admin build to app pages must be **full document navigations**; a client-side link lands on the admin build's own not-found route.
- Known tooling limit: route type generators often take one route list, so dev servers may know both builds' routes. The split is then only proven on real builds, so end-to-end runs against the built server, never the dev server.

## 6. Build-time-only modules

- A module that reads files or parses content during the build (policy Markdown, docs pages) carries the bundler's **build-only marker** (e.g. `.server.<ext>`), so it, its parser library and filesystem access never reach a browser bundle. Placement is `structure` §10.
- Make it **fail the build** with file and line on anything the page can't render safely, so a content mistake is found before deploy.
- When the server needs facts about that content (a document's version, whether it is still the template), the build emits a **dot-file manifest** (e.g. `.policies.json`) the server reads at startup, rejecting unknown fields. Production refuses to start on a template policy.
- Non-secret build values (app name, release) are baked in with the bundler's `define`. In a multi-stage container build, pass each as a build arg to **every stage that uses it** (the web stage bakes it into pages; the runtime stage uses it for emails), or the two disagree. **Never bake secrets** into a frontend build: everything in it is public.

## 7. The dev proxy

- In development the browser talks to the frontend dev server only (one port, `strictPort` so it never silently moves). It proxies `/api` to the backend **with `Origin` unchanged**, and the backend's allowed origin in development is the dev server's origin. Why: the browser then sees one origin, exactly as in production, so cookies, CSRF and Origin checks behave the same. Rewriting `Origin` in the proxy would hide CSRF bugs until production.
- A second dev server for the admin build binds to **127.0.0.1** and is reached only through the main one's proxy (websockets included, with the hot-reload client port pointed at the main port).
- **Bind dev servers to 127.0.0.1 by default.** Binding to `0.0.0.0` exposes the dev app's code, admin pages included, to everyone on the network; if a phone on the LAN needs it, do it deliberately and say so in the docs.
- The session gate, CSP and cache headers run only when the backend serves the build, so verify them against the built server (a local production-like run and end-to-end), not the dev server.

## Checklist

- [ ] Only session-free public pages are prerendered; runtime CSS is collected into them; the SPA trade-offs are documented.
- [ ] The build is embedded (or same-origin), copied into a gitignored folder with a committed placeholder; dot-files are never served.
- [ ] Hashed assets `immutable`, prerendered HTML `no-cache`, shell and API `no-store`, unknown pages 404 with the shell, admin files `private`.
- [ ] CSP hashes come from the built HTML per file; an unparseable file fails the build or startup; a test proves a changed script changes its hash.
- [ ] The app build has all three admin guards (lint, bundler, output grep with self-test), and the container build runs the grep.
- [ ] Build-only modules carry the marker and fail the build on bad content; build values reach every container stage; no secrets baked.
- [ ] The dev proxy keeps `Origin`; dev servers bind to 127.0.0.1.
