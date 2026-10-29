# Dashboard

> Status: Draft · Milestone: Alpha (M4) · Updated: 2026-10-29

The dashboard is the web app people use to run a Lewbox project: its clusters and nodes, profiles and their keys, sandboxes and everything inside them, templates, webhooks and settings. It also includes the hosted setup pages a profile's owner uses to sign in their agents, connect Git, add SSH keys and set up telemetry. It moves from Svelte and Inertia, inside the Laravel app, to a **React single-page app** built with Vite and **embedded in the `lewbox` binary** (decision D6).

## Today

The legacy dashboard is part of the Laravel app (`lewbox-legacy/resources/js`):

- **Stack:** Svelte 5 with Inertia 3, Tailwind 4, bits-ui, xterm.js and layerchart, built by Vite 8 / vite-plus. Laravel renders each page's props on the server; there is no client-side data layer.
- **52 pages** in `resources/js/pages/**`: auth, docs, setup links, the tenant picker, and the tenant dashboard (sandboxes, templates, profiles, clusters, settings). The full inventory, and where each page goes, is in [the page map](#page-map).
- **Two data paths.** Most data comes from Inertia props, rendered by Laravel controllers in `app/Http/Controllers/Tenant/*` from a third set of resources (`app/Http/Resources/*`), next to the Admin API's and the gateway's. Everything that happens *inside* a sandbox (terminal, sessions, files, ports, conversations) goes from the browser **straight to the gateway** with a sandbox key the app mints (`resources/js/lib/gateway.ts`).
- **"Live" means polling.** Inertia reloads props every 2 s (agent sign-ins), 5 s (credit transactions) and 30 s (node metrics). There is no push channel (`BROADCAST_CONNECTION=log`). Sandbox status changes show up only on reload.
- **Docs are served by the app** (`docs/Show.svelte`, `DocsLayout.svelte`), in three sets: `/docs`, `/api/docs` and `/api/private/docs`, with `.md` variants and `/llms.txt`.

Problems:

1. **Three serialisers for one sandbox** (dashboard resource, Admin API resource, gateway resource), which drift.
2. **Two auth paths in one page.** A sandbox page renders from the app with a session, then talks to the gateway with a sandbox key the page has to keep renewing.
3. **No live updates** except polling, so status, operations and builds lag.
4. **Svelte** while Lewagent, our largest product, is React. Components, the chat UI and the terminal are written twice.

## Goals

- **One API.** The dashboard is a client of the same API that admin keys use, with a session cookie instead of a key ([api](api.md)). No dashboard-only endpoints, apart from sign-in and the session, which the API also owns.
- **Every legacy page has a home**, apart from the docs, which move to their own site ([sdks-and-cli](sdks-and-cli.md#docs)).
- **Live by default.** Statuses, operations, builds, node health, activity and webhook deliveries update as they change, over one server-sent event stream per tab ([stack-and-structure](dashboard/stack-and-structure.md#live-updates)).
- **Shared with Lewagent.** The chat, terminal, file browser and status components are written once, in React, and published as a package Lewagent can use too ([stack-and-structure](dashboard/stack-and-structure.md#shared-components)).
- **Embedded.** `lewbox serve` serves the built app from the binary. No Node.js at runtime, no CDN, no separate deploy.
- **Fast, accessible, keyboard-friendly**, in light and dark.

### Non-goals

- Server-side rendering. The dashboard sits behind a sign-in, so SEO doesn't matter. The hosted setup pages are the one exception worth thinking about, and they work fine as SPA routes ([setup-pages](dashboard/setup-pages.md)).
- The public docs and marketing site. They're a separate static site ([sdks-and-cli](sdks-and-cli.md#docs)).
- Lewagent's product UI. The dashboard is for running Lewbox, not for working in a computer all day; it shares components with Lewagent but not its layouts.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Every page's purpose and most of its content; the chat UI's model (chunks merged from events, pending approvals, queued prompts, model picker, directory picker); xterm.js terminals; hosted, project-branded setup pages with no Lewbox branding; Tailwind 4 |
| **Change** | Svelte → React 19; Inertia props → the API through a generated client and TanStack Query; polling → server-sent events; tenants → projects; URLs `/t/{tenant}/…` → `/p/{project}/…`; sandbox pages use the session too, so no sandbox keys in the dashboard ([stack-and-structure](dashboard/stack-and-structure.md#authentication)) |
| **Drop** | The welcome page and the in-app docs (moved to the docs site); the stub dashboard page, which becomes a real overview; Inertia; bits-ui; layerchart |
| **Add** | An instance admin area; members and invitations; a command palette; a project overview with health and usage; webhook event browser; per-sandbox live activity; a node's AerolVM version and update status |

## Design

The parts, each in its own plan:

| Plan | What it covers |
|---|---|
| [stack-and-structure](dashboard/stack-and-structure.md) | Libraries, folder layout, the generated API client, data fetching and caching, live updates, auth and CSRF, routing, forms, errors, testing, building and embedding, the dev workflow, shared components |
| [navigation-and-layout](dashboard/navigation-and-layout.md) | The shell: project switcher, sidebar, breadcrumbs, command palette, the route tree, URL rules, empty states, theming |
| [project-pages](dashboard/project-pages.md) | Sign-in, choosing and creating a project, the overview, members, admin keys, webhooks, settings, and the instance admin area |
| [cluster-pages](dashboard/cluster-pages.md) | Clusters, creating one, installations, nodes and their metrics, and templates and builds |
| [profile-pages](dashboard/profile-pages.md) | Profiles, profile keys, limits, setup links, agent credentials, sign-ins, SSH keys, telemetry, and credits once the `credits` plugin ships |
| [sandbox-pages](dashboard/sandbox-pages.md) | The sandbox list and create dialog, a sandbox's overview, operations, activity, terminal, sessions, files, ports, SSH keys and conversations (the chat) |
| [setup-pages](dashboard/setup-pages.md) | The hosted profile setup pages behind setup links: agents and sign-ins, Git, SSH keys, telemetry |
| [plugins/dashboard-slots](plugins/dashboard-slots.md) | How plugins appear: named slots in the shell, settings forms built from plugins' schemas, plugin pages, and what shows when a plugin is off |

**Plugins** ([plugins](plugins.md)) fill much of the dashboard: each agent's card, the GitHub connection, the DNS and storage forms, limits, credits and telemetry are contributed by their plugins into the shell's slots, and appear only in projects where the plugin is on. The pages below describe them where they appear; [dashboard-slots](plugins/dashboard-slots.md) describes the mechanism.

### Route tree, in short

```
/sign-in                                        email code sign-in
/projects                                       choose or create a project
/p/{project}                                    overview
/p/{project}/sandboxes[/{sbx}[/{tab}]]          sandboxes and everything inside
/p/{project}/profiles[/{prf}[/{tab}]]           profiles, keys, limits, setup, credits
/p/{project}/clusters[/{clu}[/nodes/{node}]]    clusters, installations, nodes
/p/{project}/templates[/{tpl}[/builds/{bld}]]   templates and builds
/p/{project}/settings/{section}                 general, members, admin keys, webhooks, plugins, Git, agents, observability, and each plugin's settings (credits…)
/admin/…                                        instance admin: projects, users, instance settings, plugins (read-only: set in configuration)
/setup/{token}                                  hosted profile setup, no dashboard chrome
```

The full tree, with every legacy page mapped, is in [navigation-and-layout](dashboard/navigation-and-layout.md#route-tree).

### Page map

Every legacy page, and where it goes. Paths are under `lewbox-legacy/resources/js/pages/`.

| Legacy page | New route | Plan |
|---|---|---|
| `Welcome.svelte` | Dropped; the docs site's home | [sdks-and-cli](sdks-and-cli.md#docs) |
| `Login.svelte` | `/sign-in` | [project-pages](dashboard/project-pages.md#sign-in) |
| `docs/Show.svelte` | Dropped; the docs site | [sdks-and-cli](sdks-and-cli.md#docs) |
| `setup/Link.svelte`, `setup/Profile.svelte`, `setup/Done.svelte`, `setup/Ended.svelte` | `/setup/{token}`, `/setup/{token}/done`, `/setup/ended` | [setup-pages](dashboard/setup-pages.md) |
| `tenants/Select.svelte`, `tenants/Create.svelte` | `/projects`, `/projects/new` | [project-pages](dashboard/project-pages.md#choosing-a-project) |
| `tenants/Account.svelte` | `/account` | [project-pages](dashboard/project-pages.md#account) |
| `tenants/Dashboard.svelte` (stub) | `/p/{project}`, a real overview | [project-pages](dashboard/project-pages.md#overview) |
| `tenants/Sandboxes.svelte` | `/p/{project}/sandboxes` | [sandbox-pages](dashboard/sandbox-pages.md#list) |
| `tenants/sandboxes/Show.svelte` | `/p/{project}/sandboxes/{sbx}` | [sandbox-pages](dashboard/sandbox-pages.md#overview) |
| `tenants/sandboxes/Terminal.svelte` | `…/{sbx}/terminal` | [sandbox-pages](dashboard/sandbox-pages.md#terminal) |
| `tenants/sandboxes/Sessions.svelte`, `sessions/Show.svelte` | `…/{sbx}/sessions[/{ses}]` | [sandbox-pages](dashboard/sandbox-pages.md#sessions) |
| `tenants/sandboxes/Files.svelte` | `…/{sbx}/files` | [sandbox-pages](dashboard/sandbox-pages.md#files) |
| `tenants/sandboxes/Ports.svelte` | `…/{sbx}/ports` | [sandbox-pages](dashboard/sandbox-pages.md#ports) |
| `tenants/sandboxes/SshKeys.svelte` | `…/{sbx}/ssh-keys` | [sandbox-pages](dashboard/sandbox-pages.md#ssh-keys) |
| `tenants/sandboxes/Conversations.svelte`, `conversations/Show.svelte` | `…/{sbx}/conversations[/{cnv}]` | [sandbox-pages](dashboard/sandbox-pages.md#conversations) |
| `tenants/Templates.svelte` | `/p/{project}/templates` | [cluster-pages](dashboard/cluster-pages.md#templates) |
| `tenants/templates/Create.svelte`, `Edit.svelte` | `…/templates/new`, `…/{tpl}/edit` | [cluster-pages](dashboard/cluster-pages.md#creating-and-editing-a-template) |
| `tenants/templates/Show.svelte`, `Builds.svelte`, `builds/Show.svelte`, `clusters/Show.svelte`, `Logs.svelte` | `…/{tpl}`, `…/{tpl}/builds[/{bld}]` | [cluster-pages](dashboard/cluster-pages.md#a-template) |
| `tenants/Profiles.svelte` | `/p/{project}/profiles` | [profile-pages](dashboard/profile-pages.md#list) |
| `tenants/profiles/Create.svelte`, `Edit.svelte`, `Show.svelte` | `…/profiles/new`, `…/{prf}`, `…/{prf}/settings` | [profile-pages](dashboard/profile-pages.md#a-profile) |
| `tenants/profiles/ApiKeys.svelte` | `…/{prf}/keys` | [profile-pages](dashboard/profile-pages.md#keys) |
| `tenants/profiles/Limits.svelte` | `…/{prf}/limits` | [profile-pages](dashboard/profile-pages.md#limits) |
| `tenants/profiles/Credits.svelte`, `CreditBuckets.svelte`, `CreditTransactions.svelte` | `…/{prf}/credits[/…]` (Beta) | [profile-pages](dashboard/profile-pages.md#credits-beta) |
| `tenants/Clusters.svelte` | `/p/{project}/clusters` | [cluster-pages](dashboard/cluster-pages.md#list) |
| `tenants/clusters/Create.svelte` | `…/clusters/new` | [cluster-pages](dashboard/cluster-pages.md#creating-a-cluster) |
| `tenants/clusters/Show.svelte` | `…/clusters/{clu}` | [cluster-pages](dashboard/cluster-pages.md#a-cluster) |
| `tenants/clusters/nodes/Create.svelte`, `installations/Show.svelte` | `…/{clu}/nodes/new`, `…/{clu}/installations/{ins}` | [cluster-pages](dashboard/cluster-pages.md#adding-a-node) |
| `tenants/nodes/Show.svelte` | `…/{clu}/nodes/{node}` | [cluster-pages](dashboard/cluster-pages.md#a-node) |
| `tenants/Settings.svelte` | `/p/{project}/settings` | [project-pages](dashboard/project-pages.md#settings) |
| `tenants/settings/ApiKeys.svelte` | `…/settings/admin-keys` | [project-pages](dashboard/project-pages.md#admin-keys) |
| `tenants/settings/Credits.svelte` | `…/settings/credits` (Beta) | [project-pages](dashboard/project-pages.md#credit-rates-beta) |
| `tenants/settings/Profiles.svelte` (GitHub App; Git mode dropped) | `…/settings/git` | [project-pages](dashboard/project-pages.md#git-and-the-github-app) |
| `tenants/settings/AgentSignIn.svelte` | `…/settings/agents` | [project-pages](dashboard/project-pages.md#agents-and-sign-ins) |
| `tenants/settings/Observability.svelte` | `…/settings/observability` | [project-pages](dashboard/project-pages.md#observability) |
| `tenants/settings/Webhooks.svelte`, `webhooks/Show.svelte` | `…/settings/webhooks[/{whk}]` | [project-pages](dashboard/project-pages.md#webhooks) |

## API

The dashboard adds no API of its own. It needs these from [api](api.md), which other plans own:

- **Session auth** for the Admin API: sign-in by email code, a session cookie, CSRF protection, sign-out, and "who am I" with the user's projects ([projects/users-and-login](projects/users-and-login.md)).
- **Acting inside a sandbox with a session**, for a project member, without minting a sandbox key ([dashboard/stack-and-structure](dashboard/stack-and-structure.md#authentication)).
- **A project event stream**, `GET /v0/projects/{prj}/events` (server-sent events), carrying resource changes the dashboard is looking at ([stack-and-structure](dashboard/stack-and-structure.md#live-updates)).
- **Node metrics series**, webhook delivery stats, and an overview summary: counts by status, recent problems.

## Security

- Session cookie: `__Host-lewbox_session`, `Secure`, `HttpOnly`, `SameSite=Lax`. State-changing requests need a CSRF token header that a cookie can't supply ([stack-and-structure](dashboard/stack-and-structure.md#authentication)).
- A strict Content Security Policy, set by `lewbox serve` for the dashboard's HTML: scripts and styles only from `'self'`, no inline scripts, `frame-ancestors 'none'` except on the setup pages, which a project may embed if it sets an allowed origin ([setup-pages](dashboard/setup-pages.md#embedding)).
- Agent output is rendered as untrusted: markdown without raw HTML, links with `rel="noopener noreferrer"`, and images only from `data:` and the sandbox's own file endpoint.
- Secrets (keys, webhook secrets, credentials) are shown once, when they're made, and never stored by the browser.

## Testing

Covered in [stack-and-structure](dashboard/stack-and-structure.md#testing): unit tests with Vitest, component tests with Testing Library, and end-to-end tests with Playwright against `lewbox serve` running with the [fake AerolVM cluster](foundation/testing.md).

## Build plan

1. **M0:** scaffold `web/` with Vite, React, TypeScript, Tailwind, Base UI and TanStack; generate the client from `api/openapi.yaml`; embed `web/dist` in the binary; sign-in; the shell.
2. **M1:** projects, clusters, nodes, installations, templates, profiles, keys, limits.
3. **M2:** sandboxes: list, create, overview, operations, terminal, sessions, files, ports, SSH keys.
4. **M3:** conversations; setup pages (agents, sign-ins, Git, SSH keys, telemetry); project agent and Git settings.
5. **M4:** activity, webhooks, observability settings, the overview page, the instance admin area, the command palette, polish and accessibility.
6. **Beta:** credits pages (the `credits` plugin's slots).
7. **M4:** the plugin slots and `SchemaForm` ([plugins/dashboard-slots](plugins/dashboard-slots.md#build-plan)), the Plugins settings page.

Each page plan lists its own tasks.

## Open questions

1. **Shared component package.** Do we publish the chat, terminal and file browser as `@lewbox/react` for Lewagent and others, or keep them in `web/` and copy them? (Proposed: a package in this repository, published with the SDK.)
2. **Docs site.** Is it a separate repository (Astro, like AerolVM's), or `docs/` here, built into a static site? ([sdks-and-cli](sdks-and-cli.md#docs))
3. **Base UI or Radix.** Lewagent uses `@base-ui/react`. Proposed: match it.
4. **Does the instance admin area need to ship in the alpha**, or is configuration plus the CLI enough for self-hosters at first?
