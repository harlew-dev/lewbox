# Lewbox: the rebuild

> Status: Draft · Updated: 2026-10-29

Lewbox runs sandboxes for AI agents: isolated Linux machines, created on demand on your own servers, where an agent can run commands, read and write files, serve apps on public or private URLs, and run coding agents (Claude Code, Codex), without touching anything else.

Today Lewbox is two services: a Laravel app (`lewbox-legacy`) that owns the database and every decision, and a stateless Go gateway (`lewbox-gateway-legacy`) that serves the Consumer API and does the work on the nodes. We are rebuilding both as **one Go application, in one binary**, open source under the MIT license, with a first alpha in **December 2026** ([announcement](https://harlew.com/blog/lewbox-and-lewagent-are-going-open-source)).

These plans describe what we're building, feature by feature: what exists today, what we keep, what we change, what we drop, and how the new version works. They're the spec we build against, and they change as we learn.

---

## Contents

- [Goals](#goals)
- [Decisions](#decisions)
- [Glossary](#glossary)
- [Architecture](#architecture)
- [Tech stack](#tech-stack)
- [Repository layout](#repository-layout)
- [Ids, keys and tokens](#ids-keys-and-tokens)
- [Milestones](#milestones)
- [How these plans are written](#how-these-plans-are-written)
- [Index](#index)
- [Sources](#sources)
- [Open questions](#open-questions)

---

## Goals

1. **One binary, simple to self-host.** `lewbox serve` and a Postgres database are all a control plane needs. Nodes run `lewbox node` beside our [AerolVM fork](clusters/aerolvm-fork.md). No PHP, no queue workers, no separate gateway, no Redis.
2. **Keep everything Lewbox does today, and make it simpler.** Every feature of the legacy app and gateway has a home in these plans, including those that don't ship in the alpha.
3. **Remove the plumbing the split created.** The ~30 internal gateway→app endpoints, the 60-second key cache, the operation poll every second, event batching over HTTP, and the app calling *itself* through the public API with sandbox keys all become function calls, transactions and in-process channels.
4. **Isolation you can verify.** Trust boundaries are written down ([architecture](#trust-boundaries)), secrets never reach places they don't need to, and anyone can read the code that enforces it.
5. **One source of truth for the API.** One OpenAPI document generates the dashboard's client and the SDKs, and is checked against the server in CI, so docs, SDKs and server never drift (today the gateway's spec is missing routes the server has).
6. **Fix problems where they start.** Bugs in AerolVM are fixed in our fork and sent upstream as pull requests, rather than worked around in three places.
7. **Operable.** Structured logs, metrics, traces, health, and a clear story for upgrades: the control plane, the node agent and the in-sandbox helpers each know their version and update cleanly.

### Non-goals (for now)

- Wire compatibility with the legacy APIs. We can redesign the API now ([decision](#decisions)), and Lewagent moves to the new one ([migration](migration.md)).
- Supporting any database but Postgres.
- Our own VM runtime. Isolation comes from AerolVM, which supports Docker, gVisor and Firecracker.
- Billing customers with money. Lewbox meters compute and holds credits; projects bill their own customers. Lewbox's side of that is the [credits](credits.md) plugin.

---

## Decisions

Decisions made so far. Each links to the plan that works it out.

| # | Decision | Why | Plan |
|---|---|---|---|
| D1 | **One Go application, one binary**, with subcommands: `serve`, `node`, `agentd`, `portd`, `migrate`, and the CLI | The announcement; one thing to deploy and upgrade | [foundation/binary-and-commands](foundation/binary-and-commands.md) |
| D2 | **Postgres only** | One SQL dialect. Legacy ran SQLite in development and MySQL in production, and paid for it in migration workarounds. Postgres gives us `SKIP LOCKED` job queues, `LISTEN/NOTIFY`, partial indexes and JSONB | [foundation/database](foundation/database.md) |
| D3 | **Runtime: everything AerolVM supports** (Docker, gVisor, Firecracker), through **our own fork** of AerolVM. Fixes go into the fork first and upstream as pull requests; the fork is rebased on each upstream release. **Users choose their isolation:** a cluster offers one or more **isolation levels**, `container` (Docker), `gvisor` and `microvm` (Firecracker), named by level rather than by runtime; a sandbox picks one with `isolation`; a project sets which levels each profile may use. Firecracker isn't held back from the alpha: it's supported as soon as the fork supports it | Fix bugs at the source instead of in Lewbox; stay close to upstream; let each project decide how much isolation its work needs | [clusters/aerolvm-fork](clusters/aerolvm-fork.md), [clusters/runtimes](clusters/runtimes.md) |
| D4 | **A new API, `/v0`.** No obligation to match the legacy Lewbox or gateway endpoints. We keep what worked: the conventions in [lewbox.com/docs](https://lewbox.com/docs) (error codes, operations, idempotency, cursor pages, SSE and WebSocket protocols). `/v0` is a preview and may change; it becomes `/v1` once it's stable | Fix the inconsistencies of two APIs on two hosts with three key kinds, without promising stability before we have it | [api](api.md) |
| D5 | **Tenants become projects.** A project owns its clusters and its profiles: *Project → Clusters*, *Project → Profiles*. Multi-tenancy stays | "Project" says what it is; one instance hosts many | [projects](projects.md) |
| D6 | **The dashboard moves to React**, built with Vite and embedded in the binary | Matches Lewagent's stack; one frontend skill set | [dashboard](dashboard.md) |
| D7 | **The alpha** includes profiles, agent sign-ins, telemetry and webhooks, along with the core. It **doesn't** include the `credits` plugin (compute units, balances, metering, budgets), which is planned in full and comes after | Ship what Lewagent and self-hosters need first | [Milestones](#milestones) |
| D8 | **Git: GitHub App only, and Lewbox issues tokens rather than doing Git's work.** A profile connects the project's GitHub App for many GitHub people and organizations (25 by default, configurable); people and installations belong to the project and profiles link to them. Organizations that forbid third-party Apps can't connect, accepted for now. Lewbox clones repositories into sandboxes, keeps a token for the sandbox's repositories in it with **the access the user chooses per repository** (`read`, `write`, `full` or exact permissions; default `full`, optionally capped by the project), lists repositories and branches, forwards GitHub's webhooks, and **issues tokens** to a project's servers on request: installation tokens to admin and profile keys, user tokens (acting as a person) to admin keys only. Pushes, pull requests and creating repositories are left to the project, with those tokens; no pasted tokens | Don't rebuild Git and GitHub's API; keep only what needs Lewbox. Lewagent ports its push and pull request code from legacy, and keeps "nothing on the computer can push" by capping its sandboxes at `read` | [git](git.md), [git/integrating](git/integrating.md) |
| D9 | **Admin keys are admins.** An admin key acts on and inside any sandbox in its project directly, with no sandbox key in between | One auth path for a project's backend | [projects/admin-keys](projects/admin-keys.md), [api](api.md#decisions) |
| D10 | **agentd dials home over a WebSocket**, the agent channel: commands go down it, events and uploads come up. The toolbox only installs and starts agentd | Live events without an exec stream per follower; any replica can serve a turn | [agents/agentd](agents/agentd.md#the-agent-channel) |
| D11 | **No credential Lewbox imports ever enters a snapshot.** Agent logins, Git tokens, telemetry keys and agentd's token live on a memory-only mount in the sandbox; refreshed logins are read back before a stop; the snapshot is scrubbed of them on the node, whatever stopped the sandbox, its image config reset so create-time `env` isn't kept, and checked before upload (`scrub_failed` otherwise). Memory is never snapshotted. Setup writes them again on every start | A stopped sandbox's snapshot sits in the project's bucket for as long as the sandbox exists; legacy let AerolVM's idle and lifetime stops snapshot credentials | [sandboxes/profile-setup](sandboxes/profile-setup.md#credentials-never-enter-a-snapshot) |
| D12 | **Sandboxes run their own hooks, and Lewbox decides when to stop.** `hooks.on_create`, `on_start` and `on_stop` run the user's commands once after setup, at every start, and before every stop (idle and lifetime stops included) within a bounded `stop_grace`. A failing `on_create` or `on_start` hook fails its operation unless it's marked `required: false` (default `true`); an `on_stop` hook never blocks a stop; `scrub` lists paths removed on the node before any snapshot. The AerolVM fork's timers notify instead of stopping (`idle_warning`, `idle`, `lifetime_reached`); Lewbox re-checks and runs a normal stop, hooks first; if it doesn't act within a grace period, the node stops the sandbox itself after running the same hooks | Users decide what a sandbox does before it stops and what's never saved; timer stops can be warned about and put off while Lewbox knows the sandbox is busy, and still happen if the control plane is down | [sandboxes/hooks](sandboxes/hooks.md), [sandboxes/timers-and-activity](sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop) |
| D13 | **Plugins.** Features that vary between owners are plugins, compiled into the binary (as Caddy and CoreDNS do) and switched on or off for the instance only by configuration, and per project by the project's owners. Lewbox ships with defaults: `agents/claude-code`, `agents/codex`, `agents/opencode`, `git/github`, `dns/libdns` (Cloudflare, Route 53, DigitalOcean, Hetzner and more, through [libdns](https://github.com/libdns/libdns)), `storage/s3`, `telemetry` and `limits`, all on, and `credits`, off until an owner turns it on. The core stays: projects, profiles, clusters, sandboxes, agents' framework, Git's framework, templates, activity and webhooks. Adding an agent, a Git provider or a storage provider is writing one plugin against one interface; adding a DNS provider is usually a libdns import. Names in the API are provider-neutral (`git_provider_refused`, `/git/tokens`, `git_connection.*`, each with a `provider`), except GitHub's own forwarded webhooks (`github.<event>`). The plugin API is internal until after `/v1` | Owners choose the features they want; new providers don't touch the core; first-party features prove the extension points | [plugins](plugins.md) |
| D14 | **No "billing": `limits` and `credits`.** Lewbox doesn't bill anyone. The `limits` plugin enforces resource limits on profiles; the `credits` plugin meters compute units and keeps balances and budgets, and needs `limits` | Name things for what they do | [limits](limits.md), [credits](credits.md) |

---

## Glossary

| Term | Meaning | Id |
|---|---|---|
| **Instance** | One Lewbox installation: a control plane and its database, e.g. lewbox.com, or someone's homelab | |
| **Instance admin** | Someone who runs the instance: creates projects, sees everything. Set in configuration | |
| **User** | A person who signs in to the dashboard, by an emailed code | `usr_` |
| **Project** | *Was tenant.* What a team runs Lewbox as: its clusters, profiles, templates, webhooks and settings. Users are its members | `prj_` |
| **Admin key** | A project's API key, for managing the project and working in any of its sandboxes from its own servers (e.g. Lewagent's backend) | `key_`, token `lba_…` |
| **Cluster** | A project's nodes, one architecture, usually one region, offering one or more isolation levels. Sandboxes are created on a cluster and stay on it | `clu_` |
| **Node** | One server in a cluster, running AerolVM and `lewbox node` | `node_` |
| **Profile** | Who a project's sandboxes belong to: usually one of the project's own customers. It has credentials, keys, and, with those plugins, limits and credits | `prf_` |
| **Plugin** | A feature or provider compiled into Lewbox that an owner can turn on or off, e.g. `agents/codex`, `git/github`, `storage/s3`, `credits` | e.g. `git/github` |
| **Profile key** | A profile's API key, for creating and working with its sandboxes | `key_`, token `lbp_…` |
| **Sandbox** | An isolated Linux machine, at the isolation level it was created with. Its id stays the same through every stop and start | `sbx_` |
| **Sandbox key** | A short-lived key that reaches one sandbox, for browsers | `key_`, token `lbs_…` |
| **Template** | An image built from a Dockerfile, so sandboxes start with tools installed | slug, e.g. `claude-code` |
| **Build** | One template built on one cluster | `bld_` |
| **Operation** | Work on a sandbox or build that takes a while: create, start, stop, restart, delete, apply setup, build | `op_` |
| **Session** | A long-running process in a sandbox: a terminal, a dev server | `ses_` |
| **Port** | A port in a sandbox exposed publicly, over TCP, or privately behind a signed link | |
| **Conversation** | One coding agent's session in one sandbox and directory | `cnv_` |
| **Message / turn** | A prompt, and the agent's work in answer to it | `msg_` |
| **Toolbox** | AerolVM's daemon inside each sandbox (`toolboxd`): exec, sessions, files, ports | |
| **agentd** | Lewbox's agent runner inside a sandbox (`lewbox agentd`) | |
| **portd** | Lewbox's private-port proxy inside a sandbox (`lewbox portd`) | |
| **Node agent** | `lewbox node`: enrolls a node, keeps its certificates, firewall, metrics and AerolVM up to date | |
| **Activity** | A sandbox's log of what happened to it and in it | `evt_` |
| **Webhook** | An HTTPS endpoint of the project's that Lewbox sends events to | `whk_` |

---

## Architecture

```
                         ┌──────────────────────────────────────────────────────────┐
  Browser ──────────────▶│                    lewbox serve                          │
  (dashboard, React,     │                                                          │
   embedded)             │  HTTP:  /v0 API · dashboard · node API · webhooks in     │
                         │         port links · agent uploads · /up, /metrics       │
  Project servers ──────▶│                                                          │
  (admin key, profile    │  Work:  operations · reconciler · session watcher ·      │
   key, SDKs, CLI)       │         webhook delivery · metering · sign-ins ·         │
                         │         token refresh · pruning (jobs in Postgres)       │
  Browsers in project ──▶│                                                          │
  apps (sandbox keys)    │  Data:  Postgres (everything durable)                    │
                         └─────────────┬──────────────────────────▲─────────────────┘
                                       │ AerolVM API (PAT)        │ node API, tunnel
                                       │ over HTTPS or tunnel     │ (node token, mTLS)
                         ┌─────────────▼──────────────────────────┴─────────────────┐
                         │ Node:  AerolVM (our fork) · lewbox node · registry       │
                         │        Caddy (TLS, port URLs) · runtime: Docker,         │
                         │        gVisor or Firecracker                             │
                         │  ┌───────────────────────────┐ ┌──────────────────────┐  │
                         │  │ Sandbox                   │ │ Sandbox              │  │
                         │  │  toolboxd (PID 1)         │ │  …                   │  │
                         │  │  lewbox agentd → claude,  │ │                      │  │
                         │  │    codex (ACP adapters)   │ │                      │  │
                         │  │  lewbox portd (per port)  │ │                      │  │
                         │  └───────────────────────────┘ └──────────────────────┘  │
                         └────────────────────────────────┬─────────────────────────┘
                                                          │ registry layers
                                                    ┌─────▼──────┐
                                                    │ S3 / R2    │ snapshots, templates
                                                    └────────────┘
```

### One process, many replicas

`lewbox serve` is the API, the dashboard and every background worker in one process. Run one for a homelab; run several behind a load balancer for availability. Replicas share nothing but Postgres:

- **Jobs live in Postgres** and are claimed with `SELECT … FOR UPDATE SKIP LOCKED`, so any replica can do any job, and a crashed replica's jobs are picked up when their lease lapses. Enqueueing a job wakes workers at once through `LISTEN/NOTIFY`, which replaces the legacy poll every second ([foundation/jobs-and-scheduling](foundation/jobs-and-scheduling.md)).
- **Periodic work runs once**, on whichever replica holds the leader lock ([foundation/jobs-and-scheduling](foundation/jobs-and-scheduling.md#leadership)).
- **Live streams cross replicas** through `LISTEN/NOTIFY` where they need to. A browser following a turn can be on any replica ([api/streaming](api/streaming.md)).

### What runs where

| Where | What | Talks to |
|---|---|---|
| Control plane (`lewbox serve`) | API, dashboard, jobs, decisions, every secret | Postgres; AerolVM on each node (PAT); nodes' agents; GitHub; webhooks; S3/R2 (for registry GC and sizes); email (SMTP); DNS provider |
| Node (`lewbox node` + AerolVM fork) | Sandboxes; the node's registry; TLS for port URLs; firewall; metrics | Control plane's node API (node token); AerolVM locally |
| Sandbox (`toolboxd`, `lewbox agentd`, `lewbox portd`) | The work: commands, sessions, files, agents, private-port proxies | agentd posts events to the control plane with a per-conversation token; portd checks links with the control plane's public key |

### Trust boundaries

1. **Sandboxes are untrusted.** Anything in a sandbox may be hostile: the code an agent runs, the packages it installs, the pages it browses. A sandbox holds only what it needs: its profile's agent credentials and a Git token scoped to its own repositories, both on a memory-only mount and scrubbed from every snapshot ([D11](#decisions)). It never holds a key that reaches anything else. Its Git token reaches only its own repositories, with the access the user chose; **the default, `full`, lets anything in the sandbox push to them**, so a project that wants nothing in a sandbox to push gives `read` or caps its sandboxes at `read`, and pushes from its own servers with tokens Lewbox issues ([git/tokens](git/tokens.md#security)).
2. **Nodes are the project's.** A node holds a token that reaches only the node API for itself, and a certificate signed by its cluster's CA. The cluster's PAT for AerolVM is held by the control plane.
3. **The control plane holds the secrets**, encrypted at rest with a keyring ([foundation/secrets-and-encryption](foundation/secrets-and-encryption.md)).
4. **Projects are isolated from each other.** Every query is scoped by project, and one project's key can't see another's anything.
5. **Profiles are isolated from each other** within a project. A profile key reaches its own sandboxes on the clusters it's allowed.

### Key flows

The plans describe each in full. In short:

- **Create a sandbox** ([sandboxes/lifecycle](sandboxes/lifecycle.md)): check the key, limits and idempotency in one transaction that inserts the sandbox and its `create` operation, and enqueue the job. A worker creates it on a node through AerolVM, records each step, applies profile setup, clones repositories, exposes template ports, and finishes. The request waits up to 60 s for the operation.
- **Run a command** ([inside-a-sandbox/commands](inside-a-sandbox/commands.md)): check the key and that the sandbox is running, find its node, call the toolbox, stream or return the result, and record activity.
- **Prompt an agent** ([agents/conversations](agents/conversations.md)): record the turn, ensure agentd runs in the sandbox and is connected over its agent channel, send it the prompt, and relay its events as SSE as they come up the channel. The control plane stores events and chunks and ends the turn.

---

## Tech stack

Proposed. Each is argued in its plan, and open to change until it's built.

| Area | Choice | Plan |
|---|---|---|
| Language | Go 1.27+ | [foundation](foundation.md) |
| HTTP | `net/http` with Go's pattern routing; `coder/websocket` for WebSockets | [api](api.md) |
| Database | Postgres 17+, `pgx/v5`, `sqlc` for typed queries, embedded SQL migrations | [foundation/database](foundation/database.md) |
| Jobs | Postgres-backed queue (River, or our own small one on `SKIP LOCKED`), `LISTEN/NOTIFY` wake-ups, a leader lock for periodic jobs | [foundation/jobs-and-scheduling](foundation/jobs-and-scheduling.md) |
| API contract | OpenAPI 3.1, the source of truth; generated TypeScript types for the dashboard and SDK; CI checks the server against it | [api/openapi-and-codegen](api/openapi-and-codegen.md) |
| Secrets | Envelope encryption (XChaCha20-Poly1305) with a rotatable keyring | [foundation/secrets-and-encryption](foundation/secrets-and-encryption.md) |
| Logs, metrics, traces | `log/slog` (JSON), Prometheus `/metrics`, OpenTelemetry traces | [foundation/observability](foundation/observability.md) |
| Dashboard | React 19, TypeScript, Vite, Tailwind 4, Base UI, TanStack Router and Query, xterm.js; built into `web/dist` and embedded with `go:embed` | [dashboard/stack-and-structure](dashboard/stack-and-structure.md) |
| Runtime | AerolVM fork (`sandboxd`, `toolboxd`), Docker, gVisor, Firecracker | [clusters/aerolvm-fork](clusters/aerolvm-fork.md) |
| Agents | ACP adapters `claude-agent-acp` and `codex-acp`, driven by `lewbox agentd` | [agents/agentd](agents/agentd.md) |
| Email | SMTP, for sign-in codes and notifications | [projects/users-and-login](projects/users-and-login.md) |

---

## Repository layout

Module `github.com/harlew-dev/lewbox`. Proposed:

```
cmd/lewbox/              main: one binary, every subcommand
internal/
  app/                   wiring: config → services → server and workers
  config/                configuration from env and file, validated at once
  db/                    migrations (embedded), sqlc queries, transactions
  jobs/                  the job queue, workers, leader lock, periodic jobs
  httpapi/               router, middleware, errors, pagination, streaming, handlers by area
  auth/                  keys, sessions, permissions, scopes
  secrets/               keyring and encryption
  projects/ users/ profiles/ clusters/ nodes/ sandboxes/ operations/
  templates/ ports/ sessions/ files/ agents/ git/ hooks/
  activity/ webhooks/ signins/
  plugin/                the plugin registry and extension points
  aerolvm/               client for our AerolVM fork, and the toolbox
  nodeagent/             `lewbox node`
  agentd/                `lewbox agentd` (runs in sandboxes)
  portd/                 `lewbox portd` (runs in sandboxes)
  cli/                   the `lewbox` CLI's commands
  testing/               fakes: AerolVM cluster, toolbox, GitHub, SMTP, ACP adapter
plugins/
  config.json            which plugins a build compiles in, and their defaults
  agents/claude-code/ agents/codex/ agents/opencode/
  git/github/  dns/libdns/  storage/s3/
  telemetry/  limits/  credits/
                         each: plugin.go, migrations/, openapi.yaml, web/, agentd/ (agents)
web/                     the React dashboard (the shell, and slots plugins fill)
api/openapi.yaml         the API contract
sdks/typescript/  sdks/php/   generated and hand-written SDK code
docs/                    user documentation (lewbox.com/docs)
plans/                   these plans
```

---

## Ids, keys and tokens

Every resource has a public id: a prefix and 14 base62 characters, e.g. `sbx_Qm4pL0sX2bN8cV`. Ids never reveal order or count. Secrets are a prefix and 64 lowercase hex characters; Lewbox keeps only their SHA-256 hash and last four characters. Full list in [foundation/ids-and-keys](foundation/ids-and-keys.md).

| Token | Prefix | Reaches | Was |
|---|---|---|---|
| Admin key | `lba_` | One project's Admin API | Tenant key `lbt_` |
| Profile key | `lbp_` | One profile's sandboxes, on its clusters, with its permissions | Same |
| Sandbox key | `lbs_` | One sandbox, for 15 minutes, for browsers | Same |
| Node token | `lbn_` | One node's node API | Same idea, unprefixed |
| Install token | `lbi_` | One cluster installation, once | Same idea, unprefixed |
| Setup link | `lbl_` | One profile's hosted setup page | Same idea, unprefixed |
| Agent channel token | signed (Ed25519) | One sandbox's agent channel | Upload token, per conversation |
| Port link | signed (Ed25519) | One private port, for a minute | Same |

Other prefixes the plans add (`mem_` memberships, `gha_` GitHub Apps, `txn_` credit transactions, `wtr_` worktrees, and the rest) are listed in [foundation/ids-and-keys](foundation/ids-and-keys.md), which is the one list to keep up to date.

---

## Milestones

Today is 2026-10-29. Dates are targets.

| Milestone | Target | Contents |
|---|---|---|
| **M0: Foundation** | 2026-11-09 | Binary and subcommands, the plugin registry and extension points, configuration, Postgres and migrations, jobs, API skeleton (errors, pagination, idempotency, request ids), authentication and permissions, ids, secrets, observability, test fakes, CI and release builds. AerolVM fork set up, with its release pipeline |
| **M1: Projects, clusters, sandboxes** | 2026-11-23 | Users and sign-in, projects and members, admin keys, profiles, profile keys and limits; clusters, installation, node agent, PKI, DNS, storage; sandbox lifecycle, operations, persistence, timers, reconciler; templates and builds |
| **M2: Inside a sandbox** | 2026-11-30 | Toolbox client, commands (whole, streamed, interactive), sessions, files, ports, private ports, SSH keys, sandbox keys; hooks and scrub paths, notify-mode timers and the backstop |
| **M3: Agents and profile setup** | 2026-12-11 | agentd and its agent channel, conversations, turns and events, approvals and questions, models and modes, worktrees, background tasks; profile setup, agent credentials, agent sign-ins, telemetry; GitHub App connection, sandbox Git tokens and the token API |
| **M4: Alpha** | 2026-12-21 | Activity log, webhooks, the React dashboard, the CLI, a TypeScript SDK, docs, hardening, a public release |
| **Beta** | Q1 2027 | The `credits` plugin (metering, balances, budgets); PHP SDK; data migration from legacy; Lewagent and lewbox.com moved over; `/v1` |
| **Later** | | Custom domains, shared clusters, standalone worktrees, file watching, sandbox stats, an E2B-compatible facade, an OpenAI-compatible endpoint, conversation encryption |

**In the alpha** (D7): everything in M0 to M4.
**Not in the alpha, planned in full:** the [credits](credits.md) plugin (compute units, balances, metering, budgets).
**Not in Lewbox:** pushes, pull requests and creating repositories. Projects do them with tokens Lewbox issues ([git/integrating](git/integrating.md)).

Each plan's header says which milestone it belongs to; a plan may split, e.g. *Alpha* for its core and *Beta* for one part.

---

## How these plans are written

### Files

- `plans/{feature}.md` gives an overview of a feature area: what it is, how its parts fit, and an index of its parts.
- `plans/{feature}/{part}.md` goes into one part in detail.
- Plans link to each other rather than repeat each other. A fact lives in one plan.

### Each plan has

```markdown
# Title

> Status: Draft | Agreed | Building | Built · Milestone: Alpha (M2) | Beta | Later · Updated: YYYY-MM-DD

One paragraph: what this is, and why.

## Today            What legacy does, with file paths, and what's wrong with it
## Goals            And non-goals
## Keep, change, drop
## Design           Concepts, data model (tables and columns), states, flows, edge cases
## API              Endpoints, request and response fields, errors
## Security
## Observability    Logs, metrics, activity events, webhooks
## Configuration
## Testing
## Build plan       Go packages, and the work broken into tasks
## Open questions
```

Leave out a section that doesn't apply. Write for someone who knows Go and Postgres but not Lewbox.

### Words

- *Project*, never tenant. *Admin key*, never tenant key.
- *Sandbox* in the API; Lewagent calls them computers.
- *Lewbox* for the whole application; *control plane* for `lewbox serve`; *node agent* for `lewbox node`.

---

## Index

| Plan | What it covers | Milestone |
|---|---|---|
| [foundation](foundation.md) | The binary, configuration, database, jobs, ids, secrets, observability, testing, build and release | M0 |
| [api](api.md) | API structure, conventions, errors, authentication, permissions, idempotency, rate limits, streaming, OpenAPI | M0 |
| [projects](projects.md) | Users and sign-in, projects, members and roles, admin keys, instance administration | M1 |
| [plugins](plugins.md) | The plugin system: registry, extension points, building and enabling, and the provider interfaces for agents, Git, DNS and storage, with how to add one | M0 |
| [profiles](profiles.md) | Profiles, profile keys, setup links, agent credentials, agent sign-ins, SSH keys, telemetry | M1, M3 |
| [clusters](clusters.md) | The AerolVM fork, runtimes and isolation levels, clusters, nodes, installation, the node agent, connectivity, PKI and DNS, storage and registry, health, placement | M0, M1 |
| [sandboxes](sandboxes.md) | Lifecycle, operations, persistence, profile setup, repositories, timers and activity (Lewbox decides when to stop), hooks and scrub paths, reconciler, sandbox keys | M1, M2 |
| [inside-a-sandbox](inside-a-sandbox.md) | Toolbox, commands, sessions, files, ports, private ports, SSH | M2 |
| [agents](agents.md) | agentd, conversations, turns and events, approvals and questions, models and modes, worktrees, background tasks, storage | M3 |
| [templates](templates.md) | Templates, builds, Dockerfile rules, provided templates | M1 |
| [git](git.md) | GitHub App connections, sandbox tokens, the token API, webhook forwarding, and how to push and open pull requests yourself | M3 |
| [limits](limits.md) | The `limits` plugin: resource limits on profiles, per cluster, enforced when sandboxes are created and changed | M1 |
| [credits](credits.md) | The `credits` plugin: compute units, metering, balances, budgets and usage | Beta |
| [activity-and-webhooks](activity-and-webhooks.md) | Activity log, outbound webhooks, inbound GitHub webhooks | M4 |
| [dashboard](dashboard.md) | The React dashboard: stack, navigation, every page, setup pages | M4 |
| [sdks-and-cli](sdks-and-cli.md) | The CLI, TypeScript, PHP and Go SDKs | M4, Beta |
| [migration](migration.md) | Moving data and Lewagent from legacy | Beta |
| [lessons](lessons.md) | What legacy learned the hard way: AerolVM, gVisor, toolbox, agents, Laravel Cloud | All |

---

## Sources

- Legacy app: `../lewbox-legacy` (Laravel 13, Svelte 5). Design docs in `docs/{api,clusters,profiles,acp,sdks,todo}.md`; public, admin and private docs in `docs/{public,admin,private}/`.
- Legacy gateway: `../lewbox-gateway-legacy` (Go). `api/openapi.yaml`, `docs/*.md`, and 61 commits of lessons.
- Lewagent: `../lewagent-legacy`, the largest consumer. `docs/todo.md` § *Waiting on Lewbox* lists what it needs.
- Public docs: [lewbox.com/docs](https://lewbox.com/docs) ([llms.txt](https://lewbox.com/llms.txt)), the preview of the API we design from.
- Announcement: [Lewbox and Lewagent are going open source](https://harlew.com/blog/lewbox-and-lewagent-are-going-open-source).
- AerolVM: [aerol-ai/microvm](https://github.com/aerol-ai/microvm); Firecracker snapshots and `install.sh` support are on its roadmap ([discussion #785](https://github.com/aerol-ai/microvm/discussions/785)).

---

## Open questions

Questions that cut across plans. Each plan keeps its own.

1. **Hosted shape.** On lewbox.com, does every project bring its own servers, or does the instance also offer shared clusters that many projects use? The blog says the hosted control plane "reaches your servers through a secure tunnel or exposed ports" ([clusters/connectivity](clusters/connectivity.md)).
2. **Project ↔ profile ↔ Lewagent.** Lewagent today makes one profile per user. Should a Lewagent workspace map to a profile instead?
3. **Name of the fork.** `harlew-dev/aerolvm`? And do we vendor its client package, or import it as a module ([clusters/aerolvm-fork](clusters/aerolvm-fork.md))?
4. **Dashboard and API.** Does the dashboard call the same Admin API as admin keys do, with a session cookie? (Proposed: yes, [api](api.md).)
5. **Subscription logins on lewbox.com.** Settled for self-hosting, where logins stay on the user's own machines. Revisit with Anthropic and OpenAI before the hosted service stores them ([profiles/agent-sign-ins](profiles/agent-sign-ins.md)).
6. **Job queue.** River (MPL-2.0) for jobs, with operations keeping their own leases, or our own small queue on `SKIP LOCKED` ([foundation/jobs-and-scheduling](foundation/jobs-and-scheduling.md))?
7. **Token checksums.** Add checksum characters to `lba_`/`lbp_`/`lbs_` and register them with GitHub secret scanning? This changes the 64-hex format above ([foundation/ids-and-keys](foundation/ids-and-keys.md)).
