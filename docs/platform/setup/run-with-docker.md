---
title: Run with Docker
description: Run the orchestrator, web console and docs in one container, and keep their data in a volume.
---

The `Dockerfile` in the repository builds the whole orchestrator: the API, the web console and this documentation in one binary. Docker Compose runs it with one command.

You need Docker with the Compose plugin (Docker 25 or newer). The image runs as an unprivileged user and needs no privileges.

## Start it

From the repository root:

```bash
LEWBOX_ADMIN_PASSWORD='pick-a-long-password' docker compose up -d --build
```

The password must be at least 12 characters. Lewbox creates the first instance admin (`admin@example.com` unless you set `LEWBOX_ADMIN_EMAIL`) only while there are no accounts, so changing these variables later does not reset anything.

Open [http://localhost:8080](http://localhost:8080) and sign in. The same origin serves the API, the console, `/docs` and `/platform`.

| Variable | Default | |
|---|---|---|
| `LEWBOX_ADMIN_EMAIL` | `admin@example.com` | The first admin's email |
| `LEWBOX_ADMIN_PASSWORD` | *required* | Its password, at least 12 characters |
| `LEWBOX_HTTP_PORT` | `8080` | Host port to publish |
| `LEWBOX_VERSION` | `dev` | Value for `lewbox version` in the image |

To run the image without Compose, pass the same variables and mount a volume at `/data`; the image already listens on `:8080` and runs `lewbox serve`.

## Where its data lives

Everything is in the `lewbox-data` volume:

| File in the volume | What it is |
|---|---|
| `/data/lewbox/lewbox.db` | SQLite: accounts, sessions, clusters, nodes |
| `/data/lewbox/keyring.key` | The key that encrypts each cluster's admin token |

> [!WARNING]
> **Back up the keyring with the database.** Without `keyring.key`, the cluster admin tokens in the database can't be decrypted and every cluster has to be registered again.

Both files are created on first start, in a folder only the container's user can open. To copy the volume without stopping the container:

```bash
docker volume ls | grep lewbox-data     # <project>_lewbox-data
docker run --rm -v lewbox_lewbox-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/lewbox-data.tgz -C /data .
```

Store that archive somewhere safe: it can decrypt every registered cluster's admin token.

## Admin commands

The container has the CLI, and `docker compose exec lewbox lewbox …` runs it against the same database with no sign-in:

```bash
docker compose exec lewbox lewbox user list
docker compose exec lewbox lewbox user create you@example.com --instance-admin
docker compose exec lewbox lewbox version
```

Pass `-T` when scripting, for example `docker compose exec -T lewbox lewbox user list --json`.

From your own machine, `lewbox login http://your-host:8080` works too. See [Sign in](../accounts/sign-in.md).

## TLS

The container speaks plain HTTP. Put a reverse proxy or load balancer in front and give Lewbox an HTTPS hostname. If the proxy sets `X-Forwarded-Proto: https`, the session cookie is marked `Secure` automatically; if it does not, set `LEWBOX_COOKIE_SECURE=always` (uncomment it in `compose.yaml`).

If a proxy rewrites the `Host` header, add the public origin to `LEWBOX_TRUSTED_ORIGINS` so browser requests pass the cross-site check. See [Run in production](run-in-production.md#put-tls-in-front).

## Run the checks in Docker

Run every check in the image, as a non-root user, with no host toolchain:

```bash
make docker-test
```

It builds the image's `test` and `web-test` stages: `gofmt`, `go vet`, the Go tests, and the web console's typecheck and tests.

Compose builds through the daemon's BuildKit, so you do **not** need the Docker `buildx` plugin.

## Upgrade

Pull the new source, rebuild, and recreate the container. The volume is kept:

```bash
git pull
LEWBOX_ADMIN_PASSWORD='...' docker compose up -d --build
```

Check that the new version is running with `docker compose exec lewbox lewbox version`. Take a backup of the volume first if the release notes mention a migration.

## Next

- [Run in production](run-in-production.md): systemd, TLS in front, backups.
- [Configuration](../reference/configuration.md): every variable and flag.
- [Try it with Docker](../getting-started/try-with-docker.md): a real multi-node AerolVM cluster, not just the orchestrator.
