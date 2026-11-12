# Phase 6: the web app (portal + console)

**Goal:** one clean, quiet web app with two sides. Customers get the **portal** at `/`; the operator gets the **console** at `/admin`. Built on `@harlew-dev/ui` (Catalyst-style, Tailwind v4). Every new reusable component goes into the `ui` library source first (`../ui`), then `make update-ui` re-vendors it.

Principles: few pages, few words, numbers first, one primary action per page, empty states that tell you the next step, copy buttons on everything a user pastes. No modals inside modals. Mobile works.

## Routing

| Path | Who | Page |
|---|---|---|
| `/login`, `/signup` | anyone | Sign in; create an account (only when `/v0/instance` says sign-up isn't `closed`) |
| `/` | customer | **Home**: pending/suspended banner; until the first sandbox exists, a 3-step *Get started* (create a key in a region → copy the env vars and snippet → "Waiting for your first sandbox…", which turns green when one appears); otherwise stat tiles (running sandboxes, vCPU-hours this month, credits) and limit meters |
| `/sandboxes` | customer | Every sandbox in every region: id (copy), region, status badge, size (vCPU / memory), started, actions (stop, delete, with confirmation). Filter by region and status. Refreshes every 10 s |
| `/keys` | customer | Sandbox API keys: list (name, region, `avm_…last4`, created, expires, last used), create (name, region, expiry), revoke. The create result shows the key once, plus tabs **AerolVM SDK (TypeScript / Python)**, **E2B SDK**, **curl**, each with env vars and a 6-line example |
| `/usage` | customer | Range 7 / 30 / 90 days. Tiles: vCPU-hours, memory GiB-hours, sandbox-hours, cost (if prices are set). Stacked daily bar chart by region. Limits **per region**: the region's plan name and a `Meter` per limit (used now vs that region's limit) |
| `/billing` | customer | Only when billing is on: balance, this month's spend, monthly grant, transactions table |
| `/team` | customer | Members (existing members page), management API keys (existing `lba_` page) |
| `/settings` | customer | Organization name, regions with their plan and limits (read-only), leave organization. A customer with no regions sees "Your provider hasn't enabled a region for you yet" on Home and Keys |
| `/account` | everyone | Existing account page |
| `/docs/*` | everyone | Existing docs; customer guides first for customers |
| `/admin` | operator | **Overview**: tiles (customers, pending approvals, sandboxes running, vCPUs in use, clusters healthy n/m); running sandboxes over 7 days; 30-day uptime bar per cluster; top 5 customers by usage this month (BarList) |
| `/admin/customers` | operator | Table: name, plan, status, running sandboxes, vCPU-hours (30 d), balance. Tabs: All / Pending / Suspended. Bulk-free; row → detail |
| `/admin/customers/:org` | operator | Header with status + actions (Approve, Suspend/Restore, Change plan, Add credits). Sections: **regions** (a row per subscription: region, plan, limits, used now; add a region, change its plan or overrides, remove it with confirmation), usage chart, sandboxes, sandbox keys, members, credit transactions |
| `/admin/sandboxes` | operator | Every sandbox of every customer (for abuse triage): customer, region, status, size, running for; sort by longest running; stop/delete |
| `/admin/plans` | operator | Plans table (limits, subscriptions on it, default badge); create/edit dialog |
| `/admin/servers` | operator | Existing servers page (clusters + nodes), moved |
| `/admin/servers/clusters/:ref` | operator | Existing cluster page + 30-day uptime bars for the cluster and each node, node metrics |
| `/admin/users` | operator | Existing users pages, moved |
| `/admin/settings` | operator | Instance name, sign-up mode, default plan and default regions for new sign-ups, billing mode, prices, currency label, sign-up credits |

Old console paths (`/servers…`, `/users…`, `/organizations…`) redirect to their `/admin` equivalents for admins. A customer hitting `/admin` gets the not-found page. An instance admin who opens `/` is sent to `/admin` unless they're a member of an organization (then the org switcher offers both).

## Layout

- Customer sidebar: org switcher (when more than one org) · Home · Sandboxes · API keys · Usage · Billing (if on) · Team · Settings · Docs. Footer: account menu.
- Console sidebar: instance name · Overview · Customers (badge with pending count) · Sandboxes · Plans · Servers · Users · Settings · Docs.

## New components (in `../ui`, exported, with a test)

- `UptimeBar`: N daily bars (`{date, uptime: number|null, down_minutes, degraded_minutes}`), green ≥ 99.9, amber ≥ 95, red below, grey for no data; tooltip "99.31% uptime · 12m down"; footer "30 days ago · 99.95% · Today". Gaps count as down (the server does that).
- `CodeTabs` (if `CodeBlock` can't already do tabs): tabbed code snippets with a copy button.
- Anything else that turns out to be reusable (e.g. a `SecretReveal` "copy this key now" panel) goes into the library too.

Charts use the library's existing `BarChart`, `Sparkline`, `Meter`, `BarList`, `Stat` (`@harlew-dev/ui/chart` needs `recharts` added to `web/package.json`).

## Done when

- `npm --prefix web run typecheck`, `npm --prefix web test` and `npm --prefix web run build` pass; `make test` passes.
- Vitest tests for: signup page (closed hides the link), keys page (create shows the secret once), sandboxes page (stop calls the API), admin customers (approve), routing split (customer can't see console nav).
- Screens checked in a real browser against a running orchestrator with the dev cluster.
