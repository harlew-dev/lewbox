# Testing

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

How Lewbox is tested: the layers of tests, a real Postgres in every database test, the fakes that stand in for AerolVM, the toolbox, GitHub, email, the agent adapters and the rest, an injectable clock, end-to-end tests against real nodes, contract tests for the API and SDKs, and load tests. The aim is that almost everything can be tested on a laptop in seconds, and that what can't is tested nightly on real clusters.

## Today

- **App** (`lewbox-legacy`): Pest with SQLite in memory, Larastan, Pint (`composer ci:check`). A `PerformanceSeeder` makes a tenant with 5,000 sandboxes, 300,000 activity events, 100,000 agent chunks and two weeks of metrics from 20 nodes (`LEWBOX_PERFORMANCE_SEED_SCALE`). SQLite in tests and MySQL in production let dialect bugs through (the 64-character index name, sort buffers).
- **Gateway** (`lewbox-gateway-legacy`): `go test -race ./...` in CI, with gofmt and vet. Fakes:
  - `internal/aerolvm/aerolvmtest/aerolvmtest.go` (642 lines): an `httptest` multi-node AerolVM cluster. Any node answers about any sandbox; snapshots live on the node that took them until pushed; requests with `X-Cluster-Forwarded` are refused (as real nodes need mTLS for them); fault injection with `Fail`, `FailWith`, `SetDown`, `SetSnapshotPolls`, `FailPush`.
  - `internal/aerolvm/aerolvmtest/toolbox.go` (851 lines): the fake toolbox. `SetExec` scripts results; the exec stream runs `cat` (echo), `sleep` (until killed) or scripted output, anything else exits 127; sessions echo input, exit on KILL, attach replays then follows; every create makes a new session (as `toolboxd` does); a second `lewbox-portd` on a busy port exits 1; `SetExecLosesStatus` and `SetSessionsLoseStatus` simulate gVisor's lost exit codes (`-1`, `waitid: no such process`); files in memory with modes (`SetFileMode`, for the `chmod` check); `/proc/1/cmdline` simulated; `ToolboxWarmUp` answers 502/503 at first; `Touches`, `Killed`, `StreamStarts` record what happened.
  - `internal/lewbox/lewboxtest` (1,100+ lines): the fake app, modelling reserve, visibility, single operations, leases, preconditions, billing, sessions, ports, activity, conversations, GitHub. **Gone**: the app is no longer a separate service.
  - agentd tests re-exec the test binary as a scripted fake ACP adapter (`AGENTD_FAKE_ADAPTER=1`, `internal/agentd/fake_adapter_test.go`).
  - Server tests run the real router, authenticator and runner against both fakes.

## Goals

- **Real Postgres in every test that touches data.** No SQLite, no mocks of the database.
- **Fast**: the whole Go suite under 2 minutes on CI, parallel by default.
- **Fakes that behave like the real thing:** the AerolVM fake plays Lewbox's **minimum fork release** as its contract says, not legacy's bugs, since Lewbox has no workarounds for them ([D15](../README.md#decisions)). Kept honest by a contract suite run against a real node nightly.
- **Deterministic time.**
- **The API tested as clients see it**, and checked against the OpenAPI document.

## Design

### Layers

| Layer | What | Where | Runs |
|---|---|---|---|
| Unit | Pure functions: parsing, validation, Dockerfile rules, path rules, token formats, SSE decoding, chunk merging | Next to the code | Every push |
| Service | A domain service with a real database and fakes for everything external | `internal/<domain>/*_test.go` | Every push |
| API | The real router, auth, services and workers in-process, against a real database and fakes; requests as a client sends them | `internal/httpapi/*_test.go`, `internal/e2e` | Every push |
| Dashboard | Vitest for components and hooks; Playwright against `lewbox serve` with fakes | `web/` | Every push |
| SDK contract | The TypeScript and PHP SDKs' own suites against a test `lewbox serve` with fakes | `sdks/*` | Every push touching the API or an SDK |
| Real cluster | End-to-end scenarios against real nodes running the AerolVM fork on Docker (gVisor from the beta, Firecracker later) | `test/realcluster` | Nightly, and before each release |
| Load | Throughput and latency under load; big tables | `test/load` | Weekly, and before releases |

### Postgres

- Tests need `LEWBOX_TEST_DATABASE_URL` (a server where the test role can `CREATE DATABASE`). `make test` starts one with Docker if it isn't set; CI uses a `postgres:17` service container.
- **Template databases**: the first test package to start runs all migrations into `lewbox_test_template` (once per schema version, keyed by a hash of the migrations). Every test that needs a database calls `dbtest.New(t)`, which does `CREATE DATABASE lewbox_test_<random> TEMPLATE lewbox_test_template` (tens of milliseconds) and drops it on cleanup. Tests are fully isolated, can run in parallel, and can use their own transactions, `NOTIFY` and advisory locks, which per-test transaction rollback wouldn't allow.
- `dbtest.New` also returns a ready `app.Services` with fakes wired in, for service and API tests.
- **Factories** (`internal/testing/factory`): `factory.Project(t, db, …opts)`, `factory.Sandbox(t, db, factory.Running, factory.OnCluster(c))`. Built on the real services where possible, so factories can't create states the code can't.

### The clock

- `internal/clock`: `Clock` interface (`Now`, `After`, `NewTimer`, `NewTicker`, `Sleep`). Production uses the real clock; tests use `clocktest.Fake`, advanced by hand.
- Every timer goes through it: leases, backoffs, idle timeouts, token expiry, retention, metering. A lint test fails on `time.Now()`, `time.After`, `time.Sleep` outside `internal/clock` and tests.
- Postgres `now()` is avoided in queries that tests need to control; the time is passed as a parameter. Where `now()` is used (defaults, audit), tests don't depend on it.

### Fakes

All under `internal/testing/`, each an `httptest.Server` (or an in-process implementation of an interface where there's no network), with a Go API to script and inspect it.

| Fake | Stands in for | Notes |
|---|---|---|
| `aerolvmtest` | An AerolVM cluster on the minimum fork release: several nodes, sandboxes, snapshots on `/v1` with scrub and `reset_config`, registry push, images and builds, notify-mode timers and the backstop, lifecycle and session events, owner nodes, coded errors | Ported from the gateway, minus legacy's bugs and the Daytona routes. Grows with the fork ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md)) |
| `aerolvmtest` toolbox | `toolboxd` in each sandbox: execute, exec stream, sessions (`if_absent`), attach, logs, signal, resize, envd files and filesystem, ports, readiness | Ported, without legacy's bug simulations (lost exit codes, warm-up guessing, non-unique session names): the fork fixes those, and [lessons](../lessons.md) records each |
| `githubtest` | GitHub REST: App JWT auth, `GET /app`, manifest conversion, installation tokens (repositories and permissions checked as GitHub checks them), user OAuth and refresh, repositories and branches, webhooks signed with `X-Hub-Signature-256`; Git smart HTTP over a real bare repository (`git http-backend`) for the [push recipe](../git/integrating.md#testing). No pull request or `git-receive-pack` server of our own: Lewbox no longer does those | Grown from legacy `lewboxtest/github.go` and the PHP tests |
| `smtptest` | An SMTP server | In-process; captures messages so tests can read sign-in codes |
| `acptest` | `claude-agent-acp` and `codex-acp` | The test binary re-executed as a scripted adapter (`AGENTD_FAKE_ADAPTER=1`), kept from the gateway. Scripts: text, reasoning, tool calls, approvals, questions, plans, usage, background tasks, errors, crashes |
| `agentcli` | `claude` and `codex` CLIs for sign-ins | Prints a login URL, waits for a code, writes credentials ([profiles/agent-sign-ins](../profiles/agent-sign-ins.md)) |
| `dnstest.Fake` | A DNS provider's API, libdns-shaped | Zones, records, TXT checks; used by the contract suite of every `dns/libdns` provider and any other DNS plugin ([plugins/dns-providers](../plugins/dns-providers.md#contract-tests)) |
| MinIO (container) | S3, R2 and compatible stores, for buckets and registry GC | A real S3 API rather than a fake; Azurite and `fake-gcs-server` for other storage plugins ([plugins/storage-providers](../plugins/storage-providers.md#contract-tests)) |
| `webhooktest` | A project's webhook endpoint | Records deliveries, verifies signatures, answers as scripted (slow, 500, redirect, closed) |
| `oauthtest` | ChatGPT device auth, Anthropic token endpoints | For subscription refresh |
| `otlptest` | An OTLP collector | For profile telemetry tests ([profiles/telemetry](../profiles/telemetry.md)) |
| `nodetest` | A node agent | Enrolls, heartbeats, fetches config, for control-plane tests of the node API |

**Keeping fakes honest**: each fake has a *contract test suite*, a set of tests written against an interface, run against the fake on every push and against the real thing in the nightly real-cluster job (AerolVM and the toolbox) or with recorded fixtures (GitHub). When the real thing and the fake disagree, the fake is fixed.

### Provider contract suites

Plugins of the same kind must behave the same, so each kind has a suite every provider's tests call ([plugins](../plugins.md)):

| Suite | Package | Every provider of this kind runs it | Against |
|---|---|---|---|
| Agents | `plugin/agent/agenttest` | Spec validity, credentials and `Verify`, setup (secrets only on `/run/lewbox` or listed for the scrub; cleanup removes what was written; scrub paths cover every secret), sign-in flow, the agentd driver through `acptest` | The fake toolbox, `acptest`, `agentcli`, recorded provider APIs; nightly, one real turn |
| Git hosts | `plugin/gitprovider/gittest` | Reference parsing, access levels never exceeded, sandbox minting narrowed per repository, token API, listing, webhooks, the credential helper end to end | The provider's fake host (`githubtest`); nightly, the real host |
| DNS | `plugin/dns/dnstest` | Schema, zone lookup, idempotent set, delete of missing records, list | `dnstest.Fake`; nightly, a real test zone |
| Storage | `plugin/storage/storagetest` | Object calls, listing order and cursors, batched deletes, multipart, presign, the registry round trip and GC on a copy | MinIO (and emulators); nightly, real AWS S3 and R2 |

Plus the **plugin harness** (`plugin/plugintest`): the core with fakes and a chosen set of plugins, calling each extension point directly, and the example plugin that uses every one ([plugins/extension-points](../plugins/extension-points.md#testing)). And the **core-only** run: the lifecycle suite with no plugins compiled in ([build-and-release](build-and-release.md#ci)).

### `lewbox serve` with fakes

`LEWBOX_DEV_FAKES=aerolvm,github,smtp` starts the server with in-process fakes instead of real services: a fake cluster with nodes, a fake GitHub App, captured email. Used by:

- Playwright tests for the dashboard.
- SDK contract tests.
- Developing the dashboard without a cluster (`make dev-fake`).

Never available in production builds' `production` env: the setting is refused unless `LEWBOX_ENV=development`.

### API tests

- **As a client**: tests make HTTP requests with real keys made through the API, and assert on status, headers (`Lewbox-Request-Id`, `Idempotent-Replayed`, `Retry-After`, `WWW-Authenticate`), and JSON.
- **OpenAPI conformance**: in tests, a middleware validates every request and response against `api/openapi.yaml` (`kin-openapi`'s validator, or `libopenapi-validator` for 3.1). An undocumented field, status or error code fails the test ([api/openapi-and-codegen](../api/openapi-and-codegen.md)).
- **Error coverage**: a test asserts every error code in the error registry ([api/errors](../api/errors.md)) is produced by at least one test.
- **Isolation suite**: for every endpoint, a second project's key and a second profile's key get `404` (not `403`) for the first's resources; a sandbox key for another sandbox gets `404`; for routes outside its sandbox, `403 wrong_key_kind`. Generated from the route table, so new routes are covered automatically.
- **Golden files** for large responses (a sandbox with everything), updated with `-update`.
- **Streams**: SSE and WebSocket tests with real clients, including reconnecting mid-stream with `?after=` and attach replay, and a replica shutdown mid-stream.

### Concurrency and crashes

- `go test -race` always in CI.
- Multi-replica tests: two or three `serve` instances in one test process (separate `app.Services`, one database), for leases, leader election, `NOTIFY` fan-out, and crash recovery ([jobs-and-scheduling](jobs-and-scheduling.md#testing)).
- Fault injection in fakes: nodes down mid-operation, toolbox restarting, GitHub 502s, webhook endpoints hanging, the database restarting (the `NOTIFY` listener reconnects).

### Fuzzing

Go native fuzzing (`go test -fuzz`) for anything that parses untrusted input: Dockerfile checking, file paths (`path_forbidden`, `..`, symlinks), metadata keys, labels, token and id parsing, signed token parsing, SSE line decoding with UTF-8 boundaries (a legacy bug area), ACP JSON-RPC framing, webhook URL validation (SSRF rules). A short fuzz run in CI; long runs nightly.

### Real clusters

Nightly, and before every release:

- Small test clusters, kept running: Docker for the alpha, on amd64 and one arm64; a gVisor cluster from the beta and a Firecracker one when upstream ships it. Nodes on cheap cloud VMs, installed with `lewbox node install` from the build under test.
- A test control plane (`lewbox serve` from the build) with its own Postgres.
- Scenarios, as a Go test suite using the generated Go client:
  - Create from an image and a template; run commands; streaming and interactive; sessions with exit codes; files up and down (large ones); ports public, TCP and private (opened with a real browser via Playwright, including in an iframe with partitioned cookies).
  - Persist: stop, start on another node, files kept, persisted ports re-exposed, the image's command running again (the nested-toolbox lesson).
  - Idle timeout and lifetime stop a sandbox; the reconciler records it within 2 minutes.
  - Template build, then a sandbox from it.
  - An agent turn with a real adapter and a real, budget-capped API key (Claude Code and Codex), approvals, a worktree.
  - Node agent: install, enroll, heartbeat, certificate renewal (with a short lifetime), self-update to the build under test.
- Results posted to the PR or release; a failure blocks the release.

### Load

- `test/load`: k6 or a Go load tool. Scenarios: 1,000 concurrent command streams; 10,000 sandboxes listed and filtered by metadata; activity reads over 30 days × large volume; webhook fan-out to 50 endpoints; 500 SSE followers of turns.
- The **seeder** (`lewbox admin seed --scale N`, development only), from legacy `PerformanceSeeder`: a project with 5,000 sandboxes, 300,000 activity events, 100,000 chunks and two weeks of metrics from 20 nodes, times N. Used for load tests and for checking query plans (`EXPLAIN` on list endpoints in a test, asserting index use).

### Migration tests

- Every migration applies against a database at the previous version filled by the seeder.
- The legacy data migration ([migration](../migration.md)) runs against an anonymised legacy MySQL dump in CI.

### Lints that are tests

- No `time.Now` outside `internal/clock`; no `math/rand` outside tests.
- No secrets in job args.
- Every project-owned query is scoped ([database](database.md#conventions)).
- The `sandbox` build doesn't import server packages ([binary-and-commands](binary-and-commands.md)).
- Every metric documented; every setting documented; every error code tested.

## Build plan

1. `internal/testing/dbtest` with template databases; `make test` with Docker Postgres; CI service container.
2. `internal/clock` and the lint.
3. Port `aerolvmtest` and the fake toolbox; add contract suites.
4. `githubtest`, `smtptest`, `webhooktest`, `dnstest.Fake`, MinIO in CI, as their features land.
4a. The provider contract suites (`agenttest`, `gittest`, `dnstest`, `storagetest`) with the first plugin of each kind, and `plugintest` with the registry (M0–M3).
5. `acptest` and `agentcli`, with agents.
6. The OpenAPI validating middleware for tests.
7. The isolation suite generator.
8. `LEWBOX_DEV_FAKES` and Playwright set-up.
9. Real-cluster suite and its infrastructure (needs the fork and node agent first; M1–M2).
10. Seeder and load scenarios (M4).

## Open questions

1. Real-cluster infrastructure: whose cloud account, what budget, and which arm64 provider?
2. Real agent turns nightly cost money: a capped key per provider, or record and replay ACP sessions from real runs?
3. `kin-openapi` or `libopenapi-validator` (better 3.1 support) for conformance?
