# Foundation

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

The foundation is everything every other plan stands on: the one binary and its subcommands, how it's configured, the Postgres database, background jobs, ids and tokens, secrets, logs and metrics, how we test, how we build and release, and the plugin registry that features plug into ([plugins](plugins.md)). It's what M0 delivers, before any sandbox is created, so that every later feature is built the same way.

## Today

Legacy Lewbox has two foundations, and much of each exists only to talk to the other.

| Concern | Legacy app (`lewbox-legacy`) | Legacy gateway (`lewbox-gateway-legacy`) |
|---|---|---|
| Process | PHP-FPM, `php artisan schedule:work`, `queue:work` | `cmd/lewbox-gateway` plus `cmd/lewbox-portd` and `cmd/lewbox-agentd` built separately |
| Configuration | `.env` plus `config/lewbox.php` (~50 `LEWBOX_*` settings), `config/aerolvm.php` | Env only, `internal/config` (~40 `GATEWAY_*` settings, `docs/configuration.md`) |
| Database | SQLite in development, MySQL in production; 82 migrations with MySQL workarounds (virtual columns, partial unique indexes recreated after rebuilds) | None. Asks the app over HTTP (`internal/lewbox/client.go`, ~50 methods) |
| Jobs | Laravel `database` queue and scheduler (`routes/console.php`), with lock bugs: a day-long `withoutOverlapping` lock skipped every sign-in pass in production, and `MarkOfflineNodes` still has one | Goroutine loops: operation poll every 1 s per replica, reconciler 60 s, session watcher 5 s, event flush 2 s |
| Ids | `App\Concerns\HasPublicId`: prefix + 14 base62 | Reads the app's ids |
| Secrets | Laravel `encrypted` casts under one `APP_KEY`, which guards every cluster CA, PAT, bucket key and token | `GATEWAY_PORT_LINK_KEY` (Ed25519 seed), `LEWBOX_GATEWAY_TOKEN` shared with the app |
| Logs | Laravel log channels, Nightwatch | `log/slog`, JSON in production |
| Tests | Pest with SQLite, a `PerformanceSeeder` | `go test -race`, fakes `aerolvmtest` (AerolVM cluster and toolbox) and `lewboxtest` (the app, 1,100 lines) |
| Build | No Dockerfile; Laravel Cloud | `Makefile`, `build.sh` for Laravel Cloud, a distroless `Dockerfile` bundling helper builds in directories |

What's wrong with it:

- **Two processes need an RPC layer.** The gateway's 60 s key cache, the operation poll every second, event batching with drop-on-full, a shared static token with no rotation overlap, and the app calling *itself* through the public API with sandbox keys all exist only because the two are separate.
- **Two SQL dialects.** SQLite in tests and MySQL in production forced workarounds into the migrations and pushed sorting into PHP.
- **Scheduler locks that never lapse.** A killed run held its lock for a day.
- **One key for every secret,** with no way to rotate it.
- **Helper binaries found by directory.** If `GATEWAY_PORTD_DIR` was wrong, private ports were off.

## Goals

1. **One process, one database.** `lewbox serve` and Postgres are the whole control plane. Nothing else is required: no Redis, no queue workers, no separate gateway.
2. **No internal HTTP.** Code that needs to know something reads the database. Code that needs work done enqueues a job in the same transaction as the change that needs it.
3. **Any replica can do anything.** Replicas share only Postgres. A replica killed mid-operation loses nothing: its work is picked up when its lease lapses.
4. **Settings in one place, checked at once.** One table of settings, each with a default, validated when the process starts, every problem reported together.
5. **Secrets that can be rotated.** A keyring, not one key, and every secret in the database encrypted under it.
6. **Observable from the first commit.** Request ids on every response, structured logs, Prometheus metrics, optional traces.
7. **Testable without a cluster.** Fakes for AerolVM, the toolbox, GitHub, SMTP and the agent adapters, a real Postgres in tests, and an injectable clock.
8. **One artifact per platform.** Everything the binary needs (the dashboard, the in-sandbox helpers, migrations, the plugins in its build) is embedded.
9. **A small core with plugins compiled in.** Agents, Git hosts, DNS and storage providers, limits, credits and telemetry are plugins against typed interfaces, switched on and off per instance and project ([plugins](plugins.md)).

### Non-goals

- Running without Postgres, or on any other database (D2).
- Loading plugins at run time (Go's `plugin` package) or out-of-process plugins. Plugins are compiled in; the plugin API is internal and unstable until `/v1` ([plugins](plugins.md#non-goals)).
- Windows servers. The CLI builds for Windows; `serve` and `node` are Linux (and macOS for development).

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Public ids as prefix + 14 base62. Token formats (`prefix_` + 64 hex), stored as SHA-256 with last four. `Lewbox-Request-Id` (`req_` + 24 hex). Operations with leases and idempotent steps. The gateway's settings and their defaults, where they still mean something. JSON logs. The `aerolvmtest` and fake toolbox, ported. `-trimpath`, static `CGO_ENABLED=0` builds. |
| **Change** | Two processes → one binary with subcommands. MySQL/SQLite → Postgres. Laravel queue and goroutine polls → a Postgres job queue with `LISTEN/NOTIFY` wake-ups and a leader for periodic work. `APP_KEY` → a rotatable keyring with envelope encryption. Helper binaries in directories → embedded, versioned by content hash. Settings that were "set on nodes as they're installed" → pushed to nodes by the control plane, so changing them reaches existing nodes. |
| **Drop** | The app's internal API for the gateway (`/api/gateway/v1`), `LEWBOX_GATEWAY_TOKEN`, the key and cluster caches as correctness concerns, the event recorder's queue, `lewboxtest`, `packages/laravel-aerolvm` (unused), `build.sh` for Laravel Cloud. |

## Design

### Principles

1. **Postgres is the only state.** Anything a second replica needs is in Postgres. In-memory caches are allowed only where a stale read is harmless, and are invalidated by `NOTIFY`.
2. **Write the change and its consequences together.** A sandbox row, its `create` operation and the job that runs it are inserted in one transaction, as are a status change, its activity event and its webhook deliveries. Nothing is "sent after commit and hopefully not lost".
3. **Every step is idempotent.** Jobs can run twice: after a crash, after a lost lease, after a retry. Each step checks what's already done (legacy: a retried create adopts the AerolVM sandbox tagged with its id).
4. **Time is injected.** Every timer and timestamp goes through a `clock.Clock`, so tests can move time.
5. **Errors are values with codes.** Domain errors carry the API's stable `code` from where they start; handlers don't invent them ([api/errors](api/errors.md)).
6. **Packages by domain, not by layer.** `internal/sandboxes` holds the sandbox service, its queries' callers and its jobs; `internal/httpapi` holds only HTTP concerns.
7. **The core never imports a plugin.** Plugins import the core's `plugin/` API; the core calls them only through the extension points they registered ([plugins/extension-points](plugins/extension-points.md)). A core-only build, with no plugins, must start and pass the lifecycle tests.

### How a request flows

```
HTTP request
  → httpapi middleware: request id, recovery, access log, CORS, body limits
  → auth: key or session → Principal{project, profile?, key, permissions, scope}
  → handler: decode + validate → domain service call
  → service: db.Tx(ctx, func(q) { …reads, checks, writes, jobs.Insert(…), activity.Record(…) })
  → after commit: NOTIFY wakes workers and streams
  → handler: encode response (or wait on an operation, or stream)
```

### How background work flows

```
jobs.Insert in a transaction ──commit──▶ NOTIFY ──▶ a worker on any replica claims it (SKIP LOCKED)
                                                     → runs it with its own context and lease
                                                     → succeeds, or snoozes, or retries with backoff
leader replica ──▶ periodic schedule ──▶ inserts periodic jobs (unique per period)
```

Details: [jobs-and-scheduling](foundation/jobs-and-scheduling.md).

### What the binary contains

```
lewbox (linux, darwin; amd64, arm64)
├── serve       control plane: API, dashboard, workers
├── migrate     database migrations
├── admin       instance administration from a shell
├── node        the node agent
├── agentd      ┐ also in the small `lewbox-sandbox` build,
├── portd       ┘ which is embedded for upload into sandboxes
├── version
└── (CLI)       sandboxes, exec, files, ports, conversations… (see sdks-and-cli)

embedded: web/dist (dashboard, with plugins' UI) · lewbox-sandbox for linux/amd64 and linux/arm64 (with agent plugins' drivers)
          · SQL migrations (core's and each plugin's) · provided templates · AerolVM fork manifest
compiled in: the plugins listed in plugins/config.json (plugins_gen.go)
```

Details: [binary-and-commands](foundation/binary-and-commands.md).

## Parts

| Plan | What it covers |
|---|---|
| [binary-and-commands](foundation/binary-and-commands.md) | Subcommands and their flags, process roles, startup and shutdown, the in-sandbox helper build, embedding and uploading it, version handshakes |
| [configuration](foundation/configuration.md) | Env and file, precedence, validation, every setting with its default, what moved to project settings, legacy → new names |
| [database](foundation/database.md) | Postgres, pgx, sqlc, migrations, transactions, conventions, the full initial table list by area |
| [jobs-and-scheduling](foundation/jobs-and-scheduling.md) | River vs our own queue, leases, retries, `LISTEN/NOTIFY`, leadership, every periodic job |
| [ids-and-keys](foundation/ids-and-keys.md) | Public id format and every prefix, token formats, hashing, signed tokens, request ids |
| [secrets-and-encryption](foundation/secrets-and-encryption.md) | The keyring, envelope encryption, rotation, what's encrypted and what's hashed, signing keys, replacing `APP_KEY` |
| [observability](foundation/observability.md) | Logs, metrics, traces, request ids, health and readiness, profiling |
| [testing](foundation/testing.md) | Test layers, Postgres in tests, every fake, the clock, end-to-end against real nodes, load tests |
| [build-and-release](foundation/build-and-release.md) | CI, builds, embedding, artifacts and images, versioning, upgrades, the AerolVM fork's releases, custom builds with plugins |
| [plugins](plugins.md) | The plugin registry, extension points, building and enabling, and each provider kind (its own plan area) |

## Build plan

M0, in order. Each item is a pull request or a few.

1. Repository skeleton: `cmd/lewbox`, `internal/app`, `internal/config`, `Makefile`, CI with gofmt, vet, staticcheck, `go test -race` against Postgres.
2. Configuration loader and the settings table ([configuration](foundation/configuration.md)).
3. Postgres: pool, `db.Tx`, migrations runner, sqlc set-up, first migrations (users, projects, keys) ([database](foundation/database.md)).
4. Ids and tokens ([ids-and-keys](foundation/ids-and-keys.md)), keyring and `secrets.String` ([secrets-and-encryption](foundation/secrets-and-encryption.md)).
5. Jobs: River set-up, operation lease sweeper, leader-only periodic schedule, `NOTIFY` hub ([jobs-and-scheduling](foundation/jobs-and-scheduling.md)).
5a. The plugin registry: `plugin/` API, `plugins/config.json` and the generator, enablement, plugin migrations, route gating, the core-only CI build ([plugins](plugins.md#build-plan)).
6. HTTP server: middleware, request ids, errors, `/up`, `/ready`, `/metrics` ([observability](foundation/observability.md), [api](api.md)).
7. Test kit: Postgres template databases, the clock, `aerolvmtest` ported, `githubtest`, `smtptest` ([testing](foundation/testing.md)).
8. The `lewbox-sandbox` build, embedding, and the upload and handshake library ([binary-and-commands](foundation/binary-and-commands.md)).
9. Release pipeline: snapshot builds on every merge, signed tagged releases, the container image ([build-and-release](foundation/build-and-release.md)).
10. AerolVM fork: repository, CI, release assets, first `-lewbox.1` tag ([clusters/aerolvm-fork](clusters/aerolvm-fork.md)).

## Open questions

1. **River or our own queue?** Recommended: River for dispatch, with operations keeping their own leases ([jobs-and-scheduling](foundation/jobs-and-scheduling.md#river-or-our-own)). River is MPL-2.0; check that's acceptable alongside MIT.
2. **One binary for everything, or a slim CLI build too?** The full binary embeds the dashboard and two helper builds (~80–100 MB). A CLI user may not want that ([binary-and-commands](foundation/binary-and-commands.md#one-binary-or-two)).
3. **Partition the big tables** (activity events, node metrics, conversation chunks) by month from the start, or add it later ([database](foundation/database.md#partitioning))?
4. **A KMS for the keyring's root key** (AWS KMS, GCP KMS, Vault transit) in the alpha, or later ([secrets-and-encryption](foundation/secrets-and-encryption.md#kms))?
5. **Configuration file format:** TOML or YAML ([configuration](foundation/configuration.md#file))?
6. **Checksummed tokens** (a CRC suffix for secret scanning), now that the token format can change ([ids-and-keys](foundation/ids-and-keys.md#checksums))?
