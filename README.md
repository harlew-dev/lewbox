# Lewbox

**Run your own sandbox cloud.** Lewbox turns Linux servers into a multi-tenant sandbox platform on [AerolVM](https://github.com/aerol-ai/microvm). It installs AerolVM on your machines and joins them into clusters, one per region. Customers sign up, get API keys and run isolated sandboxes on your compute with the AerolVM or E2B SDKs. You set the plans and limits. Lewbox applies them on every cluster and records usage.

Lewbox is one Go binary with one SQLite file. The same `lewbox` binary is the server (API, web console, docs and background jobs) and the CLI that sets up machines.

[![Release](https://img.shields.io/github/v/release/harlew-dev/lewbox)](https://github.com/harlew-dev/lewbox/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## How it works

```
  operator (lewbox CLI, /admin) ───►  orchestrator  ──── admin token ───►  cluster "eu-west"
                                     (lewbox serve)                        ├── node eu-west-1
  customers (portal at /)  ────────►  API · console · docs                 └── node eu-west-2
                                     SQLite
                                                   customers' apps ──────►  cluster endpoint
                                                   (AerolVM / E2B SDK)
```

- The **orchestrator** (`lewbox serve`) holds accounts, workspaces, plans, clusters and usage. It serves the HTTP API under `/v0`, the customer portal at `/` and the operator console at `/admin`.
- Each **cluster** is a stock AerolVM cluster in one region. On each machine, `lewbox install` and `lewbox node add` set it up and register it.
- Each customer **workspace** is one AerolVM team in one region. Its plan is pushed to the cluster as that team's quota, so AerolVM enforces the limits itself.
- **Lewbox is not in the sandbox request path.** Customers' apps talk to the cluster directly with a sandbox API key, so if the orchestrator goes down, running sandboxes keep working.

## Features

**For operators**

- **Machine setup in a few commands:** `install`, `node add`, `update` and `firewall enable`. Each privileged step runs as a single `sudo` command, printed before it runs. AerolVM's installer is checked against a pinned SHA-256 before it runs, and secrets are masked in all output.
- **Clusters and nodes:** start new clusters, join nodes to them, register clusters you set up without Lewbox, and remove nodes safely.
- **Health monitoring:** cluster checks run every minute. The console shows 30-day uptime per cluster and node metrics (sandboxes, vCPU allocated, memory and disk), and warns before a node's certificate expires.
- **Customers and plans:** sign-up can be open, approval-only or closed. Single-use setup links put a new customer straight onto a plan and region. You can approve, suspend and restore workspaces.
- **Fleet view:** see every customer's sandboxes in one place, so abuse is easy to spot.

**For customers**

- **Self-serve workspaces** with members and roles.
- **Sandbox API keys** for the AerolVM SDK (TypeScript and Python), the E2B SDK and curl. Each key is shown once, with settings ready to copy.
- **Sandboxes and snapshots,** usage charts, and limit meters for their plan.
- **A management API** with `lba_` keys, so their backend can automate the workspace.

**For automation**

- Every operation is available both over HTTP under `/v0` and as a `lewbox` command. A command's `--json` output is byte-for-byte the API response.
- The CLI works locally on the orchestrator's machine, or remotely after `lewbox login https://lewbox.example.com`.

## Quickstart

You need a Linux machine with systemd and a user that can run `sudo` (a VM is fine). See [Requirements](docs/platform/setup/requirements.md) for ports and multi-node networking.

### 1. Get the binary

Download a release (amd64 and arm64):

```bash
curl -fLO https://github.com/harlew-dev/lewbox/releases/latest/download/lewbox_linux_amd64
curl -fLO https://github.com/harlew-dev/lewbox/releases/latest/download/checksums.txt
sha256sum --ignore-missing -c checksums.txt
install -D -m 755 lewbox_linux_amd64 ~/.local/bin/lewbox
LINE='export PATH="$HOME/.local/bin:$PATH"'      # keep it on PATH after you reconnect
for f in ~/.profile ~/.bashrc; do grep -qxF "$LINE" "$f" 2>/dev/null || echo "$LINE" >> "$f"; done
export PATH="$HOME/.local/bin:$PATH"
```

Or build from source (needs Go 1.27+ and Node 24+):

```bash
git clone https://github.com/harlew-dev/lewbox.git
cd lewbox
make                      # builds the web console, then bin/lewbox with it embedded
```

### 2. Start the orchestrator

```bash
LEWBOX_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
LEWBOX_BOOTSTRAP_ADMIN_PASSWORD=change-me-please \
  lewbox serve            # http://localhost:8080
```

On its first start, Lewbox creates the admin account from these two variables. Data goes to `~/.local/share/lewbox/`. Open `http://localhost:8080/admin` to sign in to the operator console.

### 3. Turn a machine into a cluster

In a second terminal on the same machine:

```bash
lewbox install --public-host 10.0.0.5          # installs AerolVM; use this machine's IP
lewbox node add node-1 --new-cluster dev       # starts cluster "dev" here and registers it
lewbox cluster check dev
```

Add more machines with `lewbox install` and `lewbox node add node-2 --cluster dev`. For production, use `--domain` to get wildcard TLS. Running `lewbox install` with no flags uses local mode, which listens on `127.0.0.1` only. That is fine for a quick look, but it can't join a cluster or serve customer workspaces. See [Start a cluster](docs/platform/clusters/start-a-cluster.md) and [Add nodes](docs/platform/clusters/add-nodes.md).

### 4. Onboard a customer

Create a plan, then let people sign up or send them a setup link. From the console, or from the CLI:

```bash
lewbox setup-link create --plan pro --region dev
```

See [Customers, plans and limits](docs/platform/accounts/customers-and-plans.md). For what your customers see, read the [customer quickstart](docs/consumer/getting-started/quickstart.md).

Want a multi-node cluster without spare servers? [Try it with Docker](docs/platform/getting-started/try-with-docker.md).

## Running in production

| Option | When to use it |
|---|---|
| [Run in production](docs/platform/setup/run-in-production.md) | A systemd service with TLS in front and backups |
| [Run with Docker](docs/platform/setup/run-with-docker.md) | The orchestrator image with a data volume (`compose.yaml` in this repo) |
| [VPS deployment](deploy/vps/README.md) | A hardened VPS with Caddy (automatic TLS), release-based deploys with rollback, and nightly backups |

Run the orchestrator on its own machine, apart from the AerolVM nodes, so it stays up when a node goes down. Always back up `lewbox.db` and `keyring.key` together: the keyring holds the key that encrypts each cluster's admin token.

## Documentation

Both books are built into the binary and served by the console: the customer book at `/docs` and the platform book at `/platform`. You can also read them on GitHub:

- **[Platform documentation](docs/platform/README.md)** is for operators: setup, clusters, accounts, operations, the [CLI reference](docs/platform/reference/cli.md), the [HTTP API](docs/platform/reference/http-api.md), [configuration](docs/platform/reference/configuration.md) and [security](docs/platform/reference/security.md).
- **[Customer documentation](docs/consumer/README.md)** is for workspace members: API keys, the [AerolVM SDK](docs/consumer/sandboxes/aerolvm-sdk.md), the [E2B SDK](docs/consumer/sandboxes/e2b.md), usage and limits, and the [management API](docs/consumer/reference/management-api.md).
- **[Design notes](docs/design/)** explain how Lewbox integrates with AerolVM and what was verified against real clusters.

## Development

```bash
make help        # list every target
make             # build the web console and bin/lewbox
make run         # build, then serve on :8080
make dev         # Vite dev server on :5173 with hot reload (run `make run` alongside)
make test        # gofmt, go vet, Go tests, web typecheck and tests
```

The Go tests use an in-memory fake AerolVM cluster, with no network or Docker needed. For anything that touches a machine, run the end-to-end test against real AerolVM in Docker (about 6 minutes, needs privileged containers):

```bash
cd deploy/dev-cluster && ./e2e.sh
```

| Path | What |
|---|---|
| [`cmd/lewbox/`](cmd/lewbox/) | The CLI (cobra) and `serve` |
| [`internal/service/`](internal/service/) | All business logic, shared by the HTTP API and the CLI |
| [`internal/httpapi/`](internal/httpapi/), [`internal/client/`](internal/client/) | The `/v0` API and its Go client |
| [`internal/db/`](internal/db/) | SQLite, goose migrations and sqlc queries |
| [`internal/aerolvm/`](internal/aerolvm/) | AerolVM admin API client and fake cluster |
| [`internal/machine/`](internal/machine/) | Node-side work: installer verification, `sudo` runner, checks |
| [`web/`](web/) | The React console (Vite, TypeScript), embedded in the binary |
| [`docs/`](docs/) | The customer and platform books |
| [`deploy/`](deploy/) | Docker dev cluster, image smoke test, VPS deployment |
| [`plans/`](plans/README.md) | The product spec and [decisions](plans/DECISIONS.md) |

Before contributing, read [AGENTS.md](AGENTS.md). It covers the project's rules: logic lives in the service layer, CLI and API output must match, errors and auth follow fixed patterns, and code that touches a machine has safety rules. The console has its own guide in [web/README.md](web/README.md).

## Status

Lewbox is early (v0.2). It has been tested end to end against real AerolVM v0.7.0 clusters, which is the version it installs by default. See [plans/STATUS.md](plans/STATUS.md) for what is built and how it was verified.

Not built yet: billing, email (verification, password reset, invitations), webhooks and cluster-wide rolling upgrades of AerolVM.

## License

[MIT](LICENSE) © 2026 Harrison Lewis
