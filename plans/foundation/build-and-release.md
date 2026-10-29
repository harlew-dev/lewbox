# Build and release

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

How Lewbox is built, checked and released: CI, the build targets and what's embedded in them, release artifacts and container images, signing, versioning, how an instance and its nodes are upgraded, and how Lewbox's releases are tied to our AerolVM fork's.

## Today

- **Gateway** (`lewbox-gateway-legacy`):
  - `Makefile`: `run`, `build` (gateway plus `portd` and `agentd` for linux amd64 and arm64, `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`), `test`, `test-race`, `vet`, `fmt`, `check`.
  - `build.sh`: the same without Docker, for Laravel Cloud's build step, putting helpers in `bin/portd` and `bin/agentd`.
  - `Dockerfile`: `golang:1.27` build stage, then `gcr.io/distroless/static-debian12:nonroot` with the helpers under `/usr/local/lib/lewbox-gateway/{portd,agentd}`, port 8080.
  - CI (`.github/workflows/ci.yml`): gofmt, vet, `go test -race`. No releases, no versions: deployed from `main`.
- **App** (`lewbox-legacy`): no Dockerfile; Laravel Cloud deploys from `main`. CI (`.github/workflows/tests.yml`): `composer setup` then `composer ci:check`. Dependabot.
- **Nodes**: `install.sh` served by the app at `/install.sh`, pinning the cluster's AerolVM release (`AEROLVM_SUPPORTED_VERSION`, v0.6.4). Pinned there because upstream v0.6.5 shipped without release assets. No upgrade path: installed nodes keep the scripts and AerolVM they were installed with; fixes needed manual scripts (`docs/examples/fix-gvisor-*.sh`).
- **SDK**: the PHP SDK lives in the app repository and is copied into Lewagent by a Composer hook (`SyncLewboxSdkCommand`).

## Goals

- Every merge to `main` produces a complete, installable snapshot build.
- Tagged releases with signed, reproducible artifacts for every platform, a multi-arch container image, and checksums.
- One version number for the binary and everything embedded in it.
- Upgrades without downtime for multi-replica instances, and without manual steps for nodes.
- The AerolVM fork released on its own cadence, with Lewbox declaring which fork versions it supports.

## Design

### CI

GitHub Actions, on every pull request and push to `main`:

| Job | Does |
|---|---|
| `go-lint` | `gofmt -l`, `go vet`, `staticcheck` (or `golangci-lint` with a short, agreed list of linters), `govulncheck` |
| `go-generate` | `sqlc diff`, `sqlc vet` (core and each plugin); OpenAPI-generated code is current; `go generate ./...` leaves no diff, including the plugin files generated from `plugins/config.json` ([plugins/building-and-enabling](../plugins/building-and-enabling.md#the-generator)) |
| `go-test` | `go test -race -shuffle=on ./...` against a `postgres:17` service; short fuzz runs; coverage report. Includes each provider plugin's contract suite against its fakes ([testing](testing.md#provider-contract-suites)) |
| `core-only` | The `noplugins` build: compiles with no plugins, migrates, passes the sandbox lifecycle suite, so the core never depends on a plugin |
| `plugin-imports` | `depguard`: no `internal/…` package imports `plugins/…`; no plugin imports `internal/…`; plugins' `agentd` and `cli` packages pull in no server code |
| `migrations` | Migrations up from empty; `lewbox migrate verify`; each new migration against the previous version with seed data |
| `openapi` | Lint the spec (Spectral or Redocly, with our rules); breaking-change check against the last release's spec (`oasdiff`), failing on breaks without a `breaking-change` label |
| `web` | `pnpm install --frozen-lockfile`, type-check, lint, Vitest, `vite build` |
| `e2e` | Build the full binary; run Playwright against `lewbox serve` with fakes; run the SDK contract suites |
| `build` | Snapshot build of every artifact (below) for every platform; uploaded as workflow artifacts on `main` |
| `docs` | Generated docs (settings, metrics, errors) are current |

Nightly: real-cluster suite ([testing](testing.md#real-clusters)), long fuzzing, `govulncheck` against the latest database, dependency updates (Renovate or Dependabot, grouped weekly).

### Build targets

Built in this order, because later ones embed earlier ones. Step 0 is committed and checked by CI, so a plain `go build` works from a checkout:

| # | Target | Command | Output | Platforms |
|---|---|---|---|---|
| 1 | Dashboard | `pnpm --dir web build` | `web/dist/` | (static files) |
| 0 | Plugin generation | `go generate ./plugins` | `plugins_gen.go` (per build tag), `web/src/plugins.gen.ts`, merged `api/openapi.yaml` | (sources) |
| 1 | Dashboard | `pnpm --dir web build` | `web/dist/` (with each plugin's `web/` as lazy chunks) | (static files) |
| 2 | Sandbox helper | `go build -tags sandbox` | `lewbox-sandbox` (agentd with the agent plugins' drivers, portd) | linux/amd64, linux/arm64 |
| 3 | Full binary | `go build` | `lewbox` (everything; the plugins in `config.json`; embeds 1 and 2) | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 |
| 4 | CLI | `go build -tags cli` | `lewbox-cli` (client commands, with plugins' commands) | linux, darwin, windows × amd64, arm64 |

All with `CGO_ENABLED=0 -trimpath -ldflags="-s -w -X …/version.Version=… -X …/version.Commit=… -X …/version.Date=…"`, and `-buildvcs`. Go's toolchain is pinned in `go.mod` (`toolchain go1.27.x`).

`make` targets mirror the jobs: `make web`, `make sandbox`, `make build`, `make cli`, `make test`, `make check`, `make dev` (Postgres in Docker, `lewbox serve --dev` with `LEWBOX_DEV_WEB_URL` pointing at `vite dev`), `make dev-fake` (with `LEWBOX_DEV_FAKES`).

### Embedding

`internal/embedded` holds what the full binary carries:

| Asset | From | Embedded as | Used by |
|---|---|---|---|
| Dashboard | `web/dist/` | `embed.FS`, gzip and brotli precompressed variants served by `Accept-Encoding`; hashed asset names cached for a year, `index.html` never | [dashboard](../dashboard.md) |
| Sandbox helpers | `dist/sandbox/lewbox-sandbox-linux-{amd64,arm64}` + `manifest.json` | zstd-compressed bytes with SHA-256 in the manifest, checked at start | [binary-and-commands](binary-and-commands.md#the-in-sandbox-helpers) |
| Migrations | `internal/db/migrations/*.sql`, and each plugin's `plugins/<name>/migrations/*.sql` | `embed.FS` (the plugin's, registered in its `Register`) | [database](database.md#migrations), [database](database.md#plugin-migrations) |
| Provided templates | `templates/provided/*/` (Dockerfile, ports, defaults) | `embed.FS` | [templates/provided-templates](../templates/provided-templates.md) |
| Node bootstrap script | `internal/nodeagent/bootstrap.sh` | string | The one-line install command fetches it ([clusters/installation](../clusters/installation.md)) |
| AerolVM fork manifest | `aerolvm.json`: supported fork versions, default for new clusters, release asset URLs and checksums per platform | JSON | [clusters/aerolvm-fork](../clusters/aerolvm-fork.md) |
| OpenAPI document | `api/openapi.yaml` | bytes, served at `/v0/openapi.yaml` | [api/openapi-and-codegen](../api/openapi-and-codegen.md) |
| Docs (optional) | `docs/` | For `/docs` on self-hosted instances, if we serve docs from the binary | [sdks-and-cli](../sdks-and-cli.md) |

The full binary's build fails if any embedded asset is missing (no silent "private ports are off").

### Releases

Tagging `vX.Y.Z` on `main` runs the release workflow, built with **GoReleaser** (or an equivalent script; GoReleaser handles the matrix, archives, checksums, signing, SBOMs, Homebrew and images):

- **Archives**: `lewbox_X.Y.Z_{os}_{arch}.tar.gz` (full), `lewbox-cli_X.Y.Z_{os}_{arch}.tar.gz` / `.zip` (CLI), each with `LICENSE` and `README`.
- **Checksums**: `checksums.txt` (SHA-256).
- **Signatures**: cosign keyless signatures of the checksums file and images (Sigstore, GitHub OIDC) ([signing](#signing)).
- **SBOMs**: SPDX via Syft, per archive and image.
- **Container images**: `ghcr.io/harlew-dev/lewbox:X.Y.Z`, `:X.Y`, `:latest`, multi-arch (amd64, arm64). Base `gcr.io/distroless/static-debian12:nonroot`; entrypoint `lewbox`, default command `serve`; port 8080; no helper directories (they're embedded).
- **Packages**: Homebrew tap for the CLI; npm `@lewbox/cli` wrapping the CLI binaries ([sdks-and-cli](../sdks-and-cli.md)); `.deb`/`.rpm` for nodes later, if wanted.
- **Release notes**: generated from Conventional Commits and edited by hand, with an *Upgrading* section for anything an operator must do.
- **The node endpoint**: each control plane serves its own version's binary to nodes (`GET /node/v0/binary?os=linux&arch=amd64`, with the checksum), so nodes install exactly the control plane's version without reaching GitHub ([clusters/node-agent](../clusters/node-agent.md)).

### Signing

- Release checksums and images signed with cosign keyless (Sigstore); provenance attestations (SLSA level 3 via GitHub's `attest-build-provenance`).
- Install docs show `cosign verify-blob` for checksums and `cosign verify` for images.
- The node agent verifies binaries it downloads against the checksum the control plane gives it over its authenticated, TLS connection; the control plane's own binary was verified when the operator installed it.

### Versioning

- **SemVer**. `v0.x.y` during alpha and beta: minor versions may break things, with notes. `v1.0.0` when the API is stable.
- **One version** for the binary, the embedded helpers, the node agent and the dashboard. Helpers additionally carry a content hash used for installs ([binary-and-commands](binary-and-commands.md#version-handshake)).
- **The API's version** (`/v0`) is separate from the release version. Within `/v0`, changes are additive ([api](../api.md)). The OpenAPI document's `info.version` is the release version.
- **SDKs** are versioned on their own, each stating the API features it supports.
- `lewbox version --json` prints version, commit, date, Go version, helper hashes, supported AerolVM fork versions, and minimum node agent version.

### Upgrades

**The control plane**, with several replicas:

1. Migrations for release N+1 run first (by the first N+1 replica to start, under the advisory lock). They must be expand-only: release N keeps working against N+1's schema ([database](database.md#migrations)).
2. Replicas roll one at a time. N and N+1 serve side by side for a few minutes. Jobs inserted by either can be run by either: job arguments are versioned (new fields optional, old fields kept for a release).
3. Contract migrations (dropping what N used) ship in N+2.
4. A replica at N that finds the schema *ahead* (N+1's migrations applied) keeps running; one that finds it *behind* refuses to start, unless `--migrate`.

For a single-replica homelab: stop, replace the binary (or pull the image), start; migrations run on start.

**Skipping versions**: allowed within a minor series; across minors, the release notes say if an intermediate version is required (it is if a contract migration needs its expand step first). `lewbox migrate status` says so before anything is changed.

**Nodes** (node agent and AerolVM fork):

- The control plane says which node agent version it wants (its own) and which fork version each cluster should run (pinned per cluster). Node agents report theirs in each heartbeat.
- A node agent behind the control plane downloads the new binary from the control plane, verifies it, replaces itself, and restarts (systemd restarts it). Running sandboxes aren't affected; the agent doesn't run them, AerolVM does.
- **Compatibility window**: the control plane supports node agents from the previous minor version, so nodes can update after the control plane. A node agent older than that is told to update before anything else.
- **AerolVM upgrades** are a cluster operation, not automatic: the project admin (or instance admin) chooses a new fork version for a cluster, and Lewbox upgrades its nodes one at a time: drain (no new sandboxes placed), wait for sandboxes to move or stop as policy says, upgrade, health check, undrain ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox)).

**In-sandbox helpers** update themselves: a new agentd takes over when the old one is idle; a new portd when its port is next exposed ([binary-and-commands](binary-and-commands.md#version-handshake)).

### The AerolVM fork's releases

Details in [clusters/aerolvm-fork](../clusters/aerolvm-fork.md); the coupling with Lewbox's releases:

- The fork is tagged `vX.Y.Z-lewbox.N`: upstream's version, and our patch level on top. E.g. `v0.6.8-lewbox.2`.
- **We always build our own release assets** in the fork's CI (`sandboxd`, `toolboxd`, installers, for amd64 and arm64), so an upstream release without assets (as v0.6.5 was) never blocks us.
- Lewbox embeds `aerolvm.json`, listing fork versions it supports (tested in the nightly real-cluster job), the default for new clusters, and each version's asset URLs and checksums.
- A Lewbox release may *add* support for a newer fork version; it drops support for an old one only after a release that warns about clusters still on it.
- When the fork gains a fix that lets Lewbox delete a workaround (e.g. exit codes under gVisor), the workaround stays until every supported fork version has the fix, then is deleted, and [lessons](../lessons.md) records both.

### Custom builds

The release is the default build: the plugins in `plugins/config.json`. Anyone can build with more or fewer, with `lewbox build --with MODULE@VERSION --without NAME` ([plugins/building-and-enabling](../plugins/building-and-enabling.md#custom-builds)):

- It builds targets 0 to 3 the way the release does (both sandbox architectures included), in a temporary module pinned to the running binary's version, so a custom build differs from the release only in its plugins.
- It reuses the release's dashboard when no added plugin has UI; otherwise it needs Node and pnpm, or the `lewbox-builder` image.
- The result reports `build: custom` and its plugins' modules and checksums in `lewbox version --json` and `/v0/meta`, so support can tell a custom build at a glance.
- Custom builds aren't signed by us. Operators sign their own, if they distribute them.
- We publish `ghcr.io/harlew-dev/lewbox-builder:<version>` (Go, Node, pnpm, the release's sources) with each release.

### Development builds

- `make dev`: Postgres in Docker, `air` (or `watchexec`) rebuilding `lewbox serve --dev`, Vite's dev server for the dashboard proxied by `LEWBOX_DEV_WEB_URL`.
- `make dev-node`: a node in a local VM (Lima or Multipass) or a Docker-in-Docker node, for working on clusters without a server (where AerolVM supports it).
- A `docker-compose.yml` for trying Lewbox: `lewbox serve` and Postgres, with a generated keyring printed on first start (development only).

## Security

- Release builds run only on tags, in a workflow with protected environments; no secrets in pull request workflows from forks.
- Dependencies pinned by `go.sum` and `pnpm-lock.yaml`; `govulncheck` and `pnpm audit` in CI; Renovate for updates.
- Actions pinned by commit SHA.
- Signed artifacts and provenance ([signing](#signing)).
- The container runs as `nonroot` on a distroless base with no shell.

## Observability

- `lewbox_build_info{version,commit,go}`.
- Node versions per cluster on the cluster page and in `lewbox_nodes_by_version{cluster,agent_version,aerolvm_version}` (gauge).
- Release notes link to the docs' upgrade guide.

## Testing

- The release workflow runs in dry-run mode on every push to `main` (snapshot builds), so it never first runs on release day.
- Upgrade tests: start release N with a seeded database, roll to the candidate, run the API suite against a mix of N and N+1 replicas.
- Node upgrade test in the real-cluster job: install nodes at the previous release, upgrade the control plane, watch the nodes update themselves.

## Build plan

1. Makefile and CI jobs (lint, test, migrations, web, build).
2. Embedding: `internal/embedded`, build-order script, missing-asset failures.
3. GoReleaser configuration: archives, checksums, images, SBOMs, signing; dry-run on `main`.
4. `/node/v0/binary` and the node agent's self-update (with [clusters/node-agent](../clusters/node-agent.md)).
5. `aerolvm.json` and the fork's release pipeline (with [clusters/aerolvm-fork](../clusters/aerolvm-fork.md)).
6. Upgrade tests.
7. Homebrew and npm packages for the CLI (M4).
8. Plugin generation in CI, the `core-only` and `plugin-imports` jobs (M0); `lewbox build` and the builder image (Beta).

## Open questions

1. GoReleaser (free or Pro?) or our own release script?
2. Do we publish `.deb`/`.rpm` packages for nodes, or is the bootstrap script enough?
3. Where are docs hosted for self-hosters: embedded in the binary at `/docs`, or only lewbox.com/docs?
4. Do we need an LTS line, or only the latest minor, once past v1?
