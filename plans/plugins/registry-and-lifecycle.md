# Registry and lifecycle

> Status: Draft · Milestone: Alpha (M0) · Updated: 2026-10-29

What a plugin is in Go, how the binary finds the plugins compiled into it, the order they're registered and started in, what they're given to work with, and what happens to each as it's turned on and off. The hooks a plugin can register are in [extension-points](extension-points.md); how a build chooses its plugins and how they're enabled is in [building-and-enabling](building-and-enabling.md).

## Today

Nothing to port: legacy has no plugins ([plugins](../plugins.md#today)). The closest precedents are outside Lewbox:

- **Caddy**: modules register themselves in `init()` with `caddy.RegisterModule`; a custom binary is built with `xcaddy build --with <module>`; configuration decides which modules are used. Its weakness for us: modules are found by string ids in JSON config, loosely typed.
- **CoreDNS**: `plugin.cfg` lists plugins in order; `go generate` writes the imports; the order in the file is the order requests pass through them.

We take both: CoreDNS's generated list and explicit order, Caddy's self-registration and custom builds, and typed Go interfaces in place of Caddy's untyped modules.

## Goals

- A plugin is one Go package with one exported value; nothing else to wire.
- The core never imports a plugin. Plugins import the core's `plugin/` API.
- Registration is deterministic: the same build always registers the same plugins in the same order.
- A broken plugin fails start-up loudly, never half-registers.
- Turning a plugin off and on at run time (project level) is safe.

## Design

### The interface

```go
package plugin // github.com/harlew-dev/lewbox/plugin

// A Plugin is registered once per process, at start-up.
type Plugin interface {
    Info() Info
    // Register declares everything the plugin adds. It's called once, in dependency
    // order, before the database is migrated or anything is served. It must not do I/O.
    Register(r Registrar) error
}

// Optional: a plugin with background work of its own (beyond jobs) or state to load.
type Starter interface {
    Start(ctx context.Context, h Host) error // after migrations, before serving; ctx ends at shutdown
    Stop(ctx context.Context) error          // at shutdown, with the shutdown timeout
}

// Optional: called when the plugin is turned on for a scope (the instance at start-up,
// or a project), to re-create schedules and state its disabling cancelled. Idempotent.
type Reconciler interface {
    Reconcile(ctx context.Context, h Host, scope Scope) error
}

type Info struct {
    Name        string   // "credits", "git/github", "agents/codex": stable, lowercase, [a-z0-9-/]
    Kind        Kind     // KindFeature, KindAgent, KindGit, KindDNS, KindStorage
    Title       string   // "Credits"
    Description string   // one sentence, shown on the Plugins page
    Version     string   // the binary's version for first-party plugins; the module's for others
    APIVersion  int      // the plugin API it was written against; must equal plugin.APIVersion
    Requires    []string // plugins that must be on wherever this one is
    Uses        []string // plugins used when on, not required
    Projects    ProjectMode // default: All, OptIn or OptOut ([building-and-enabling])
    Docs        string   // URL or path of its user docs
}

type Scope struct {
    Instance bool
    Project  *ProjectRef // nil for the instance
}
```

A plugin package exports one value and registers it:

```go
package credits // github.com/harlew-dev/lewbox/plugins/credits

func init() { plugin.Register(Plugin{}) }

type Plugin struct{ svc *service }

func (Plugin) Info() plugin.Info {
    return plugin.Info{
        Name: "credits", Kind: plugin.KindFeature, Title: "Credits",
        Description: "Compute units, balances and budgets for profiles.",
        APIVersion: plugin.APIVersion, Requires: []string{"limits"}, Projects: plugin.OptIn,
    }
}

func (p Plugin) Register(r plugin.Registrar) error {
    r.Migrations(migrations)               // embed.FS
    r.Tables("credit_buckets", "credit_transactions", "credit_transaction_lines", "usage_hourly")
    r.Permissions(permProfilesCredits)
    r.Admission(admission.Hook{Name: "credits", Check: p.admit, After: []string{"limits"}})
    r.Lifecycle(lifecycle.Handler{Name: "credits.runs", On: []lifecycle.Kind{lifecycle.SandboxStarted, lifecycle.SandboxStopped}, Handle: p.onTransition})
    r.Job(jobCheckpoint{}); r.Job(jobExhaust{})
    r.Periodic("credits.checkpoint", time.Hour, jobCheckpoint{})
    r.Routes(p.routes)
    r.OpenAPI(openapiYAML)
    r.Settings(plugin.ProjectSettings, ratesSchema)
    r.UI(webDist)
    return nil
}
```

### The registry

`plugin.Register` adds the value to a package-level list. The generated `plugins_gen.go` ([building-and-enabling](building-and-enabling.md#the-generator)) imports each plugin package for its `init`, and the order in `plugins/config.json` is recorded with the list, so start-up never depends on Go's package initialisation order.

At start-up, `internal/plugins.Load(cfg)`:

1. **Collect** the registered plugins. Two with one name: fail.
2. **Check versions.** `Info.APIVersion` must equal `plugin.APIVersion`; otherwise fail with "plugin X was written for plugin API 3; this build is 4". (A custom build fails earlier, at `lewbox build`.)
3. **Resolve dependencies.** Build the graph from `Requires` and `Uses`. A missing required plugin, or a cycle, fails start-up. The registration order is a topological sort, ties broken by the order in `config.json`. That order is used everywhere order matters: registration, migrations, admission hooks, setup steps (unless a hook says otherwise), lifecycle handlers.
4. **Read configuration** for each plugin ([building-and-enabling](building-and-enabling.md#configuration)): instance on or off, its projects mode, its instance settings validated against its schema. Every problem is collected and reported together, with the core's configuration errors ([foundation/configuration](../foundation/configuration.md)).
5. **Check enablement against dependencies.** An enabled plugin whose required plugin is off for the instance: fail, naming both.
6. **Register.** Call each plugin's `Register` with a `Registrar` that records what it declares, and checks as it goes: a route that clashes with another, a permission or event name already taken, a table name another owns, a job kind outside the plugin's namespace. A plugin's `Register` returning an error, or any clash, fails start-up. Nothing is applied until every plugin has registered, so a failure leaves nothing half-done.
7. **Freeze.** The registries (routes, permissions, events, errors, settings, admission hooks, setup contributors, providers, UI) become read-only for the life of the process.

Then the core's normal start-up continues: migrations (core first, then each plugin's, [foundation/database](../foundation/database.md#plugin-migrations)), `Start` for each plugin enabled on the instance, in order, then serving.

### What a plugin is given

`Register` gets a `Registrar` (declarations only, [extension-points](extension-points.md)). Handlers, jobs and `Start` get a `Host`, the plugin's view of the core:

```go
type Host interface {
    DB() DB                         // the pool; Tx(ctx, fn) with the core's retry and lint rules
    Jobs() Jobs                     // insert jobs (in a tx or not); kinds must be the plugin's
    Clock() clock.Clock
    Log() *slog.Logger              // with plugin=<name>
    Metrics() Metrics               // names prefixed lewbox_<plugin>_
    Secrets() Secrets               // encrypt and decrypt with the keyring
    HTTP() *http.Client             // outbound, with the SSRF guard and timeouts
    Settings() Settings             // this plugin's instance, project and profile settings
    Enabled(ctx context.Context, scope Scope) (bool, error)

    // Core services, through narrow interfaces, never the internal packages:
    Profiles() ProfilesService      // read profiles, their credentials (decrypted on request, audited)
    Sandboxes() SandboxesService    // read sandboxes; request operations (stop, apply_setup) as Lewbox
    Toolbox(sbx SandboxRef) Toolbox // commands and files inside a running sandbox, as Lewbox
    Activity() Activity             // record activity events of the plugin's registered kinds
    Events() Events                 // raise webhook events of the plugin's registered types
    Notifications() Notifications   // project notifications
}
```

The host's services are the same code the API uses, so a plugin can't skip a check the API makes: `Sandboxes().RequestStop` takes the operation lock and refuses `operation_in_progress` like any caller. Calls are attributed to the plugin in the activity log (`actor: {type: "app", name: "credits"}`).

### Lifecycle of a plugin

```
compiled in ──▶ registered ──▶ migrated ──▶ ┬─ instance off: idle (routes answer 409, nothing runs)
                                            └─ instance on: Start ──▶ serving
                                                      │
                                     per project:  on ⇄ off   (Reconcile on each turn-on)
                                                      │
                                      shutdown ──▶ Stop
```

- **Migrations run whether a plugin is on or off.** Its tables always exist, so turning it on never runs DDL, and turning it off never loses data.
- **Routes are always registered.** A middleware checks the plugin is on for the request's scope (the instance, and the project the path or principal names) and answers `409 plugin_disabled` otherwise. Routes that take no project (`/v0/meta`) aren't plugin routes.
- **Workers are always registered**, so a job of a plugin that was turned off doesn't fail as "unknown kind". A job whose plugin is off for its scope ends without running (recorded as `discarded: plugin_disabled`); turning the plugin back on calls `Reconcile`, which re-creates what's needed.
- **Periodic jobs** of a plugin off for the instance aren't scheduled. A periodic job that works per project skips projects where the plugin is off.

### Turning a plugin on or off for a project

`PATCH /v0/projects/{prj}/settings/plugins {"plugins": {"credits": true}}` ([projects/settings](../projects/settings.md#plugins)), for each plugin changed, in one transaction:

1. Lock the project's `project_plugins` rows.
2. Check: the plugin is on for the instance and the instance's projects mode lets projects choose; turning on, every `Requires` is on for the project (`422 plugin_dependency`, `requires: [...]`); turning off, no plugin that's on for the project requires it (`422 plugin_dependency`, `required_by: [...]`), and nothing in the project uses it if it's a driver (`409 plugin_in_use`, `used_by: [{type: "cluster", id: "clu_…"}]`).
3. Write the row, an audit entry (`settings.updated`, area `plugins`) and a `project.plugin_enabled` or `project.plugin_disabled` webhook event.
4. Off: cancel the plugin's pending jobs for the project (jobs carry `plugin` and `project_id` in their metadata).
5. On: enqueue `plugin.reconcile` for the plugin and project, which calls its `Reconcile`.

Commit; `NOTIFY plugin_changed` so every replica's enablement cache drops the entry. The cache is a map of `(plugin, project) → bool`, filled on first read, with no TTL: it's only ever invalidated by the notify (and fully on reconnect to Postgres).

### Turning a plugin on or off for the instance

Instance enablement is configuration only, never an API call or a dashboard switch ([building-and-enabling](building-and-enabling.md#configuration)), so it changes at a restart. Replicas roll one at a time, so for a while some replicas have the plugin on and some off; that's safe because:

- a plugin's tables exist on both;
- a request served by a replica with it off gets `409 plugin_disabled`, and a retry may succeed: acceptable during a rollout;
- jobs check enablement where they run.

Turning a plugin on for the instance calls its `Reconcile` for the instance, and for each project where it's on, once (a leader-only job after start-up, unique per plugin and build).

### What a disabled plugin still does

| | Off for the instance | Off for a project |
|---|---|---|
| Tables and rows | Kept | Kept |
| Routes | `409 plugin_disabled` (scope `instance`) | `409 plugin_disabled` (scope `project`) for that project |
| Fields it owns on core resources | `409 plugin_disabled` when given | Same, in that project |
| Admission hooks | Skipped | Skipped for that project's profiles |
| Setup step | Not contributed | Not contributed for that project's sandboxes |
| **Cleanup and scrub paths** | **Still contributed** for any sandbox whose last setup report says the plugin wrote something | **Still contributed** |
| Lifecycle handlers | Not called | Not called for that project |
| Jobs | Discarded when they run | Pending ones cancelled; others discarded when they run |
| Webhook events it registered | Never raised | Not raised for that project; endpoints subscribed keep their subscription |
| Providers (agents, Git, DNS, storage) | Not offered (not in provider lists) | Not offered to that project; existing references kept and readable |
| UI | Not mounted | Not mounted in that project |

Cleanup and scrub paths are kept because they're promises already made: a credential a plugin wrote must leave the sandbox and never reach a snapshot, whatever happens to the plugin afterwards ([sandboxes/profile-setup](../sandboxes/profile-setup.md#credentials-never-enter-a-snapshot)). The setup report records, per step, the plugin and paths it wrote, so this needs the plugin's code only to render its cleanup commands, which every compiled plugin has.

A plugin **removed from the build** while its data exists: start-up warns, naming it and its tables; its rows are left alone; any sandbox whose last report names it gets the report's recorded paths scrubbed (the paths are in the report, so no plugin code is needed). Its webhook subscriptions are kept, inert.

## Security

- `Register` can't do I/O (the `Registrar` has no host), so a plugin can't act before its configuration is validated.
- The `Host` is per plugin: its logger, metrics, settings, job namespace and activity actor carry its name, and a plugin can't register or raise another plugin's events or jobs.
- Decrypting profile credentials through `Host.Profiles()` is allowed only for the kinds the plugin owns (an agent provider reads its agent's credentials; nothing else does), and writes an audit entry the first time per process.

## Observability

- Start-up log: `plugins loaded` with each name, version, kind, enabled, and the resolved order.
- `lewbox_plugin_info{plugin,version,kind,enabled}`.
- `lewbox_plugin_disabled_refusals_total{plugin,scope}`; `lewbox_plugin_jobs_discarded_total{plugin}`.
- Audit: `plugin.enabled`, `plugin.disabled` (project), with the actor.

## Testing

- Registry: duplicate names, API version mismatch, missing and cyclic dependencies, clashing routes, permissions, events, tables and job kinds each fail start-up with a clear message.
- Order: the resolved order is stable for a given `config.json`, and dependencies always come first.
- Enable and disable for each first-party plugin: data kept, routes answer `409` in the right scope, admission skipped, jobs cancelled and then reconciled, setup step absent but cleanup and scrub present.
- The core-only build: no plugins at all, the lifecycle suite passes.
- A test plugin (`plugin/plugintest/example`) exercising every extension point, kept as the reference for authors.

## Build plan

1. `plugin/` package: `Plugin`, `Info`, `Registrar` (recording), `Host` façade, `APIVersion`.
2. `internal/plugins`: `Load`, dependency resolution, configuration, enablement cache with `NOTIFY` invalidation, the route-gating middleware, job discard, `plugin.reconcile`.
3. `project_plugins` table and the routes ([building-and-enabling](building-and-enabling.md#api)).
4. The example test plugin and the registry's tests.

## Open questions

1. ~~**`Uses` vs optional interfaces.**~~ Decided (2026-10-29): plugins find each other only through core extension points; `Uses` only affects order.
2. ~~**A plugin's own goroutines.**~~ Decided (2026-10-29): forbidden for first-party plugins; `Starter` only loads state, and all background work is jobs (visible, retried, leader-aware).
