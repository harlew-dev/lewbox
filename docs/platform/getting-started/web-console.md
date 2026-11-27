---
title: Use the web console
description: Open Lewbox in a browser: the console for you, the portal for your customers, and what each page does.
---

Lewbox's web app has two sides on one address: the **console** at `/admin` for you (instance admins) and the **portal** at `/` for your customers. It is the same application that renders this documentation. Instance admins land on the console; everyone else lands on the portal.

The orchestrator serves the console itself on the same port and origin as the API, so there is nothing extra to run and no separate URL to configure.

## Open it

Open `http://<host>:8080/` in a browser. If the orchestrator listens somewhere else, use that address; `lewbox serve --port 8000` means `http://<host>:8000/`.

### From source

The repository's `Makefile` builds the console and the binary together:

```bash
git clone https://github.com/harlew-dev/lewbox.git
cd lewbox
make run
```

`make run` builds `bin/lewbox` and starts it on port 8080. You need Go 1.27+ and Node 24+; see [Install the CLI](../setup/install-the-cli.md).

### With Docker

```bash
LEWBOX_ADMIN_PASSWORD='pick-a-long-password' docker compose up -d --build
```

Then open [http://localhost:8080](http://localhost:8080). See [Run with Docker](../setup/run-with-docker.md).

## Sign in

Use a Lewbox account. On the orchestrator's machine you can create the first one with the CLI:

```bash
lewbox user create you@example.com --instance-admin
```

For containers and automation, set `LEWBOX_BOOTSTRAP_ADMIN_EMAIL` and `LEWBOX_BOOTSTRAP_ADMIN_PASSWORD` (or `LEWBOX_BOOTSTRAP_ADMIN_PASSWORD_FILE`) before the orchestrator starts. It creates that account only while there are no accounts. See [Run the orchestrator](../setup/run-the-orchestrator.md#create-the-first-admin).

The console and the CLI share accounts, so a password change in either place affects both.

## Console pages (`/admin`, instance admins)

The sidebar lists Home, Workspaces and Users, then two groups: **Compute** (Sandboxes, Snapshots, Servers) and **Manage** (Plans, Audit trail, Settings). Home is the Overview page.

| Page | Shows | Actions |
|---|---|---|
| Overview (Home) | Workspaces (and how many are pending), running sandboxes, capacity in use, 30-day uptime per cluster, running sandboxes over 7 days, the top workspaces this month | Open a cluster or workspace |
| Workspaces | Every workspace: its cluster and plan, status, running sandboxes, vCPU-hours over 30 days. Tabs for **workspaces** and **setup links**; filter by status, region, plan or owner | **New workspace**: the owner's email (and a password if the account is new), a workspace name, a cluster and a plan, in one dialog. **Create setup link**: a single-use link for a plan in a cluster. Each row's `⋯` menu: view, approve, suspend or restore |
| Workspace | One workspace: status, its cluster and plan (limits, use, quota push errors), usage, sandboxes, sandbox keys, members | Approve, suspend, restore; assign a cluster and plan, change the plan or limits, remove it (and remove anyway if keys couldn't be revoked) |
| Sandboxes | Every workspace's sandboxes, longest-running first. Search by ID, filter by status, region or workspace | Stop, delete |
| Snapshots | One list of every workspace's snapshots and archived sandboxes, with each row's kind, workspace and node (an archive has none); totals of snapshots, disk and memory, and failed uploads; disk by node and by workspace. Search by name and filter by kind, status, region or workspace. AerolVM reports no snapshot size, so the totals add up each snapshot's sandbox disk and memory, not bytes on disk | Delete |
| Plans | Plans with their limits, how many workspaces use each, and the default | Create, edit, delete an unused plan |
| Audit trail | Every change made through Lewbox, newest first, with the actor, the action, the target, the workspace and the client IP | Filter by several actors, workspaces and actions, and by a time preset or custom range; load more pages |
| Servers | **Clusters** (status, runtime, nodes, version, last check) and **Nodes** | **Add server**: **Start a new cluster** and **Add a node** are two steps, *Configure* (access mode, DNS and storage profiles, runtime, when a sandbox stops, node role) and *Install commands* (the exact commands for those choices, with Back to change them). The machine signs in with an access token, which **Create an access token** makes without leaving the dialog. The last step waits and says when the cluster or node has connected, and the lists refresh when the dialog closes. Or a single-use join command. Register an existing cluster, remove a node or cluster |
| Cluster | Node metrics (sandboxes, vCPU allocated, memory, disk) over 24 hours, 7 or 30 days; 30-day uptime bars for the cluster and each node; the summary; a **Settings** card (domain, DNS profile, DNS records, storage profile, when a sandbox stops); the latest checks; the nodes, with an **Outdated** badge, its reasons and `lewbox node configure` on a node behind the cluster's profiles; an **Upgrade** card while a rollout exists, with each node's state | Run the checks, edit the settings (after a profile change, run `lewbox node configure` on every node), add or remove a node, **Upgrade cluster**: plan the tested release or another tag, with the node order, warnings, one-way migrations and allow-downtime choice, then stop, resume, roll back or finalize the rollout |
| Users | Every account | Create, disable, set a password, remove |
| Settings | Tabs. **General**: instance name, sign-up mode, and the default plan and region for workspaces people create themselves. **DNS profiles** and **Storage profiles**: saved Cloudflare tokens and S3-compatible buckets, with the masked key, the clusters using each and the last test | Save; create, edit, test and delete profiles (saving tests the profile and shows the step that failed; a profile clusters use can't be deleted) |

## Portal pages (`/`, customers)

The sidebar starts with the **workspace menu**: Workspace settings, every workspace the account belongs to, **New workspace…** (unless sign-up is closed), and for instance admins the operator console. A workspace runs in one region, so no portal page asks for one.

| Page | Shows | Actions |
|---|---|---|
| Home | **Get started** until the first sandbox exists (create a key, run the snippet, wait for the sandbox); then the last 30 days' numbers and the plan's limits | Create a key |
| Sandboxes | The workspace's sandboxes, and a **Snapshots** tab with its snapshots and archived sandboxes | Stop, delete; restore an archive as a new sandbox, delete a snapshot or an archive (viewers see the list only) |
| API keys | Sandbox keys (the token is shown once, with the AerolVM SDK, E2B SDK and curl settings) | Create, revoke |
| Usage | vCPU-hours, memory GiB-hours and sandbox-hours over 7, 30 or 90 days, and the plan's limits with current use | |
| Plan & Billing | The workspace's plan, its region and its limits with current use. There is no billing | |
| Workspace settings | Tabs: **General** (name, region and plan, workspace ID, leave), **Members**, **Management API keys** | Rename (owners and admins), add or remove members, create management keys, leave |

An account that belongs to no workspace goes to **onboarding** (`/onboarding`) to create one, then sees a short getting-started tour. Someone you send a [setup link](../accounts/customers-and-plans.md) to opens `/setup/<token>`, creates an account there (even when sign-up is closed), and then creates a workspace that starts on the link's plan and cluster.

Every page also has **Account settings** (your email, password and [access tokens](../accounts/access-tokens.md)) and the documentation, in the account menu at the bottom of the sidebar.

Customers only see their workspace's region, never clusters, nodes or other workspaces. The API enforces the same rules as the pages. Lewbox refuses to remove or disable the last enabled instance admin.

## Searching, sorting and filtering lists

Every list in the console and the portal pages through the API. The footer shows the rows on screen, such as **Showing 21–40 of 131**, and the page links move one page at a time.

- **Search**: the field before the filters matches the columns the list documents, such as a workspace name, a sandbox ID, an email or a note.
- **Sort**: click a column header to sort by it, and click it again to reverse. Dates and sizes start newest or largest first; text starts A–Z. The sorted column is marked for screen readers.
- **Filter**: each badge opens a menu of values. Tick one or more and choose **Apply**. One value shows its name; several show **N selected**. **Clear filters** on the right removes them all. Filters with many options, such as workspaces and accounts, also have a search field and **Load more**.
- The list's state is in the URL, such as `?q=acme&status=active,pending&sort=-created&page=2`, so a reload, a shared link or Back shows the same rows. Changing a search, filter or sort returns to page 1.

The Workspaces page filters by status, region, plan or owner, and its **Setup links** tab has its own filters. An old link to pending or suspended workspaces opens Workspaces with that status applied. The audit trail keeps its newest-first order and loads more rows at the bottom; its actor, workspace and action filters accept several values, and its time badge offers presets or a custom range.

## Browser sessions

Signing in puts a session token in an `HttpOnly` cookie, never in JavaScript. The console cannot read it, and no token is shown in the page.

A browser session lasts 7 days at most and ends after 12 hours without a request. Signing out ends it immediately. Changing your password signs out every other session of yours, in browsers and in the CLI; disabling an account ends all of its sessions. The cookie is `SameSite=Strict` and is scoped to the orchestrator's origin.

## Sign-in rate limits

Sign-in is rate limited by client IP and by email. Too many attempts return `429` with `Retry-After`. The exact limits and what they protect are in [Security](../reference/security.md#signing-in).

## Documentation without an account

The customer documentation at `/docs` and the platform documentation at `/platform` are public: anyone who can reach the orchestrator can read them without signing in. The rest of the console and the API require an account.

## Behind a reverse proxy or TLS

Put TLS in front of the orchestrator rather than exposing plain HTTP. Two settings matter for the console:

- `--cookie-secure=always` (or `LEWBOX_COOKIE_SECURE=always`) marks the session cookie `Secure` when your proxy terminates TLS without sending `X-Forwarded-Proto: https`.
- `--trusted-origin` (or `LEWBOX_TRUSTED_ORIGINS`, comma-separated) adds the public origin, such as `https://lewbox.example.com`, to the origins the CSRF check accepts. Set it when the proxy rewrites `Host`.

Behind a proxy, set `--trusted-proxy` (or `LEWBOX_TRUSTED_PROXIES`) to the proxy's address, for example `127.0.0.1/32`, so sign-in and sign-up rate limits see the real client IP from `X-Forwarded-For`. Without it, every request appears to come from the proxy and all users share one per-IP budget. See [Security](../reference/security.md#browser-sessions) and [Configuration](../reference/configuration.md) for the full list.

See [Run in production](../setup/run-in-production.md#put-tls-in-front) for a reverse-proxy example.

## Next

- [Quickstart](quickstart.md): a working node in about five minutes.
- [Manage accounts](../accounts/manage-accounts.md): instance admins, passwords and disabling.
- [Security](../reference/security.md): what is stored and how secrets are protected.
