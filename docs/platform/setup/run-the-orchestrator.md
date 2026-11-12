---
title: Run the orchestrator
description: Start the Lewbox server, create the first admin, and know where its data lives.
---

The orchestrator is `lewbox serve`. It serves the API, keeps the database, and checks every registered cluster in the background.

> [!TIP]
> Prefer Docker? See [Run with Docker](run-with-docker.md): one container, one command.

## Start it

```bash
lewbox serve                        # port 8080 on every interface
lewbox serve --port 8000
lewbox serve --listen 127.0.0.1:8000
```

`lewbox orchestrator serve` is the same command. Use `--port` or `--listen`, not both.

Check it from the same machine:

```bash
lewbox orchestrator status --port 8000
```

```text
healthy  : true
listening: true
version  : dev
```

## Create the first admin

Pick one of these two ways.

**On the orchestrator's machine**, the CLI opens the database directly, with no sign-in:

```bash
lewbox user create you@example.com --instance-admin
```

It asks for a password twice (at least 12 characters).

**When it starts**, for containers and automation, set these and the orchestrator creates the account if there are no accounts yet:

```bash
export LEWBOX_BOOTSTRAP_ADMIN_EMAIL=you@example.com
export LEWBOX_BOOTSTRAP_ADMIN_PASSWORD_FILE=/run/secrets/lewbox-admin   # or LEWBOX_BOOTSTRAP_ADMIN_PASSWORD
lewbox serve
```

It does nothing once any account exists, and it never logs the password.

Then sign in from anywhere: `lewbox login http://your-host:8080`. See [Sign in](../accounts/sign-in.md).

## Web console

The orchestrator serves the web console on the same origin as the API: open `http://your-host:8080/` and sign in with any account. The same app renders this documentation at `/platform`, including `/platform/reference/cli`.

If the page says the console isn't built, the binary has no `web/dist` yet. Run `make` (or `make web` if you only need the console), then restart `lewbox serve`. While working on the console or the docs, `LEWBOX_WEB_DIR` and `LEWBOX_DOCS_DIR` serve them from disk instead. See [Use the web console](../getting-started/web-console.md).

## Where its data lives

| File | Default | What it is |
|---|---|---|
| Database | `~/.local/share/lewbox/lewbox.db` | SQLite: accounts, sessions, clusters, nodes. Set with `--db` or `LEWBOX_DB` |
| Keyring | `keyring.key`, next to the database | The key that encrypts each cluster's admin token. Set with `--keyring` or `LEWBOX_KEYRING_FILE` |

Both are created on first use, in a folder only you can open (`0700`); the keyring itself is `0600`, and Lewbox refuses to use one that others can read. `$XDG_DATA_HOME` moves the default folder.

> [!WARNING]
> **Back up the keyring with the database.** Without it, Lewbox can't decrypt the cluster admin tokens it stored, and you'd have to register every cluster again.

## Background checks

Every minute, the orchestrator runs [the cluster checks](../clusters/health-and-checks.md) on every cluster and updates its status and node list. Change the interval with `--check-interval 30s` (or `LEWBOX_CHECK_INTERVAL`); `0` turns it off.

## Next

- [Run in production](run-in-production.md): a systemd service, TLS, and backups.
- [Start a cluster](../clusters/start-a-cluster.md)
- [Configuration reference](../reference/configuration.md): every flag, environment variable and file.
