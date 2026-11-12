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

| Page | Shows | Actions |
|---|---|---|
| Overview | Customers (and how many are pending), running sandboxes, capacity in use, 30-day uptime per cluster, running sandboxes over 7 days, the top customers this month | Open a cluster or customer |
| Customers | Every workspace: its regions and plans, status, running sandboxes, vCPU-hours over 30 days. Tabs for pending and suspended | **New customer**: the owner's email (and a password if the account is new), a workspace name and a plan per region, in one dialog |
| Customer | One workspace: status, regions (plan, limits, use, quota push errors), usage, sandboxes, sandbox keys, members | Approve, suspend, restore; add a region, change its plan or limits, remove it (and remove anyway if keys couldn't be revoked) |
| Sandboxes | Every customer's sandboxes, longest-running first | Stop, delete |
| Plans | Plans with their limits, how many customers use each, and the default | Create, edit, delete an unused plan |
| Servers | **Clusters** (status, runtime, nodes, version, last check) and **Nodes** | **Add server** (the commands to run on the machine, or a single-use join command), register an existing cluster, remove a node or cluster |
| Cluster | The latest checks, 30-day uptime bars for the cluster and each node, node metrics (sandboxes, vCPU allocated, memory, disk) over 24 hours, 7 or 30 days | Run the checks, add or remove a node |
| Users | Every account | Create, disable, set a password, remove |
| Settings | Instance name, sign-up mode, default plan and regions for new sign-ups | Save |

## Portal pages (`/`, customers)

| Page | Shows | Actions |
|---|---|---|
| Home | **Get started** until the first sandbox exists (create a key, run the snippet, wait for the sandbox); then the last 30 days' numbers and the limits in each region | Create a key |
| Sandboxes | The workspace's sandboxes in every region, and a **Snapshots** tab | Stop, delete |
| API keys | Sandbox keys per region (the token is shown once, with the AerolVM SDK, E2B SDK and curl settings) | Create, revoke |
| Usage | vCPU-hours, memory GiB-hours and sandbox-hours over 7, 30 or 90 days, by region | |
| Team | The workspace name, its members and its management keys | Rename (owners and admins), add or remove members, create management keys, leave |

Every page also has **Account settings** (your email and password) and the documentation, in the account menu at the bottom of the sidebar.

Customers only see regions their workspace has a plan in, never clusters, nodes or other workspaces. The API enforces the same rules as the pages. Lewbox refuses to remove or disable the last enabled instance admin.

## Browser sessions

Signing in puts a session token in an `HttpOnly` cookie, never in JavaScript. The console cannot read it, and no token is shown in the page.

A browser session lasts 7 days at most and ends after 12 hours without a request. Signing out ends it immediately. Changing your password signs out every other session of yours, in browsers and in the CLI; disabling an account ends all of its sessions. The cookie is `SameSite=Strict` and is scoped to the orchestrator's origin.

## Sign-in rate limits

Sign-in is rate limited by client IP and by email. Too many attempts return `429` with `Retry-After`. The exact limits and what they protect are in [Security](../reference/security.md#signing-in).

## Documentation without an account

The documentation at `/docs` is public: anyone who can reach the orchestrator can read it without signing in. The rest of the console and the API require an account.

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
