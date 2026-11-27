---
name: tooling
description: The local development loop and CI for any stack — one command surface (make targets or an equivalent task runner) shared by laptops and CI, one-terminal `make dev`, Docker Compose services, `.env.example` and the committed `.env.testing`, the emptied test environment, guarded test databases, parallel e2e runs with port locks, preview-then-confirm operator commands, dev-server exposure, and the CI pipeline. Use when setting up a new project's tooling, adding or changing a make target or script, touching env files or compose, running or debugging the dev server, tests or e2e locally, adding a destructive command, or fixing or extending CI.
---

# Dev environment

A new developer (or agent) should get from a clean checkout to a running app with a handful of documented commands, and the commands that pass on their laptop are the commands CI runs. Every rule here exists because its absence caused a real failure: tests that ran against the dev database, two e2e runs wiping each other's data, a stale build served by the dev server, a mailbox full of working reset links reachable from the café Wi-Fi, a CI that went green while skipping the database tests.

Examples use a Makefile, Docker Compose, Postgres, a Go API and an npm frontend with Playwright, because that is what the previous project used. They illustrate a **stack-neutral command surface**: read `make test` as "the project's `test` command", whether that is `make`, `just`, Task, `npm run`, `composer`, `mix` or a `bin/` script.

## When to use

- Setting up tooling for a new project, or adding, renaming or changing a command, script or target.
- Touching `.env.example`, `.env.testing`, `.gitignore`/`.dockerignore`, `compose.yaml` or the dev Dockerfile stages.
- Running or debugging the dev server, the test suites or e2e locally ("port in use", "database refused", "works locally, fails in CI").
- Adding a command that deletes or rewrites data (migrate, seed, prune, admin).
- Changing the CI workflow.

Related: `testing` (what tests to write and how), `conventions` (writing checks, shared-machine etiquette, completion reports), `ops` (the production image, deployment, production config refusals, the admin CLI's production side), `security` (what must never leak), `structure` (where `scripts/` and `cmd/` live).

## References

| File | Read when |
| --- | --- |
| [references/command-surface.md](references/command-surface.md) | Adding or changing a target or script; writing the dev runner; choosing a task runner for a non-Make stack |
| [references/env-files.md](references/env-files.md) | Adding a config variable, writing `.env.example`/`.env.testing`, the test-env wrapper, `app-key`, ignore files |
| [references/test-databases-and-parallel-e2e.md](references/test-databases-and-parallel-e2e.md) | Setting up test databases, the e2e preflight, port locks, the e2e server script, or debugging a clash |
| [references/operator-commands.md](references/operator-commands.md) | Writing or changing migrate, seed, prune, admin or any command that changes data |
| [references/ci.md](references/ci.md) | Writing or changing the CI workflow, caching, service containers, artifacts |

## 1. One command surface

- **ALWAYS expose every routine action as a named target** in one task runner at the repo root, and use the **same targets on a laptop, in docs, in AGENTS.md and in CI**. A step that only exists as a paragraph in a README, or only in the CI YAML, drifts until it is wrong.
- **The canonical set** (rename to taste, but keep the set):

  | Group | Targets |
  | --- | --- |
  | Setup | `help` (the default), `install`, `app-key` |
  | Services | `db` (start local services, create the test database) |
  | Schema and data | `migrate`, `migrate-status`, `migrate-rollback`, `migrate-fresh`, `migration name=…`, `seed`, `prune`, `admin cmd=…` |
  | Run | `dev` (everything, one terminal), `api`/`web` (one process each), `build`, `serve` (the built artefact, production-like) |
  | Verify | `test` (unit + integration), `check` (static gate), `e2e`/`test-e2e`, `serve-test` (the e2e server, for checking by hand) |
  | Containers | `docker-dev` (whole stack in containers), `docker-app` (rebuild only the app), `docker-down` |

- **Targets are thin.** One line calling a script (`scripts/<name>.sh`) or the app's own CLI. Logic lives in scripts that can be run, read and tested without the task runner.
- **Parameters are `key=value`** (`make seed only=demo count=100`, `make migration name=add_tasks`, `make admin cmd=grant email=… CONFIRM=1`). A missing required parameter prints the usage line and exits non-zero. NEVER silently fall back to a guess.
- **`help` is the default target** and lists every target, so a bare `make` teaches the surface.
- **Fix-it messages, not stack traces:** every precondition failure says the command that fixes it: `No .env. Create it with: cp .env.example .env`, `Dependencies are missing. Run: make install`, `The test database isn't running at 127.0.0.1:5432: start it with make db`.
- **Guard scripts ship with a self-test** run by `check` (a script that scans the build for leaks gets a test that plants each forbidden marker and expects failure). A guard that has never failed may guard nothing.
- **Common mistake:** a task runner that loads `.env` for every target (Make's `include .env` + `export`) also overrides what you type in front of it. `DATABASE_URL=…other… make migrate` still migrates the `.env` database. To act on another database, run the script or binary directly. Document this beside the target.

Details, a Makefile skeleton and equivalents for other runners: [references/command-surface.md](references/command-surface.md).

## 2. `make dev`: everything in one terminal

- **One command starts every dev process** (API, frontend dev server, any secondary build) in one terminal, with **each line prefixed** by its process (`[build]`, `[api]`, `[web]`).
- **MUST check preconditions first:** `.env` exists, dependencies are installed, and **every port it will use is free**, before starting anything. A taken port refuses the whole start and names what probably holds it ("stop make api, make web or make serve first"). Never start half the stack.
- **MUST build what the dev processes depend on first** (e.g. a frontend build the backend embeds), so nothing serves a stale build. **A failed build starts nothing**; the error stays on screen under `[build]`.
- **One dies, all stop:** if any process exits (the API crashed because the database is down), stop the rest and say so. Ctrl-C stops everything, including the output prefixers. A dev stack with a dead API and a live frontend wastes an hour on "why does every request fail".
- **Dev servers MUST use a fixed port and fail if it's taken** (`strictPort` in Vite; equivalent flags elsewhere). A server that silently moves to the next port breaks the proxy, the configured origin and anyone's bookmarks, and may now be talking to someone else's backend.
- **One canonical dev origin** (`http://localhost:3000`) that matches the configured app origin. Opening `127.0.0.1:3000` instead fails the server's exact-Origin check on mutations.
- Also provide per-process targets (`make api`, `make web`) for people who want separate terminals or to restart only the API.
- Say in the docs what reloads (frontend on save) and what needs a restart (backend code, `.env` changes).
- **`make serve`** runs the built artefact as production does, on the same port. Use it to check what the dev server can't show: CSP and security headers, server-side page gating, 404s for unknown URLs, the embedded build.

## 3. Local services with Docker Compose

- **Every service port binds to `127.0.0.1`** (`"127.0.0.1:5432:5432"`), never the bare `"5432:5432"`, which listens on every interface. The database has a committed password and the mail catcher's inbox holds working password-reset links.
- **Every service has a healthcheck**, `make db` runs `docker compose up -d --wait`, and app containers `depends_on: { condition: service_healthy }`. "Started" is not "ready": a test run that races Postgres startup fails at random.
- **Set an explicit project `name:`** in `compose.yaml`. Without it the project name comes from the folder name, so renaming or re-cloning the folder silently creates a new, empty volume and "loses" the dev data.
- **Pin image tags** (`postgres:17`, `axllent/mailpit:v1.31.0`), never `latest`: CI and every laptop run the same version.
- **One shared database container per machine, reused by everything:** dev, integration tests and every e2e run use separate *databases* inside it, never separate containers or a second Postgres install. Check `docker compose ps` before starting anything.
- **Create the test database idempotently in `make db`** (`SELECT 1 FROM pg_database WHERE datname='app_test'` or create it). NEVER rely on `docker-entrypoint-initdb.d` alone: it runs only on a brand-new volume, so existing developers never get the new database.
- **A local mail catcher** (e.g. Mailpit) receives all dev and test email over real SMTP. Nothing local ever sends to a real inbox.
- **No migrate-on-start, even locally.** Migrations run through `make migrate` (or `docker-migrate`). A server that migrates on boot hides migration failures in startup logs and, copied to production, runs schema changes from every replica at once.
- **Containerised dev is optional, not the default loop:** `docker-dev` (start services, build the image, migrate, seed, start the app) and `docker-app` (rebuild and restart only the app). Note in the docs that a running app container holds the app port and serves its last build, not your working tree.

The production image, its runtime user and deployment are `ops`'s.

## 4. Env files

- **`.env.example` is committed and documents every variable**, in grouped, commented blocks: what it does, its default, what production refuses, and a link to the doc section. Developers start with `cp .env.example .env`.
- **Its secret values are obviously local-only, and production refuses those exact values** (`ops`). A copied example key in production is the same as no key.
- **`.env` is never committed.** `.gitignore`: `.env` and `.env.*`, then `!.env.example` and `!.env.testing`. `.dockerignore` excludes `.env*` and local scratch (`**/tmp`, which may hold a dev mailbox file with live reset links).
- **`make app-key` writes a fresh key into a `.env` that has none, and NEVER replaces an existing one.** Replacing the master key makes everything sealed with it unreadable: every account with two-factor authentication is locked out of the dev database.
- **`.env.testing` is committed** and holds **only local, throwaway values**: the local test database URL, the e2e base port, `RATE_LIMITS=off`, the mail catcher's address. **No per-run secrets**: the test runner generates the app key and similar per run.
- **A test asserts that nothing named like a secret has a value** in `.env.testing`: any key containing `PASSWORD`, `SECRET` or `TOKEN`, or ending in `KEY`, must be empty. The same test checks the file parses and its database URL passes the local-test-database guard (§6). This stops "just for now" real credentials landing in git.
- **Plain `KEY=value` lines only** in `.env.testing`: no quotes, no spaces around `=`, no `export`, no `$` expansion, no repeated keys. Bash, Make, your backend and Node then all read the same values. A parser test refuses anything else.

Details and templates: [references/env-files.md](references/env-files.md).

## 5. Tests never see `.env`

- **Every test target runs in an emptied environment**, populated from `.env.testing` plus a short **allow-list** from the caller: what finds the tools (`PATH`, `HOME`, `USER`, `TMPDIR`, `TERM`, the toolchain's own variables such as `GO*`/`CC`), `CI`, the browser path, `FAKER_SEED`, the test runner's own settings (`PLAYWRIGHT_*`), and the run's own non-empty `E2E_*` values. Implementation: `env -i` in a small wrapper script (`scripts/test-env.sh <command>`).
- **Why:** a task runner that exports `.env` hands the developer's database URL, mail settings and proxy headers to every test. If `.env` ever points at a shared or staging database, the tests run against it. An allow-list fails closed; a deny-list misses the next variable someone adds.
- **Direct runs are covered too:** the backend test harness reads the database URL from `.env.testing` itself (never from the process environment), and the e2e config loads `.env.testing` over whatever is set. A bare `npx playwright test` or an IDE "run test" button is as safe as `make test`.
- **Only the e2e port may come from the caller.** Everything else is forced, so tests behave the same on every machine and in CI.
- NEVER add a `TEST_DATABASE_URL`, an `APP_ENV=testing` mode or a `.env.testing.local`. Tests use the same variable names the server uses, the server under test behaves like development, and personal overrides belong in `.env`.

## 6. Test databases

| Database | Used by | Created by | Rebuilt |
| --- | --- | --- | --- |
| `app` | development | `make db` (the image's `POSTGRES_DB`) | only by you, `make migrate-fresh CONFIRM=1` |
| `app_test` | backend integration tests, **each in its own throwaway schema** | `make db`, idempotently | never; each test drops only its own schema |
| `app_test_<port>` | one e2e run on that port | the e2e server script, on first use | at every e2e server start |

- **MUST refuse any non-local host:** only `127.0.0.1`, `::1`, `localhost` and the compose service name (for running inside a container). `E2E_PORT` changes a port and a name suffix, never the host.
- **MUST refuse any database name that isn't a test database:** `*_test` or `*_test_<digits>` for the integration harness; **only** `^[a-z0-9_]+_test_[0-9]+$` for anything that drops and rebuilds (the e2e server). The guard runs in the harness, in the e2e server script, and in the `.env.testing` test. An edited file can then never aim `DROP SCHEMA` or `migrate fresh` at real data.
- **A missing or unreachable database fails the run** with the fix ("start it with `make db`"). NEVER skip: a skipped integration suite is green and tests nothing.
- Old `app_test_<port>` databases stay until that port is used again; they hold only throwaway data.

The per-test schema harness itself is `testing`'s (`references/backend-integration.md`). Details: [references/test-databases-and-parallel-e2e.md](references/test-databases-and-parallel-e2e.md).

## 7. Parallel e2e runs

Agents and people start e2e while another run is going. Each run gets **its own port, its own server and its own database from one variable**, and learns within a second, before anything is built, if it can't have them.

- **A preflight runs first:** it checks the database and mail catcher answer and the needed tools exist (each with an install hint), then **reserves the port with an exclusive lock** (`flock -n` on `/tmp/<app>-e2e-<port>.lock`), held until the run ends or crashes, and checks nothing is listening.
- **`E2E_PORT=auto`** takes the first port in a range (e.g. 3101–3199) whose lock it gets and where nothing listens. Taking the lock *is* the reservation, so two runs starting together get different ports. Agents always use `auto`.
- **A clash stops at once** with a free port worked out: `Port 3100 is in use … make test-e2e E2E_PORT=3104, or E2E_PORT=auto`.
- **Why a lock and not only a port check:** the build takes minutes and the server rebuilds its database before binding the port. Two runs both pass a port check, and the second's rebuild wipes the first's database mid-run.
- **The lock path is machine-wide** (`/tmp`, not `$TMPDIR`, which differs per session) so it works across checkouts and worktrees. Make it world-writable so another user can take a released port.
- **One database per port** (`app_test_<port>`), rebuilt from scratch at every server start. **The e2e server refuses to rebuild a database whose name doesn't match `*_test_<port>` or whose host isn't local.**
- **The run prints what it took** (`E2E run: app http://localhost:3104 · database app_test_3104`). Use those values; never guess.
- **Shared on purpose:** the database container (a database per run) and the mail catcher (runs find their mail by unique recipients).
- **Not supported: two runs in one checkout.** They share build output; one build replaces the other's mid-run. Give each agent its own worktree.

Details (lock hand-off to the child, second ports, reuse mode): [references/test-databases-and-parallel-e2e.md](references/test-databases-and-parallel-e2e.md).

## 8. The static gate: `make check`

- **`check` runs every static check and fails on any finding:** formatter in check mode (lists and fails, never rewrites), linter with **zero warnings allowed**, type check, the docs link and anchor check, convention checks that aren't ordinary tests, and the self-tests of guard scripts.
- **A check that rewrites files in `check` is wrong:** CI would pass on unformatted code and a laptop run would leave surprise diffs. Formatting is a separate, explicit command.
- Writing the checks themselves is `conventions`'s (`references/writing-a-check.md`); this skill makes sure `check` runs them and CI fails on them.

## 9. Operator and destructive commands

- **Every command that changes or deletes data previews unless confirmed** (`-confirm` flag, `CONFIRM=1` through the task runner). A bare run only reports what it would do, with counts.
- **Commands that make no sense in production refuse there** (`migrate fresh`, `seed`), checked from config before connecting. Commands meant for production (`prune`, `admin`) are allowed but still preview by default.
- **Strict input:** typed flags with ranges, a usage text, non-zero exit on any error (unknown target, bad value, database error), a per-command timeout, Ctrl-C handled.
- **Allowed without asking:** rebuilding *your local* dev database while migrations are squashed in development, and e2e's own throwaway databases. Everything else destructive needs the user's explicit authorisation (`conventions`).

Details: [references/operator-commands.md](references/operator-commands.md). The production side (audit events, reasons, roles) is `ops`'s.

## 10. Dev-server exposure

- **A dev server listening on `0.0.0.0` (every interface) exposes the app to the whole network:** unminified source with source maps, admin pages' code, the dev proxy to your API, and seeded demo accounts with known passwords. On shared Wi-Fi, that is everyone in the room.
- **Default every dev listener to `127.0.0.1`** (API, frontend dev server, secondary dev servers, compose ports). When LAN access is genuinely needed (testing on a phone), make it an explicit opt-in (`DEV_HOST=0.0.0.0 make dev`) that prints a warning, or document the exposure next to the setting.
- **Common mistake:** setting `host: true`/`0.0.0.0` "because it didn't work in a container" and committing it. Bind to all interfaces only inside the container and publish the port on `127.0.0.1` on the host.
- The mail catcher and database are never exposed, for the same reason (§3).

## 11. CI

- **CI runs the laptop's commands:** `make install`, `make test check build`, install the browser, `make test-e2e`. No test settings in the CI YAML: they come from `.env.testing`, exactly as on a laptop. If CI needs a step a laptop doesn't, that is a missing target.
- **Service containers mirror `compose.yaml` and `.env.testing`:** same images and tags, same credentials, the test database name, and a healthcheck on each.
- **Toolchains come from the repo** (`go-version-file`, `.nvmrc`/`engines`, `.tool-versions`) and dependency caches are keyed on the lockfiles.
- **Fail on everything:** lint, types, format, docs links, convention checks, guard self-tests, unit, integration, build and e2e. No `continue-on-error`, no retries to hide flakiness, no skipped suites.
- **Upload artifacts on failure:** e2e traces, screenshots and videos (`test-results/`), the report, and the e2e server's log, so a red run can be diagnosed without re-running it.
- Start with **one job** that mirrors the laptop; split into parallel jobs (check, test, e2e) only when the wall-clock time matters, each still calling make targets.

Details and a workflow skeleton: [references/ci.md](references/ci.md).

## 12. Shared machines

Other people and agents may share the machine, the database container and the port range. Follow `conventions` (§11 and `references/agent-etiquette.md`): never stop, restart or reuse a process, server, container or run you didn't start; reuse the existing service containers; check what's running before a heavy run; use `E2E_PORT=auto` and the URLs it reports. Note that sandboxed tools may hide other users' processes: the dev runner's own port check is the reliable one.

## Pre-merge checklist

- [ ] Every new routine action is a target; docs, AGENTS.md and CI call the target, not raw commands.
- [ ] New targets are thin, take `key=value` params, print usage on a missing required param, and appear in `help`.
- [ ] New config variable: documented in `.env.example` (purpose, default, production behaviour, doc link); added to `.env.testing` only if tests need a non-default, and never with a secret value.
- [ ] `.gitignore`/`.dockerignore` still exclude `.env*` (except the example and testing files) and local scratch.
- [ ] New dev process: added to `make dev` with a prefix, a port check before start, strict port, `127.0.0.1` by default.
- [ ] New service: pinned image, port on `127.0.0.1`, healthcheck, mirrored as a CI service container.
- [ ] Test targets run through the emptied-environment wrapper; the allow-list was not widened without a reason.
- [ ] Anything that drops or rebuilds a database checks for a local host and a `*_test` / `*_test_<port>` name.
- [ ] New destructive command: previews without confirmation, refuses production where meaningless, strict flags, non-zero exits.
- [ ] CI runs the same targets, fails on every check, and uploads traces and screenshots on failure.
