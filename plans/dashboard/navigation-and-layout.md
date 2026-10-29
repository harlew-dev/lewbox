# Dashboard: navigation and layout

> Status: Draft · Milestone: Alpha (M0 shell, M4 palette) · Updated: 2026-10-29

The dashboard's shell: how you move between projects and their parts, the full route tree, URL rules, the command palette, and the layouts every page sits in.

## Today

- `lewbox-legacy/resources/js/layouts/TenantLayout.svelte` puts the tenant's name and a dropdown menu at the top, with five links: **Sandboxes, Templates, Profiles, Clusters, Settings**. Nodes count as part of Clusters. `AppLayout.svelte` wraps signed-in pages, `DashboardLayout.svelte` the tenant pages, and `DocsLayout.svelte` the docs.
- URLs are `/t/{tenant}/…`, with Laravel route names and Wayfinder helpers (`sandboxes(tenantId)`).
- `lib/navigation.ts` exists to stop a poll from bringing back the page you're leaving: a symptom of polling the page's URL.
- Sandbox tabs (Terminal, Sessions, Files, Ports, SSH keys, Conversations) are separate Inertia pages with a shared header.
- There's no search, no command palette, no breadcrumbs, and no way to jump to a sandbox by id.

## Goals

- Get anywhere in two clicks or one search.
- Every view has a URL that reproduces it, including filters, tabs, a file browser's path, and a conversation's scroll anchor.
- Keep the project in the URL, so two tabs can show two projects.
- Make ids first-class: paste `sbx_…` anywhere and go there.

## Design

### Layouts

| Layout | Used by | Has |
|---|---|---|
| `Bare` | `/sign-in`, `/setup/*`, error pages | Centred card, the instance's or the project's logo, nothing else |
| `Account` | `/projects`, `/account`, `/admin/*` | Top bar with the user menu; no project sidebar |
| `Project` | `/p/{project}/*` | Sidebar, top bar, breadcrumbs, the project event stream ([stack-and-structure](stack-and-structure.md#live-updates)) |
| `Resource` | A sandbox, profile, cluster, node, template | `Project`, plus a resource header (name, id with copy, status, primary actions) and tabs |

### The project shell

```
┌──────────────┬───────────────────────────────────────────────────────────────┐
│ ◆ Acme   ▾   │ Sandboxes / sbx_Qm4pL0sX2bN8cV                ⌘K   ?   (HL) ▾ │
│              ├───────────────────────────────────────────────────────────────┤
│ Overview     │ ● running  sbx_Qm4pL0sX2bN8cV  ⧉   eu-west · 2 vCPU · 4 GB    │
│ Sandboxes    │ Profile Acme · template claude-code     [Stop] [Restart] [⋯]  │
│ Profiles     │ ───────────────────────────────────────────────────────────── │
│ Clusters     │ Overview  Activity  Terminal  Sessions  Files  Ports  SSH     │
│ Templates    │ keys  Conversations                                           │
│              │                                                               │
│ ──────────── │   …                                                           │
│ Settings     │                                                               │
│              │                                                               │
│ docs ↗       │                                                               │
│ v0.1.0       │                                                               │
└──────────────┴───────────────────────────────────────────────────────────────┘
```

- **Project switcher** (top of the sidebar): the user's projects, recent first, with search; "Create project"; "All projects". An instance admin sees "Instance admin" here too.
- **Sidebar:** Overview, Sandboxes, Profiles, Clusters, Templates; then Settings. Each shows a count badge where it helps: sandboxes running, clusters with problems (red), templates with failed builds. Counts come from the overview summary query and update live.
- **Top bar:** breadcrumbs, the command palette (`⌘K`), keyboard help (`?`), and the user menu (account, theme, sign out).
- **Footer of the sidebar:** a link to the docs, and the instance's version. If the binary is newer than the version the instance's nodes run, a dot leads to the clusters that need updating ([clusters/node-agent](../clusters/node-agent.md)).
- **Narrow screens:** the sidebar collapses into a menu button. Pages are usable on a phone for checking and stopping things; the terminal and chat work but aren't designed for it.

### Route tree

File-based TanStack Router routes. `$` marks a parameter. Every route under `/p/$project` checks membership in its loader and shows a "not found" page otherwise.

```
/                                         → /projects, or the last project used
/sign-in                                  email, then code             (Bare)
/sign-in/code                             enter the code               (Bare)
/account                                  name, email, sessions        (Account)
/projects                                 choose a project             (Account)
/projects/new                             create a project             (Account)
/invitations/$token                       accept an invitation         (Bare)

/p/$project                               overview                     (Project)

/p/$project/sandboxes                     list; ?status ?cluster ?profile ?template ?q ?metadata.k
/p/$project/sandboxes/$sbx                overview tab                 (Resource)
/p/$project/sandboxes/$sbx/activity       ?kind ?group ?outcome ?label ?q ?since ?until
/p/$project/sandboxes/$sbx/operations     list; /$op for one operation's steps
/p/$project/sandboxes/$sbx/terminal       a new login shell; ?session=ses_… to reattach
/p/$project/sandboxes/$sbx/sessions       list; ?status
/p/$project/sandboxes/$sbx/sessions/$ses  log and attach
/p/$project/sandboxes/$sbx/files          ?path=/root/workspace
/p/$project/sandboxes/$sbx/ports
/p/$project/sandboxes/$sbx/ssh-keys
/p/$project/sandboxes/$sbx/conversations  list
/p/$project/sandboxes/$sbx/conversations/new
/p/$project/sandboxes/$sbx/conversations/$cnv   the chat; ?message=msg_… to scroll to one

/p/$project/profiles                      list; ?status ?q
/p/$project/profiles/new
/p/$project/profiles/$prf                 overview: sandboxes, usage, setup status
/p/$project/profiles/$prf/keys            profile keys
/p/$project/profiles/$prf/limits
/p/$project/profiles/$prf/setup           setup links; agents, Git, SSH keys, telemetry as the project sees them
/p/$project/profiles/$prf/sign-ins        agent sign-ins in progress and past
/p/$project/profiles/$prf/activity        every sandbox of the profile
/p/$project/profiles/$prf/settings        name, external id, default cluster, Git identity, suspend, delete
/p/$project/profiles/$prf/credits         (Beta) balance, buckets, transactions

/p/$project/clusters                      list
/p/$project/clusters/new                  create: runtime, architecture, storage, DNS
/p/$project/clusters/$clu                 overview: health, nodes, capacity
/p/$project/clusters/$clu/nodes/new       requirements, then an installation
/p/$project/clusters/$clu/installations/$ins   install command and progress
/p/$project/clusters/$clu/nodes/$node     metrics, uptime, versions, sandboxes on it
/p/$project/clusters/$clu/settings        name, status (suspend), storage, DNS, AerolVM version

/p/$project/templates                     list
/p/$project/templates/new
/p/$project/templates/$tpl                overview: builds per cluster, ports, defaults
/p/$project/templates/$tpl/edit
/p/$project/templates/$tpl/builds         list; ?cluster
/p/$project/templates/$tpl/builds/$bld    status, error, log

/p/$project/settings                      → general
/p/$project/settings/general              name, slug, logo and colours for setup pages, delete
/p/$project/settings/members              members, roles, invitations
/p/$project/settings/admin-keys
/p/$project/settings/webhooks             endpoints
/p/$project/settings/webhooks/$whk        deliveries, attempts, secret, test
/p/$project/settings/plugins              plugins on and off for the project
/p/$project/settings/git                  the GitHub App, sandbox Git cap, deliveries
/p/$project/settings/agents               sign-in cluster, sign-in limits, the agents template
/p/$project/settings/observability        activity retention, stderr recording
/p/$project/settings/credits              (Beta) compute unit rates (the credits plugin)
/p/$project/settings/$plugin              a plugin's settings section (slot, or a form from its schema)

/admin                                    → /admin/projects            (Account)
/admin/projects                           every project on the instance
/admin/users                              every user
/admin/settings                           instance settings visible to admins (read-only where set by config)
/admin/plugins                            the plugins compiled in, on or off, their instance settings: read-only, set in configuration

/setup/$token                             hosted profile setup         (Bare, project-branded)
/setup/$token/done
/setup/ended
```

### URL rules

- **The project is `/p/{project}`**, using the project's `prj_` id. A project's human slug, if we add one ([projects](../projects.md)), may redirect to it, but links the dashboard makes always use the id, so renaming never breaks them.
- **Resources use their public ids** (`sbx_`, `prf_`, `clu_`, `node_`, `ses_`, `cnv_`, `whk_`, `bld_`, `op_`, `cin_`), and templates their slug. These are the same ids the API and SDKs use, so you can copy one from a log and paste it into a URL.
- **Filters and tabs are search params**, validated per route. Defaults are left out of the URL.
- **Cursors aren't in the URL.** "Load more" appends in place; a reload starts from the first page. A shared link shows the same filters, not the same scroll position.
- **Legacy redirects:** `lewbox serve` answers `/t/{tenant}/…` with a `301` to the matching `/p/{project}/…` path, using the [migration](../migration/data.md) id map (project ids keep their tenant's random part), for the first months after cutover ([migration/cutover](../migration/cutover.md)).

### Command palette

`⌘K` (or `Ctrl K`) opens a palette that searches:

1. **Ids.** Anything matching `^(sbx|prf|clu|node|ses|cnv|whk|bld|op|key|evt)_[A-Za-z0-9]{14}$` is looked up with `GET /v0/projects/{prj}/lookup/{id}` (or the resource's own get), and Enter goes to it. Ids from other projects the user belongs to are found too, and switch project.
2. **Names.** Sandboxes by metadata values and template; profiles by name and external id; clusters and templates by name. One search request, debounced at 150 ms.
3. **Pages.** "Webhooks", "Admin keys", "New sandbox" and the rest.
4. **Actions** on the current resource: "Stop sandbox", "Open terminal", "Copy id", "Create sandbox key", "Rebuild template". Destructive actions still confirm.

Recent items (last 10 visited resources) show before you type, kept in `localStorage` per project.

### Breadcrumbs and resource headers

- Breadcrumbs follow the route: `Sandboxes / sbx_… / Files`. On a profile's sandbox, the sandbox's profile is a link in its header, not a breadcrumb, since a sandbox lives under the project.
- A resource header shows its name (or id where there's none), id with copy, status badge, the facts that identify it (cluster, size, profile, template, created), and its primary actions. Secondary actions go in a `⋯` menu.

### Empty, loading and error states

- **Empty:** says what the thing is, why you'd want one, and the next step, with the docs link. For example the sandbox list in a project with no clusters says to add a cluster first, with the button.
- **Loading:** skeletons shaped like the content for first loads; the previous content stays during refreshes.
- **Errors:** described in [stack-and-structure](stack-and-structure.md#errors-and-empty-states).
- **Not running:** inside-a-sandbox tabs on a sandbox that isn't running show its status, what it's waiting for, and Start, instead of a broken terminal.

### Notifications

The legacy app stores `tenant_notifications` (certificate expiring, webhook endpoint disabled) but never shows them. The new shell has a bell in the top bar listing the project's unread notifications, from [projects](../projects.md). Each links to the thing it's about. Members can also get them by email, per their account settings.

## Testing

- Route tree: a test renders every route with fixtures and fails on any route without a loader, a title or an error boundary.
- Playwright: switch projects, the command palette by id and by name, keyboard shortcuts, legacy `/t/…` redirects, narrow layout.

## Build plan

1. `Bare`, `Account`, `Project` and `Resource` layouts; sidebar; switcher; user menu; theme. (M0)
2. Route tree skeleton, with loaders and error boundaries, filled in as pages land. (M0 onwards)
3. Breadcrumbs and resource headers. (M1)
4. Sidebar counts from the overview summary, live. (M1)
5. Command palette: pages and actions (M2), ids and names (M4).
6. Notifications bell. (M4)
7. Legacy redirects. (Beta, with migration)

## Open questions

1. **Project slugs in URLs?** `/p/acme/…` reads better than `/p/prj_…`, but slugs change. (Proposed: ids, with slugs only as a redirect.)
2. **A lookup endpoint by id** across resources, or does the palette try each resource's get by prefix? (Proposed: by prefix; no new endpoint.)
3. **Should the sandbox list be the landing page** of a project rather than an overview? (Proposed: overview, which links to everything that needs attention.)
