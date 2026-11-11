---
title: Use the web console
description: Open the Lewbox web console in a browser, sign in, and know what each page does.
---

The web console is the browser UI for the orchestrator: it shows the clusters and nodes Lewbox manages, and lets instance admins check and remove them and manage accounts. It is the same application that renders this documentation.

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

## Pages

| Page | Shows | Actions |
|---|---|---|
| Overview | Clusters (with how many are ready), nodes (with how many are alive), how many need attention, the AerolVM versions in use, and a table of clusters with status, runtime, node count, version and last check | Open a cluster, refresh |
| Servers | Two tabs: **Clusters**, with status, runtime, node count, version and last check; and **Nodes**, filtered by cluster, with role, liveness, version, last seen time and URL | **Instance admins:** **Add server** to start a new cluster or add a node (the console gives you the commands to run on the machine, or a single-use join command), register an existing cluster, or register a node by URL; remove a node from its row's **⋯** menu |
| Cluster | Slug, name, status, admin and endpoint URLs, runtime, version, the last four characters of the admin token, timestamps, the latest checks and the cluster's nodes | **Instance admins:** run the checks again, add a node, remove a node, or stop managing the cluster (in the **⋯** menu) |
| Members | The current organization's members and their roles | **Owners and admins:** add a member, change a role, remove a member. Anyone: leave |
| API keys | The current organization's API keys, with status, creator, last use and expiry | **Owners and admins:** create a key (its token is shown once), revoke a key |
| Settings | The current organization's name, slug, ID and your role | **Owners and admins:** rename it. **Instance admins:** suspend or restore it |
| Organizations | Every organization (yours, if you aren't an instance admin) | **Instance admins:** create, suspend or restore one |
| Users | Every account, whether it is an instance admin, and whether it is active | **Instance admins only:** create an account, enable or disable it, set its password, remove it |
| User | One account, and the organizations it belongs to with its role in each | **Instance admins only:** the same actions, and add it to an organization |
| Account settings | Your email, role and sign-in method | Change your password |
| Documentation | This documentation, with its own layout: the page list on the left, **On this page** on the right on wide screens, and previous/next links | Browse to any page, or go back to the console |

To sign out, open the account menu at the bottom of the sidebar (the avatar at the top right on a phone) and choose **Sign out**. The same menu opens **Account settings** and the documentation.

Adding clusters and nodes stays in the CLI: run `lewbox node add` on the machine. The console says so when there are no clusters.

## Instance admins and regular accounts

Every signed-in account can look at the overview, clusters, cluster details, nodes and their own account. That is useful for people who need to see what is running.

Only instance admins can change things:

- run a cluster's checks;
- remove a cluster (Lewbox stops managing it and deletes its stored admin token; the AerolVM cluster keeps running);
- remove a node (Lewbox drains it and takes it out of its cluster);
- see the **Users** page and create, disable, set the password of, or remove accounts.

The organization menu at the top of the sidebar switches between your organizations; the **Organization** section below it (Members, API keys, Settings) is for the current one. See [Organizations and API keys](../accounts/organizations-and-api-keys.md).

The console hides what you cannot do: regular accounts have no **Instance** section in the sidebar and no action buttons on servers, and members and viewers don't see API keys. The API enforces the same rules, so a hidden button is not the only protection. Lewbox also refuses to remove or disable the last enabled instance admin.

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

Lewbox takes the client IP from the connection, never from `X-Forwarded-For`. Behind a proxy, every request appears to come from the proxy, so all users share one per-IP sign-in budget. See [Security](../reference/security.md#browser-sessions) and [Configuration](../reference/configuration.md) for the full list.

See [Run in production](../setup/run-in-production.md#put-tls-in-front) for a reverse-proxy example.

## Next

- [Quickstart](quickstart.md): a working node in about five minutes.
- [Manage accounts](../accounts/manage-accounts.md): instance admins, passwords and disabling.
- [Security](../reference/security.md): what is stored and how secrets are protected.
