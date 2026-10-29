# The binary and its commands

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

Lewbox is one Go program, `lewbox`, with subcommands for each job it does: the control plane, migrations, instance administration, the node agent, the in-sandbox helpers, and the CLI. This plan covers the subcommands, process roles, startup and shutdown, and how the helpers that run inside sandboxes are built, embedded, uploaded and kept in step.

## Today

- **Three binaries, built separately**, in `lewbox-gateway-legacy/cmd/`: `lewbox-gateway` (the Consumer API), `lewbox-portd` (the private-port proxy) and `lewbox-agentd` (the agent runner, with `serve`, `call` and `events` subcommands, and a Codex proxy mode chosen by the `LEWBOX_AGENTD_CODEX` env var).
- **Helpers found by directory.** The gateway reads `lewbox-portd-linux-{amd64,arm64}` from `GATEWAY_PORTD_DIR` and `lewbox-agentd-linux-*` from `GATEWAY_AGENTD_DIR` at start (`internal/privateports.LoadBinaries`, `internal/agents`). If they're missing, private ports or conversations are silently off.
- **Version by content hash.** `internal/agents` hashes each build (`sha256`, first 6 bytes in hex) and installs it at `/usr/local/lib/lewbox/agentd-<version>`, writing a `.part-…` file, `chmod`, checking the mode (gVisor), then renaming. An idle daemon of another build answers `upgrade` and exits; the gateway then starts the new one.
- **The app is Laravel**: `php artisan serve`, `queue:work`, `schedule:work`, plus `php artisan dev` (`app/Console/Commands/RunDevProcesses.php`) to run a Cloudflare tunnel and `../lewbox-gateway` together locally.
- **Nodes run bash**: `resources/scripts/install.sh` (1,246 lines) installs AerolVM and systemd timers for certificates, firewall, metrics and snapshot cleanup. Installed nodes keep the scripts they were installed with.
- **Shutdown** (`cmd/lewbox-gateway/main.go`): stop accepting connections, give requests `GATEWAY_SHUTDOWN_TIMEOUT` (30 s), close streams with `gateway_restarting` (WebSocket close 1001), then stop loops. Operations are left to lapse and be resumed elsewhere.

## Goals

- One `lewbox` binary that does every job, so there's one thing to download, version and upgrade.
- Helpers that can't be missing: embedded at build time, uploaded on demand, verified by hash.
- Clean, fast startup with every misconfiguration reported at once; graceful shutdown that loses nothing.
- Roles, so a large instance can split API and workers across processes if it wants, without a different build.

## Design

### Subcommands

```
lewbox serve [--role all|api|worker] [--addr :8080] [--migrate] [--dev]
lewbox migrate up|down [N]|status|create NAME|verify
lewbox admin <command>               instance administration (below)
lewbox node install|run|status|update|uninstall
lewbox agentd serve|call|events|codex-proxy
lewbox portd
lewbox version [--json]
lewbox config check|print [--defaults]
lewbox plugins list|info NAME           the plugins in this build, and which are on
lewbox build --with MODULE[@V]… [--without NAME…] [--output PATH]   a custom build with other plugins
lewbox <cli command>                 sandboxes, exec, files, ports, conversations… (see sdks-and-cli)
```

| Command | What it does | Plan |
|---|---|---|
| `serve` | The control plane: HTTP (API, dashboard, node API, hooks, uploads), workers and the periodic schedule | This plan |
| `migrate` | Runs and checks migrations. `verify` checks the database's schema matches what this build expects, for CI and readiness | [database](database.md) |
| `admin` | Things an instance admin does from a shell, mostly before the dashboard is usable | Below |
| `node` | The node agent: installs and supervises our AerolVM fork, enrolls, keeps certificates, firewall, metrics and itself up to date | [clusters/node-agent](../clusters/node-agent.md) |
| `agentd` | Runs coding agents inside a sandbox | [agents/agentd](../agents/agentd.md) |
| `portd` | Private-port proxy inside a sandbox | [inside-a-sandbox/private-ports](../inside-a-sandbox/private-ports.md) |
| `version` | Version, commit, build date, Go version, embedded helper hashes, supported AerolVM fork versions | [build-and-release](build-and-release.md) |
| `config` | `check` validates configuration and exits 0 or 1 with every problem; `print` shows the effective settings with secrets redacted, and where each came from | [configuration](configuration.md) |
| `plugins` | `list` shows the plugins compiled in, their versions and whether this instance's configuration turns them on; `info` shows one's settings schema, dependencies, permissions, events and tables | [plugins/building-and-enabling](../plugins/building-and-enabling.md) |
| `build` | Builds a custom binary with more or fewer plugins, the `xcaddy` way; needs Go (and Node for plugins with UI) | [plugins/building-and-enabling](../plugins/building-and-enabling.md#custom-builds) |
| CLI | The client for the API, with `LEWBOX_API_KEY` | [sdks-and-cli](../sdks-and-cli.md) |

Subcommand parsing uses the standard `flag` package with a small dispatcher, or `spf13/cobra` if the CLI's needs (completion, help groups) make it worth it. The CLI plan decides; the server commands don't need cobra.

#### `lewbox admin`

For first-run and break-glass work, talking to the database directly with the server's configuration:

| Command | Does |
|---|---|
| `admin users create --email E [--instance-admin]` | Makes a user, optionally an instance admin |
| `admin users grant-instance-admin --email E` / `revoke-instance-admin` | |
| `admin projects create --name N --owner E` | Makes a project with an owner |
| `admin keys create --project P --name N` | Makes an admin key, printed once |
| `admin keys revoke KEY_ID` | |
| `admin secrets rotate` | Starts re-encrypting under the keyring's primary key ([secrets-and-encryption](secrets-and-encryption.md#rotation)) |
| `admin secrets status` | How many values are on each key |
| `admin jobs list|retry|cancel` | Inspect the job queue |
| `admin operations list|fail OP_ID` | Unstick an operation by hand, recording who did it |
| `admin login-link --email E` | Prints a one-time sign-in link, for an instance without email set up |

Each writes an audit entry ([observability](observability.md#audit)).

### Process roles

`lewbox serve --role` (env `LEWBOX_ROLE`) chooses what a process does: `all` (the default), or a comma-separated list such as `api,worker`. `all` includes `relay` when `LEWBOX_RELAY_DOMAIN` is set (Beta).

| Role | HTTP | Job workers | Periodic schedule (leader only) | Stream relays |
|---|---|---|---|---|
| `all` | Yes | Yes | Takes part in leader election | Yes |
| `api` | Yes | No (inserts jobs only) | No | Yes |
| `worker` | Only `/up`, `/ready`, `/metrics` | Yes | Takes part | No |
| `relay` (Beta) | The relay's listeners only ([clusters/connectivity](../clusters/connectivity.md#the-relay)) | No | No | No |

A homelab runs one `all`. lewbox.com might run three `all` replicas, or split API and workers. The code doesn't care: anything a handler needs from a worker goes through Postgres.

Note that requests inside a sandbox (commands, files, streams) are done *by the API process* handling the request: they're not jobs. Only work that outlives a request is a job ([jobs-and-scheduling](jobs-and-scheduling.md)).

### Startup

`lewbox serve`:

1. Load and validate configuration. Report every problem, exit 2 ([configuration](configuration.md)).
2. Set up logging, metrics, tracing ([observability](observability.md)).
3. Load the keyring and check it can decrypt a sentinel ([secrets-and-encryption](secrets-and-encryption.md)).
4. Connect to Postgres (retry with backoff for up to `LEWBOX_DATABASE_CONNECT_TIMEOUT`, 60 s, so a container starting beside its database works).
5. Migrations: with `--migrate` (env `LEWBOX_MIGRATE_ON_START`, default `true` for `all`), run pending migrations under an advisory lock so concurrent replicas don't race. Otherwise check the schema is current and refuse to start if it's behind (it may be *ahead*, during a rolling deploy: see [build-and-release](build-and-release.md#upgrades)).
6. Load embedded assets: verify the `lewbox-sandbox` builds' hashes against the manifest compiled into the binary.
7. Sync provided templates and other code-defined data ([templates/provided-templates](../templates/provided-templates.md)).
8. Start the `NOTIFY` listener, workers, leader election ([jobs-and-scheduling](jobs-and-scheduling.md)).
9. Start the HTTP server. `/ready` answers 200 only once steps 1–8 are done.

### Shutdown

On `SIGTERM` or `SIGINT`:

1. `/ready` starts answering 503 so load balancers drain the replica. Wait `LEWBOX_SHUTDOWN_DRAIN` (default 5 s) for them to notice.
2. Stop accepting connections; give in-flight ordinary requests up to `LEWBOX_SHUTDOWN_TIMEOUT` (30 s).
3. Streams (SSE, WebSockets) end themselves at once, telling the client why: an SSE `error` event with code `server_restarting`, and WebSocket close 1001. Clients reconnect to another replica and resume (`?after=`, attach replay). The legacy code was `gateway_restarting`; the new name is decided in [api/streaming](../api/streaming.md).
4. Workers stop claiming new jobs. Jobs in flight get the same timeout to finish their current step; then their context is cancelled. Their leases lapse and another replica resumes them from the last recorded step. Nothing is failed because of a shutdown.
5. The leader gives up its lock, so another replica takes over the schedule at once rather than after the lock's timeout.
6. Close the database pool. Exit 0.

A second signal exits at once.

### The in-sandbox helpers

`agentd` and `portd` run *inside sandboxes*, which may be amd64 or arm64 Linux with any userland, so they must be:

- **Static**: `CGO_ENABLED=0`, no libc.
- **Small**: they're uploaded on first use, sometimes over slow links.
- **Exactly the version the control plane expects.**

So there's a second build target, `lewbox-sandbox`, from the same module with the `sandbox` build tag. It contains only `agentd`, `portd` and `version` (and `agentd codex-proxy` from the Codex plugin), plus each agent plugin's `agentd` driver package, and none of the server, the dashboard or the database code. Its main file (`cmd/lewbox/main_sandbox.go`, `//go:build sandbox`) registers only those subcommands.

```
make sandbox      → dist/sandbox/lewbox-sandbox-linux-amd64
                    dist/sandbox/lewbox-sandbox-linux-arm64
                    dist/sandbox/manifest.json   {"linux/amd64": {"sha256": "…", "size": …}, …}
make build        → copies dist/sandbox/* into internal/embedded/sandbox/ and builds lewbox
```

The full binary embeds them with `//go:embed sandbox/*` in `internal/embedded`. They're stored zstd-compressed to keep the full binary small, and decompressed once into memory on first use, then cached.

The full binary also has `agentd` and `portd` subcommands itself, for development and tests on a Linux host; production always uses the embedded build.

#### Upload and install

One library, `internal/sandboxhelper`, used by agents, private ports and anything later:

1. **Choose the build** by the sandbox's architecture (the cluster's, recorded on the sandbox).
2. **Path:** `/usr/local/lib/lewbox/lewbox-sandbox-<hash12>`, where `hash12` is the first 12 hex characters of the build's SHA-256. A new build gets a new path, so it sits beside the old one, which may still be running.
3. **Check:** `test -x <path> && sha256sum <path>` through the toolbox. If it's there and matches, done.
4. **Upload** to `<path>.part-<random>` through the toolbox's file API, `chmod 0755`, then `mv` into place. (Legacy also checked the mode with `stat`, as gVisor once ignored `chmod`, `4f0da62`; gVisor isn't in the alpha and Lewbox carries no workarounds, [D15](../README.md#decisions).) Two concurrent installs can't run a half-written file.
5. **Clean up** builds older than the last two, best-effort, after a successful install.

The legacy code installed per helper (`portd-<sha12>`, `agentd-<version>`); now both are one file.

#### Version handshake

- **agentd** reports its build hash on every call. The control plane passes the hash it expects in each request. An idle daemon of another build answers `upgrade` and exits; a busy one finishes its turns first and then exits, and the next call starts the new build ([agents/agentd](../agents/agentd.md)). Conversation state on disk (`/root/.lewbox/agents/<cnv>/`) has a format version, so a new agentd can read an old one's files.
- **portd** is started per port with its hash in its session name (`lewbox-portd-<port>-<hash12>`). A new build takes effect when the port is next exposed or the sandbox next starts. Running proxies keep working: links are checked against the public keys they were started with ([secrets-and-encryption](secrets-and-encryption.md#signing-keys)).
- **The node agent** reports its version in each heartbeat; the control plane says which version it wants, and the agent updates itself ([clusters/node-agent](../clusters/node-agent.md), [build-and-release](build-and-release.md#upgrades)).

#### Arguments go in files

Linux limits one argument to 128 KiB (`MAX_ARG_STRLEN`). Requests to helpers are written to a file and passed by path, never inline (legacy commit `4776361`). The helper deletes the file after reading it.

### Plugins in the binary

Plugins ([plugins](../plugins.md)) are compiled in; nothing is loaded at run time. `plugins/config.json` lists them, and `go generate ./plugins` writes one file per build target ([plugins/building-and-enabling](../plugins/building-and-enabling.md#the-generator)):

| Generated file | Build tag | Imports |
|---|---|---|
| `cmd/lewbox/plugins_gen.go` | default | Every plugin package, for its `init()` registration, with the list's order and defaults |
| `cmd/lewbox/plugins_sandbox_gen.go` | `sandbox` | Each agent plugin's `agentd` sub-package (its driver) |
| `cmd/lewbox/plugins_cli_gen.go` | `cli` | Each plugin's `cli` sub-package (its commands) |

At start-up `serve` loads the registry before anything else that depends on it: resolve dependencies, read each plugin's configuration, call `Register` in order, freeze the registries, then migrate (core, then plugins) and `Start` the plugins on for the instance ([plugins/registry-and-lifecycle](../plugins/registry-and-lifecycle.md#the-registry)). `lewbox version` lists the plugins compiled in and whether the build is a release, a custom build or a development build.

The dependency guard extends to plugins: a test fails if any `internal/…` package imports a `plugins/…` package (the core never imports a plugin), if a plugin imports `internal/…` (it uses `plugin/…` only), or if a plugin's `agentd` or `cli` sub-package pulls in server code.

**A core-only build** (`go build -tags noplugins`, whose generated files import nothing) is built and tested in CI, so the core never quietly depends on a plugin.

### One binary or two

The full binary embeds the dashboard (~3–5 MB), two `lewbox-sandbox` builds (~10–15 MB each, compressed less), migrations and templates. Expect 80–100 MB uncompressed. That's fine for servers and nodes, but heavy for someone who wants the CLI on a laptop.

Options:

1. **One binary, always.** Simplest. The CLI is the same download.
2. **Also a `lewbox-cli` build** (build tag `cli`): only the client commands, ~15 MB, for macOS, Linux and Windows, and for npm (`@lewbox/cli` wraps it).

Recommended: **option 2**. It's one extra build target with no code of its own, and it matches what the docs promise (`@lewbox/cli`). It includes each compiled plugin's CLI commands, through `plugins_cli_gen.go`. See [sdks-and-cli](../sdks-and-cli.md).

## Configuration

| Setting | Default | |
|---|---|---|
| `LEWBOX_ROLE` | `all` | `all`, or a list of `api`, `worker` and (Beta) `relay` |
| `LEWBOX_MIGRATE_ON_START` | `true` | Run pending migrations when `serve` starts |
| `LEWBOX_SHUTDOWN_DRAIN` | `5s` | How long `/ready` answers 503 before the listener closes |
| `LEWBOX_SHUTDOWN_TIMEOUT` | `30s` | How long requests and job steps get to finish |
| `LEWBOX_HELPER_INSTALL_DIR` | `/usr/local/lib/lewbox` | Where helpers go inside sandboxes |

Full list: [configuration](configuration.md).

## Security

- Helpers are uploaded by the control plane, never fetched by the sandbox from anywhere, so nothing in the sandbox can swap the build before it's installed. A sandbox can still replace the file afterwards (it's root in its own sandbox); that's fine, because helpers have no secrets beyond what the sandbox already holds, and what they send is checked by the control plane (agentd's channel token reaches only its own sandbox, [ids-and-keys](ids-and-keys.md#signed-tokens)).
- `lewbox admin` uses the server's database credentials and keyring, so it's as privileged as the server. It's for shells on the control plane's hosts only.
- Releases are signed ([build-and-release](build-and-release.md#signing)).

## Observability

- On start, one log line with version, commit, role, replica id, listen address, the database host (no password) and which features are on.
- `lewbox_build_info{version,commit,go}` gauge.
- `lewbox_helper_installs_total{helper,result}` and `lewbox_helper_install_duration_seconds`.

## Testing

- The dispatcher: every subcommand's `--help`, unknown commands, exit codes.
- Startup: misconfiguration reports every problem; a schema behind the binary refuses to start; one ahead starts.
- Shutdown: requests finish; streams get their `server_restarting` message; a job mid-step resumes on another (test) replica.
- Helper install against the fake toolbox: a fresh install, an existing matching build, a corrupt file, a `chmod` that doesn't take, two installs racing.
- The `sandbox` build compiles without importing server packages: a test runs `go list -deps -tags sandbox ./cmd/lewbox` and fails on any `internal/db`, `internal/httpapi` or similar import, including through an agent plugin's driver.
- The `noplugins` build starts, migrates and passes the sandbox lifecycle suite.

## Build plan

1. `cmd/lewbox` dispatcher with `version`, `config`, `serve` (empty), `migrate`.
2. `internal/app`: wiring for `serve`, roles, startup sequence, shutdown sequence.
3. `cmd/lewbox/main_sandbox.go` and the `sandbox` build tag; dependency-guard test.
4. `internal/embedded`: embed with manifest, zstd, hash check.
5. `internal/sandboxhelper`: choose, check, upload, chmod-verify, rename, clean up.
6. `lewbox admin` commands, as the plans that own each resource land.
7. `lewbox-cli` build target, once the CLI exists.
8. `plugins_gen.go` generation, `lewbox plugins`, the `noplugins` build (M0); `lewbox build` (Beta, [plugins/building-and-enabling](../plugins/building-and-enabling.md#build-plan)).

## Open questions

1. **cobra or `flag`?** Decided with the CLI.
2. **Do we ship the `cli`-only build** (recommended), and under what name?
3. **Should `worker` processes serve in-sandbox requests too**, so a big instance can scale stream relays separately from the management API? Not needed for the alpha.
4. **Helper retention in sandboxes:** keep the last two builds, or only the running ones?
