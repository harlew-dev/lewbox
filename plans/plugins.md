# Plugins

> Status: Draft · Milestone: Alpha (registry in M0; first-party plugins as their features land) · Updated: 2026-10-29

Lewbox is a small core with features plugged into it. The core runs sandboxes on clusters for profiles. Everything that varies from one installation to another is a **plugin**: which coding agents there are, which Git host, which DNS provider and object store, whether sandboxes are limited or spend credits, where telemetry goes. Plugins are Go packages **compiled into the binary** (the Caddy and CoreDNS model, not Go's `plugin` package), shipped with sensible defaults, and switched on or off per instance and per project. Adding another agent, Git host, DNS provider or object store means writing one plugin against a typed interface, with a checklist and a contract test suite to pass.

This is the overview. The parts:

| Plan | What it covers | Milestone |
|---|---|---|
| [plugins/registry-and-lifecycle](plugins/registry-and-lifecycle.md) | The `Plugin` interface, `Info`, the registry, dependency order, startup, start and stop, enable and disable, what a disabled plugin still does, the host a plugin gets | M0 |
| [plugins/extension-points](plugins/extension-points.md) | Every hook into the core, with Go signatures, ordering, transaction rules and failure semantics: routes, migrations, jobs, permissions, events, settings, admission, setup contributors, lifecycle handlers, sandbox fields, CLI commands, UI slots, agentd drivers | M0 |
| [plugins/building-and-enabling](plugins/building-and-enabling.md) | `plugins/config.json`, the generator, `lewbox build --with`, instance and project enablement, configuration, the API for it | M0 |
| [plugins/agent-providers](plugins/agent-providers.md) | Coding agents: the `agent.Provider` interface, the spec agentd runs from, credentials, sign-in flows, setup, harvest, the agentd driver; adding an agent | M3 |
| [plugins/git-providers](plugins/git-providers.md) | Git hosts: the `gitprovider.Provider` interface under the core Git framework; adding a Git host | M3 |
| [plugins/dns-providers](plugins/dns-providers.md) | DNS for nodes and certificates: the `dns.Provider` interface, libdns adapters; adding a DNS provider | M1 |
| [plugins/storage-providers](plugins/storage-providers.md) | Object storage for snapshots and templates: the `storage.Provider` interface, the node registry's driver; adding a store | M1 |
| [plugins/dashboard-slots](plugins/dashboard-slots.md) | How plugins appear in the React dashboard and setup pages: slots, schema-driven settings forms, plugin pages | M4 |

---

## Today

Legacy has no plugin system; variation is hard-coded in several places at once.

- **Agents** are `if` branches across both codebases: the app's setup steps (`lewbox-legacy/app/Sandboxes/Setup/*`), credential providers (`app/Enums/AgentCredentialProvider.php`), sign-ins (`app/Sandboxes/SignIns/*`), the gateway's agent specs (`lewbox-gateway-legacy/internal/agentd/agents.go`) and the Codex stand-in. Adding OpenCode touches about fifteen files in two repositories.
- **Git** is GitHub only, written straight into the app (`app/Actions/GitHub/*`, `app/Sandboxes/Setup/GitSetup.php`).
- **DNS** is Cloudflare only (`app/Actions/Clusters/TestDnsConnection.php`, `install.sh`), **storage** is S3 or R2 through flysystem (`TestStorageConnection.php`, `CleanUpRegistry.php`).
- **Billing** (credits, metering, budgets) is always on for every tenant, with a 5-second metering loop whether anyone uses credits or not.
- **Telemetry** providers are a PHP enum.

So nothing can be turned off, and nothing can be added without touching the core.

## Goals

1. **A small core that doesn't know which agents, Git hosts, DNS providers or stores exist.** It defines what they must do; plugins do it.
2. **Adding one is a checklist, not an archaeology project.** One directory, one interface, one contract test suite.
3. **Turn features off cleanly**, per instance and per project, without losing data.
4. **Still one binary,** type-checked, at full speed, with no runtime loading.
5. **First-party features are plugins from the first commit**, so the extension points are real, used by at least two plugins each where it matters, and not designed in the abstract.

### Non-goals

- **Loading plugins at run time** from `.so` files (Go's `plugin` package needs the exact same Go and dependency versions, works on Linux and macOS only, and can't unload). Never.
- **Out-of-process plugins** (gRPC as HashiCorp's go-plugin does, or WASM) for third parties who won't rebuild the binary or don't write Go: Later, if there's demand.
- **A stable plugin API before `/v1`.** The plugin API is internal and may change in any release until then; third-party plugins are possible from the start (they're Go modules), but at their own risk.
- **Third-party React loaded at run time** (import maps, module federation): Later ([dashboard-slots](plugins/dashboard-slots.md#later-runtime-ui-plugins)).

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Every feature legacy has. Each moves into core or into a plugin, nothing is lost |
| **Change** | Agent specs, credential providers, setup steps and sign-in flows → one agent plugin per agent. GitHub → the `git/github` plugin under a core Git framework. Cloudflare → `dns/libdns`, one plugin covering many DNS hosts through libdns (Cloudflare the default); S3 and R2 → `storage/s3`. Billing → two plugins, `limits` and `credits` (Lewbox bills nobody; decided 2026-10-29). Telemetry → the `telemetry` plugin. Hard-coded enums → registries the dashboard and SDKs read |
| **Drop** | The always-on metering loop; agent-specific branches in core code |

## Design

### Core and plugins

| Core (always present) | Plugins (shipped by default, each can be turned off) |
|---|---|
| Users, projects, members, admin keys | |
| Profiles, profile keys, sandbox keys, setup links, SSH keys | `limits`: resource limits on profiles ([limits](limits.md)) |
| Clusters, nodes, the node agent, PKI, placement, isolation levels | `dns/libdns`: node DNS records and DNS-01, for Cloudflare and the other libdns providers ([clusters/pki-and-dns](clusters/pki-and-dns.md)) |
| | `storage/s3`: snapshots and template images in S3, R2, MinIO or any S3-compatible store ([clusters/storage-and-registry](clusters/storage-and-registry.md)) |
| Sandboxes, operations, persistence, hooks, timers, the reconciler | `credits`: compute units, balances, metering, budgets ([credits](credits.md)) |
| Inside a sandbox: toolbox, commands, sessions, files, ports, private ports | |
| Templates and builds | |
| The agent framework: conversations, turns and events, approvals, worktrees, agentd, the agent channel, the sign-in machinery | `agents/claude-code`, `agents/codex`, `agents/opencode` (planned) |
| The Git framework: the sandbox's Git setting, token files, credential helper and wrapper, refresh, the token API's shape | `git/github` |
| Profile setup and the snapshot scrub | `telemetry`: OTLP export from agents ([profiles/telemetry](profiles/telemetry.md)) |
| Activity, webhooks, notifications | |
| The dashboard shell, the API, the plugin registry | |

The rule for where something goes: **core is what every installation needs and what other features stand on; a plugin is a choice** (of vendor, of policy) or something an installation may not want at all. The frameworks (agents, Git) are core because conversations, worktrees and the sandbox's Git setting are the same whatever the agent or host; the providers that fill them in are plugins.

### The default plugins

| Plugin | Kind | Owns | On by default | Milestone |
|---|---|---|---|---|
| `limits` | feature | `profile_limits`; limit kinds; the admission check on create, start and settings changes; template limits; `…/limits` routes | Instance: on. Projects: on | M1 |
| `credits` | feature | Credit buckets, the ledger, rates, budgets, usage roll-ups; admission (`402 credits_exhausted`); exhaustion and checkpoint jobs; `…/credit-*` and `…/usage` routes. **Requires `limits`**. `sandbox_runs` (when each sandbox ran, at what size) stays core, so usage history exists whether or not credits are on | Instance: on. Projects: **off** until a project turns it on | Beta |
| `dns/libdns` | driver | DNS providers for clusters and the relay domain, through [libdns](https://github.com/libdns): Cloudflare (default, supported), Route 53, DigitalOcean, Hetzner, Google Cloud DNS, Azure, Gandi, Porkbun, deSEC, RFC 2136; which are compiled in is set in `config.json` ([plugins/dns-providers](plugins/dns-providers.md)) | Instance: on | M1 |
| `storage/s3` | driver | A storage provider (AWS S3, Cloudflare R2, MinIO, any S3-compatible store), and the node registry's `s3` driver | Instance: on | M1 |
| `agents/claude-code` | agent | Claude Code's spec, its credentials (API key, subscription), sign-in flow, setup step, agentd driver, its lines in the provided `agents` template | Instance and projects: on | M3 |
| `agents/codex` | agent | Codex's spec, credentials (API key, ChatGPT login), device sign-in, ChatGPT login refresh and harvest, setup step, the Codex stand-in driver | Instance and projects: on | M3 |
| `agents/opencode` | agent | OpenCode, driven by a manifest through the generic ACP driver (`opencode acp`) | Compiled in when ready; off until tested | Later |
| `git/github` | git | The project's GitHub App, people and accounts, installation and user tokens, repository listing, inbound GitHub webhooks and their forwarding | Instance and projects: on | M3 |
| `telemetry` | feature | `profile_telemetry`, OTLP presets (Langfuse, Honeycomb, Grafana Cloud, LangSmith, custom), the telemetry setup step, routes | Instance and projects: on | M3 |

Plugin names are `kind/name` for providers and a single word for features. They're stable identifiers: they appear in configuration, the API, error bodies and table ownership.

### Turning plugins on and off

Three layers, each narrower than the last ([building-and-enabling](plugins/building-and-enabling.md)):

1. **The build** decides which plugins exist: `plugins/config.json` lists what's compiled in. A plugin not in the build has no routes (`404`), no tables, no settings; a field it would own is an unknown field (`422 validation_failed`).
2. **The instance** decides which compiled plugins are on, and how projects get them (`all`, `opt_in` or `opt_out`), **in configuration only** (`[plugins.credits]`, or `LEWBOX_PLUGINS_*`). There's no API or dashboard switch for it: changing it is editing the configuration and rolling the replicas ([building-and-enabling](plugins/building-and-enabling.md#configuration)). Instance admins see the result read-only (`GET /v0/instance/plugins`, the dashboard's *Plugins* page).
3. **The project** turns an instance-enabled plugin on or off for itself, when the instance lets it (`PATCH /v0/projects/{prj}/settings/plugins`, [projects/settings](projects/settings.md#plugins)).

**Disabling never deletes data.** A disabled plugin:

- answers its routes with **`409 plugin_disabled`**, carrying `plugin: {name, scope: "instance" | "project"}` ([api/errors](api/errors.md#405-406-409)). `409` because the request is valid and the plugin exists, but the instance's or project's state refuses it, as `cluster_suspended` does;
- refuses new use elsewhere the same way: a conversation with a disabled agent, a token from a disabled Git host, a cluster saved with a disabled DNS provider;
- stops contributing to admission (with `limits` off, nothing is limited; with `credits` off, nothing spends credits), lifecycle handlers and periodic jobs; its pending jobs for that scope are cancelled, and re-created by its `Reconcile` when it's turned back on;
- **is removed from running sandboxes at once.** Turning it off enqueues an `apply_setup` on every running sandbox in scope, which runs the plugin's **cleanup** for what its last apply wrote (Git tokens, agent logins, telemetry keys) and doesn't apply it again; its **scrub paths still run** at the next snapshot for anything a sandbox that was stopped at the time still holds, so turning a plugin off can never leave its credentials in a sandbox or a snapshot ([extension-points](plugins/extension-points.md#setup-contributors), [projects/settings](projects/settings.md#plugins)). The sandboxes' own records (e.g. a `git` setting) are kept, so turning it back on and re-applying restores them;
- keeps its tables, rows and settings, so turning it back on resumes where it was.

**Dependencies** are declared (`credits` requires `limits`; the sign-in machinery needs an enabled agent provider with a subscription method). Configuration that disables a plugin another enabled one requires fails at start-up, naming both; a project doing the same, or turning on a plugin whose dependency is off, gets `422 plugin_dependency`. Disabling a driver that's in use (a project's clusters use a `dns/libdns` provider) is refused with `409 plugin_in_use`, naming what uses it.

### Where code lives

```
plugin/                       the plugin API (importable by other modules; internal and unstable until /v1)
  plugin.go                   Plugin, Info, Registrar, Host
  agent/  gitprovider/  dns/  storage/  setup/  admission/  lifecycle/  ui/
  agent/agenttest/  gitprovider/gittest/  dns/dnstest/  storage/storagetest/   contract suites
plugins/
  config.json                 which plugins a build includes, and their defaults
  agents/claude-code/         plugin.go · agentd/ · web/ · openapi.yaml · migrations/ · docs/
  agents/codex/
  agents/opencode/
  git/github/
  dns/libdns/                 providers/ (one file per libdns provider)
  storage/s3/
  limits/
  credits/
  telemetry/
```

`plugin/` is outside `internal/` on purpose: Go forbids importing `internal/` packages from another module, so a third-party plugin could never be written against it. It's a façade over core services. A plugin imports only `plugin/...` and the standard library and its own dependencies, never `internal/...` (a `depguard` lint enforces it for first-party plugins too, so they're honest examples).

### How it fits the rest

- **The binary** gains a generated `plugins_gen.go` and `lewbox build --with …` for custom builds ([foundation/binary-and-commands](foundation/binary-and-commands.md#plugins-in-the-binary)).
- **Configuration** gains `[plugins.<name>]` sections ([foundation/configuration](foundation/configuration.md#plugins)).
- **The database** runs every compiled plugin's migrations, enabled or not, after the core's ([foundation/database](foundation/database.md#plugin-migrations)).
- **The API** is one document merged from the core's and each plugin's fragments; `/v0/meta` lists the instance's plugins ([api/openapi-and-codegen](api/openapi-and-codegen.md#plugin-fragments)).
- **The dashboard** mounts plugins into slots and builds their settings forms from their schemas ([dashboard-slots](plugins/dashboard-slots.md)).
- **The SDKs** group a plugin's operations under its name and raise `PluginDisabledError` ([sdks-and-cli](sdks-and-cli.md#plugins)).

## API

| Method | Path | Who | Permission | M | What |
|---|---|---|---|---|---|
| GET | `/v0/meta` | anyone | — | M0 | Includes `plugins`: each compiled plugin's `name`, `kind`, `title`, `version` and whether the instance has it on |
| GET | `/v0/instance/plugins` | S (instance admin) | — | M0 | Read-only: every compiled plugin with its instance settings (secrets redacted), dependencies, `projects` mode and what uses it, and where each value came from (config file, environment, `config.json` default). There's no write route: instance enablement is configuration only |
| GET | `/v0/projects/{prj}/settings/plugins` | S, A | `project.read` | M0 | The plugins the instance allows this project, each with `enabled`, `scope` (who decided), whether the project can change it, what it requires and `blocked_by` ([projects/settings](projects/settings.md#plugins)) |
| PATCH | `/v0/projects/{prj}/settings/plugins` | S (owner, admin), A | `project.write` | M0 | `{"plugins": {"credits": true}}`. `422 plugin_dependency`, `409 plugin_in_use`, `409 plugin_disabled` (off for the instance) |
| GET, PATCH | `/v0/projects/{prj}/settings/{plugin}` | S, A | `project.read` / `project.write` | M0 | A plugin's project settings, validated by its schema; secret fields write-only. The same area path the core's settings use (`…/settings/credits`) |

`{plugin}` is the name with its slash, e.g. `/v0/projects/prj_…/settings/git/github`; `plugins` is reserved. Details: [building-and-enabling](plugins/building-and-enabling.md#api).

## Security

- **A compiled-in plugin can do anything the server can.** That's acceptable for first-party plugins and ones an operator chose to build in; it's why third-party plugins are a build-time decision by whoever runs the instance, never something a project can upload. Untrusted code needs the out-of-process tier (Later).
- **Plugins reach the database through the host's scoped helpers**, and the same query lint as core ([foundation/database](foundation/database.md#conventions)) runs on first-party plugins' queries: every project-owned table filtered by project or profile.
- **Secrets in plugin settings** are encrypted with the keyring like any other ([foundation/secrets-and-encryption](foundation/secrets-and-encryption.md)), marked in the settings schema, never returned.
- **Disabling is never a way to skip a guarantee:** scrub paths and cleanups of a disabled plugin still run.

## Observability

- Start-up logs the compiled plugins, their versions, which are on, and the dependency order.
- `lewbox_plugin_info{plugin,version,enabled}`; every metric a plugin registers is prefixed `lewbox_<plugin>_` (slashes become underscores).
- Logs from a plugin carry `plugin=<name>`; jobs carry their plugin in their kind (`credits.checkpoint`).
- Enabling and disabling write an audit entry (`settings.updated`, area `plugins`) and, for projects, a `project.plugin_enabled` or `project.plugin_disabled` webhook event ([projects/settings](projects/settings.md#plugins)).

## Testing

- **Every build in CI** is tested twice: the default build, and a **core-only** build with no plugins, which must start, migrate and run the sandbox lifecycle tests (proving the core doesn't depend on any plugin).
- **Contract suites** for each provider kind (`agenttest`, `gittest`, `dnstest`, `storagetest`), which every provider's tests call ([foundation/testing](foundation/testing.md#provider-contract-suites)).
- **Enable and disable** tests per plugin: data kept, routes `409`, admission off, jobs cancelled and reconciled, scrub paths kept.

## Build plan

1. **M0:** `plugin/` API skeleton, registry, dependency resolution, `plugins/config.json` and the generator, enablement (instance and project), route gating and `plugin_disabled`, plugin migrations, `/v0/meta` and the plugins routes, the core-only CI build ([registry-and-lifecycle](plugins/registry-and-lifecycle.md), [building-and-enabling](plugins/building-and-enabling.md)). About a week on top of M0's other work; it pays for itself in cleaner boundaries.
2. **M1:** `limits`, `dns/libdns` (Cloudflare supported), `storage/s3` built as plugins with their contract suites.
3. **M3:** `agents/claude-code`, `agents/codex`, `git/github`, `telemetry`.
4. **M4:** dashboard slots and schema-driven settings forms; `lewbox plugins` CLI commands.
5. **Beta:** `credits`; `lewbox build --with` documented for third parties.
6. **Later:** `agents/opencode` and other agents; more DNS and storage providers as they're tested; out-of-process plugins; runtime UI plugins.

## Open questions

1. ~~**Instance enablement at run time.**~~ Decided (2026-10-29): configuration only. Predictable, reviewable, fits infrastructure as code; instance admins see it read-only. Projects still turn allowed plugins on and off for themselves at run time.
2. **The API package's path.** `plugin/` at the module root (proposed), or `pkg/plugin`? It must be outside `internal/`.
3. **First-party table names.** New plugins prefix their tables with their name; should existing first-party tables be renamed to match (`profile_limits` → `limits_profile_limits`), or keep their names and be declared owned (proposed: keep and declare)?
4. ~~**Which more DNS providers ship, and how.**~~ Decided (2026-10-29): one `dns/libdns` plugin; its providers are listed in `config.json`, and AerolVM needs none of them thanks to the DNS-01 relay ([plugins/dns-providers](plugins/dns-providers.md)).
5. **Should a project be able to disable a core-adjacent plugin** like `limits` for itself, or only the instance (proposed: the instance decides; `limits` is `all` by default so projects can't opt out of the limits their operator set)?
