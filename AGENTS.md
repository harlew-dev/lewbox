# AGENTS.md

Context for coding agents working in this repo. Read it before changing anything.

## What this is

**Lewbox** is one Go binary, `lewbox`, that is both a server (the **orchestrator**, `lewbox serve`, HTTP API under `/v0`) and a CLI. It installs [AerolVM](https://github.com/aerol-ai/microvm) (a sandbox platform: `sandboxd`) on Linux machines, starts and joins AerolVM clusters, registers them with the orchestrator, and checks them. It is a self-hosted E2B: one operator (the instance admins) resells their clusters' compute to customer **workspaces** (code: `organization`), each exactly one AerolVM team with a plan per region pushed as that team's quota. Customers' apps talk to the clusters directly; Lewbox is never in the sandbox request path. **`plans/` is the current spec** (start with `plans/README.md` and `plans/DECISIONS.md`); `MVP.md` is the older, larger design and is partly superseded. `REVIEW.md` explains why Lewbox builds on stock AerolVM clusters.

**Built:** accounts, sessions and sign-up; workspaces with members and API keys; plans and per-region subscriptions; per-region sandbox keys (AerolVM runner tokens); the sync loop (sandbox mirror, usage metering, quota pushes, suspension); snapshots; 30-day health, uptime and node metrics; the customer portal (`/`) and operator console (`/admin`); clusters, nodes, `install`, `node add`, `update`, `firewall enable`; text/JSON output. **Deliberately not built** (see `plans/DECISIONS.md`): billing (D24), per-member limits (D29), webhooks, email.

## Layout

| Path | What |
|---|---|
| `cmd/lewbox/` | cobra CLI. `main.go` (root, app, backend choice, confirmations, exit codes), `authcmds.go`, `serve.go`, `install.go`, `nodeadd.go`, `update.go`, `machine.go` (host deps + helpers), `render.go` (text output) |
| `internal/service/` | **All business logic.** Methods `(ctx, req) (resp, error)`. Users/sessions/sign-up, workspaces (`organizations.go`), plans, subscriptions, sandbox keys, `sync.go` (mirror, metering, quotas), sandboxes, usage, snapshots, monitoring, clusters, nodes |
| `internal/httpapi/` | Exposes service over HTTP. `ErrorFrom` maps error codes to statuses; `Encode` writes one JSON line |
| `internal/backend/` | `Backend` interface: `Local` (service in-process, local principal) and remote (`internal/client`) |
| `internal/client/` | HTTP client implementing Backend; turns API error bodies back into `*service.Error` |
| `internal/cliconfig/` | Contexts and credentials (`~/.config/lewbox/`), backend selection precedence |
| `internal/db/` | SQLite (modernc), goose migrations in `migrations/`, sqlc queries in `queries/` → generated `*.sql.go` |
| `internal/aerolvm/` | AerolVM admin API client. `aerolvmtest/` is an in-memory fake cluster for tests. `testdata/` holds **real** v0.7.0 responses (see its README) |
| `internal/machine/` | Node-side work: `Runner` (sudo exec + secret masking), installer source/verification, checks, Lewbox settings, node state, releases. `machinetest/` is a fake runner |
| `internal/secrets/` | AES-256-GCM keyring for cluster admin tokens |
| `internal/webui/` | Top-level HTTP handler: the API, the built console, and one origin for both |
| `internal/docs/` | Parses the embedded markdown into nav and pages for `/v0/docs` |
| `docs/embed.go` | Embeds `docs/` into the binary |
| `web/` | React web console (Vite, TypeScript). `web/dist` is embedded by the Go binary; see `web/README.md` |
| `Makefile` | One entry point: build, test, run, Docker and the UI library |
| `deploy/dev-cluster/` | Docker test bed (systemd + Docker-in-Docker nodes) and `e2e.sh` |
| `deploy/docker/` | Smoke test for the orchestrator image (repo-root `Dockerfile`, `compose.yaml`) |
| `docs/` | User docs (`docs/README.md` is the index). `docs/design/` holds design notes |

## Commands

```bash
# The Makefile is the entry point. `make` builds web/dist and then bin/lewbox.
make                        # build the console and the binary (default)
make build-go               # Go only, after the console is built
make test                   # gofmt, go vet, go test, web typecheck + tests
make fmt                    # gofmt -w .
make docker                 # the runtime image, lewbox:local, via Compose
make docker-test            # the Go checks and web tests inside the image
make e2e                    # build and smoke-test the container

# The same checks raw. `make test` runs all four plus the web checks.
gofmt -l .                  # must print nothing
go vet ./...
go test ./...               # unit + CLI/HTTP parity tests, ~20s, no network or Docker
go build ./...
go generate ./internal/db   # after editing migrations/ or queries/ (runs sqlc via go run)

# End to end against real AerolVM in Docker (~6 min, needs Docker and privileged containers)
cd deploy/dev-cluster && LEWBOX_DEV_DNS="1.1.1.1 8.8.8.8" ./e2e.sh
```

The four Go checks (or `make test`, which adds the web checks) must pass before you call a change done. Run `e2e.sh` for anything touching `internal/machine`, `cmd/lewbox/{install,nodeadd,update,machine}.go`, cluster checks or node sync. The unit tests use fakes and **have missed real bugs**: a wrong `sudo` argv, and running an unverified file.

## Rules of the code

- **Service first, two entry points.** Logic lives in `internal/service`. HTTP and the CLI both call the same method. CLI `--json` output must be **byte-identical** to the API body; `cmd/lewbox/parity_test.go` and `internal/backend/backend_test.go` enforce it. A new operation means: a service method, a route, a Backend method on both implementations (local and client), a CLI command, and parity tests. The docs routes (`/v0/docs…`), `GET /v0/instance`, `POST /v0/auth/signup` and the static web console are HTTP-only exceptions: they are public content or browser sign-up, so they have no Backend method or CLI command.
- **Errors** that users may see are `*service.Error{Code, Message, Details}` built with the helpers in `errors.go` (`invalid`, `unauthorized`, `forbidden`, `notFound`, `conflict`, `precondition`, `unavailable`). Anything else is reported as `internal`, and its text is hidden.
- **Auth:** every service method starts with `requireUser(ctx)` or `requireAdmin(ctx)`, except the public `Health`, `Instance`, `Signup`, `Login` and `Authenticate`. Local mode uses `service.LocalPrincipal()` (instance admin).
- **JSON:** snake_case; lists are `{"data": [...]}` and never `null`; times are UTC, truncated to milliseconds. IDs are UUIDv7 (`import "uuid"`, Go 1.27 std). Names (node, cluster slug, email) are looked up like IDs and must never parse as a UUID.
- **DB:** never hand-edit generated `internal/db/*.sql.go`. Add a new numbered migration; don't edit applied ones. Invariants that must hold under concurrency go **in the SQL statement** (see the last-admin guard in `queries/users.sql`).
- **Output:** results go to stdout; progress (checks, `→ sudo …` lines, installer output) goes to stderr. Text rendering is in `render.go`; JSON is the default when stdout isn't a terminal.
- **Exit codes:** 0 ok (including a declined confirmation), 1 failure, 2 usage (`usagef`), 130 interrupted. Destructive commands call `a.confirm(...)` and need `--yes` outside a terminal.
- Go 1.27: use `errors.AsType`, std `uuid`, `crypto/pbkdf2`. Prefer the standard library; ask before adding dependencies.

## Rules for code that touches a machine

These came from running against real AerolVM. Don't regress them.

- The CLI **refuses to run as root** (`LEWBOX_ALLOW_ROOT=1` overrides). Each privileged step is **one `sudo` command**, printed (masked) before it runs; `sudo -n` when stdin isn't a terminal.
- **Never pipe a download into a shell.** Verify `install.sh` (pinned SHA-256 for v0.7.0 in `internal/machine/source.go`; otherwise the release's `checksums.txt`), write the verified bytes to the cache, and run **that copy**. A mirror (`--from`) is still held to the pinned hash.
- **Mask secrets everywhere.** The installer prints `PAT token: …`; `machine.MaskLine` rewrites it. Join tokens and DNS tokens go in `Cmd.Secrets`. The PAT is only read (`machine.LocalPAT`, via `Output`) and never printed or stored.
- **Lewbox's AerolVM settings go in Lewbox's own files**: `/etc/sandboxd/lewbox.env` and the drop-in `/etc/systemd/system/sandboxd.service.d/lewbox.conf`. Never edit the installer's `sandboxd.env`. Identity must be on **every** node, or that node answers 401.
- **Version strings are untrusted** (one comes from a cluster's `/health`). Validate them with the release-tag regex before using them in a path, URL or shell line.
- **Fail closed:** if you can't confirm something that guards a dangerous step (for example the cluster size before `update --aerolvm`), refuse.

## AerolVM facts (verified on v0.7.0; see `docs/design/aerolvm-integration.md`)

- Install with `--no-start`, then `install.sh … init` (new cluster) or `install.sh … join <url> --token --ca-cert-hash --node-id --role` on the same machine. Local mode (`--local`) can't join.
- Re-running the installer is safe, but **always pass `--version`**: without it, a re-run upgrades to the newest release.
- Right after a start, `/health` answers before Raft has a leader. Token writes then return **503** for 1–3 s: retry them. A 503 saying `cluster: not enabled on this node` means single-node mode: **don't** retry it.
- The PAT (`SB_PAT_TOKEN` in root-only `sandboxd.env`) only works over `127.0.0.1` with `--pat-scope host`. Lewbox uses it once to mint `lewbox-<slug>` (`--role admin --team '*' --expires-in 36500d`).
- A `mixed` node needs a `server` join token. `POST /v1/cluster/nodes/{id}/drain` returns 204 even for unknown nodes, so check membership first. Remove a member with `DELETE /v1/cluster/nodes/{id}[?force=true]`.
- New tokens take a few seconds to work on other nodes.

## Testing notes

- `aerolvmtest.New(t)` fakes a cluster: `SetMembers`, `SetClusterMode(false)`, `FailNext(path, status, body, n)`, `Stop()`. Give fake members distinct `APIURL`s. Only the node behind the cluster's admin URL may share it (removal refuses that node).
- CLI machine tests use a harness with a fake runner (`cmd/lewbox/machine_test.go`). Set `h.pat` to the fake's `AdminToken` when a test reaches the local AerolVM. `writeMirror` pins the fake installer's hash through `installerPin`.
- Tests must never write to the real `~/.cache` or `~/.config`: set `XDG_*` to temp dirs, or `Source.CacheDir`.
- Dev cluster: on this host's network, `archive.ubuntu.com` fails to resolve without `LEWBOX_DEV_DNS`. Clean up with `./dev.sh down` or `docker rm -v <your container>`. **Never** run `docker volume prune` or other host-wide Docker cleanup.

## Docs

- User docs live in `docs/`, one page per task, with front matter (`title`, `description`) and relative links. They will be published as a website. When behavior, flags or messages change, update the matching page and `docs/reference/cli.md` in the same change. `docs/README.md` is the index; add new pages there.
- The web console has its own contributor guide in `web/README.md`: how it is built and served, the auth and CSP rules, and how to add a page.
- Design notes and verified findings go in `docs/design/`. Don't edit `MVP.md` or `REVIEW.md` unless asked.

## Working agreements

- Don't commit, push or create branches unless asked.
- Keep changes scoped. Update tests with behavior, and never weaken a test to make it pass.
- If gopls reports errors that `go build`/`go vet` don't, its snapshot is stale; trust the build.
