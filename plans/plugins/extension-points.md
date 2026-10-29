# Extension points

> Status: Draft · Milestone: Alpha (M0 for the registry and the generic points; each provider kind with its first plugin) · Updated: 2026-10-29

Every place a plugin can hook into the core, with its Go signature, when the core calls it, in what order, inside or outside which transaction, and what happens when it fails. A plugin declares all of them in `Register` ([registry-and-lifecycle](registry-and-lifecycle.md#the-interface)); nothing can be added later.

The rule behind every point: **the core keeps its guarantees whatever a plugin does.** A plugin can refuse a request it owns a check for, add work, add data and add UI. It can't make a sandbox skip its scrub, an operation skip its lock, or a request skip authentication.

## Summary

| Point | Declared with | Called | Transaction | A failure |
|---|---|---|---|---|
| [Routes](#routes) | `r.Routes(func(Router))` | On a request | The handler's own | Its error response |
| [OpenAPI fragment](#openapi-fragment) | `r.OpenAPI([]byte)` | At build time | — | Build fails |
| [Migrations and tables](#migrations-and-tables) | `r.Migrations(fs.FS)`, `r.Tables(...)` | Start-up, after core | Each migration's own | Start-up fails |
| [Jobs](#jobs-and-periodic-jobs) | `r.Job(Worker)`, `r.Periodic(...)` | By the queue | The job's own | Retried with backoff |
| [Permissions](#permissions) | `r.Permissions(...)` | Registry | — | Start-up fails on a clash |
| [Events](#activity-kinds-and-webhook-events) | `r.ActivityKinds(...)`, `r.WebhookEvents(...)` | Registry | — | Start-up fails on a clash |
| [Error codes](#error-codes) | `r.Errors(...)` | Registry | — | Start-up fails on a clash |
| [Id prefixes](#id-prefixes) | `r.IDPrefixes(...)` | Registry | — | Start-up fails on a clash |
| [Settings](#settings) | `r.Settings(scope, schema)` | On read and write | The write's | `422` on invalid |
| [Admission](#admission) | `r.Admission(Hook)` | Create, start, resize, timer changes | **Inside** the core's | Refuses the request, or `500` and roll back |
| [Setup contributors](#setup-contributors) | `r.Setup(Contributor)` | Every apply, partial apply, cleanup, scrub | None (builds a manifest) | That step `failed` in the report; never fails the operation |
| [Harvest](#harvest) | part of `Contributor` | Before cleanup and at scrub | Its own, under the credential's row lock | Kept stored value; logged |
| [Lifecycle handlers](#lifecycle-handlers) | `r.Lifecycle(Handler)` | After commit, as jobs | Its own | Retried; never undoes the change |
| [Sandbox fields](#sandbox-fields) | `r.SandboxField(Field)` | Create, `PATCH`, render | Validate: none. Store: **inside** the create's | `422` on invalid |
| [Providers](#providers) | `r.AgentProvider`, `r.GitProvider`, `r.DNSProvider`, `r.StorageProvider` | By the core frameworks | Per provider | Per provider |
| [CLI commands](#cli-commands) | `r.CLI(...)` (in the plugin's `cli` package) | By the CLI | — | Exit code |
| [UI](#ui) | `r.UI(fs.FS)` | By the dashboard | — | Build fails |
| [agentd drivers](#agentd-drivers) | `agentd.Register(Driver)` (in the plugin's `agentd` package) | Inside sandboxes | — | Turn `error` |
| [Readiness](#readiness) | `r.Readiness(name, func)` | `/ready` | None | Not ready |

## Rules for every point

- **Contexts and deadlines.** Every call gets a `context.Context` with a deadline: the request's, the job's, or the point's own budget (admission: 250 ms). A plugin must return when it's cancelled.
- **Panics are recovered** at every boundary and treated as errors (`500 internal_error` for requests, a retry for jobs, a `failed` step for setup), logged with the plugin's name and a stack.
- **Errors carry codes.** A refusal is a `*plugin.Error` with a code from the catalogue or the plugin's own registered codes ([api/errors](../api/errors.md)); any other error is a `500`, logged, and never shown to the caller beyond the request id.
- **Idempotency.** Anything called from a job, or that may be retried (lifecycle handlers, setup contributors, `Reconcile`), must be safe to run twice.
- **No I/O inside a transaction you don't own**, except through the transaction you're given. Admission hooks and sandbox fields' `Store` get the core's `pgx.Tx`; they may read and write rows in it, and must not make network calls, sleep or take advisory locks the core doesn't know about.
- **Scope.** Everything a plugin is called with says which project and profile it's for; a plugin off for that scope isn't called ([registry-and-lifecycle](registry-and-lifecycle.md#what-a-disabled-plugin-still-does)).

## Routes

```go
r.Routes(func(rt plugin.Router) {
    rt.Handle("GET /v0/projects/{prj}/profiles/{prf}/credit-buckets", p.listBuckets, plugin.Route{
        Principals: plugin.Session | plugin.AdminKey,
        Permission: "profiles.credits",
        Project:    plugin.FromPath("prj"),      // which project gates enablement
        RateLimit:  plugin.RateManagement,
        Idempotent: false,
        Milestone:  "beta",
    })
})
```

- Paths must be under `/v0/` (or `/hooks/<plugin>/` for inbound webhooks, or `/auth/<plugin>/` for browser redirect flows such as OAuth callbacks). A path another plugin or the core registered is a start-up failure.
- The core's middleware runs first, in the API's order ([api](../api.md#build-plan)): request id, auth, CSRF, rate limit, **plugin enablement**, permission, idempotency. A handler only ever sees an authenticated principal allowed to call it.
- Handlers use the core's `respond` helpers (envelope, errors, pagination, SSE), from `plugin/httpkit`, so every plugin's API looks like the core's.
- The route's metadata drives the route-conformance and principal-matrix tests exactly as for core routes ([api](../api.md#testing)).
- Where a plugin's resource belongs under a core resource (a profile's credit buckets), it uses the core's path shape. A plugin's own top-level resources go under `/v0/<plugin>/…` (e.g. `/v0/telemetry/providers`).

## OpenAPI fragment

```go
//go:embed openapi.yaml
var openapiYAML []byte
r.OpenAPI(openapiYAML)
```

Each plugin's `openapi.yaml` holds its paths, schemas and the fields it adds to core schemas (`x-lewbox-extends: Sandbox`). The generator merges the core's document with every compiled plugin's into one ([api/openapi-and-codegen](../api/openapi-and-codegen.md#plugin-fragments)), tagging each operation `x-lewbox-plugin: <name>`. Clashing paths or schema names fail the build.

## Migrations and tables

```go
//go:embed migrations/*.sql
var migrations embed.FS
r.Migrations(migrations)
r.Tables("credit_buckets", "credit_transactions", "credit_transaction_lines", "usage_hourly")
```

- Each plugin has its own migration sequence and its own goose version table (`goose_plugin_credits`), so plugins' numbering never collides ([foundation/database](../foundation/database.md#plugin-migrations)).
- Run at start-up after the core's, in plugin order, whether the plugin is on or off. The same expand-and-contract rules as core migrations.
- A plugin may reference core tables (foreign keys to `profiles`, `sandboxes`); **the core never references a plugin's table**, so the core-only build migrates.
- `Tables` declares what the plugin owns. A lint test parses its migrations and fails on any `CREATE`, `ALTER` or `DROP` of a table it doesn't own, and on any core migration touching a plugin's table. New plugins name their tables with their name as a prefix (`gitlab_…`); first-party tables keep their names and are declared ([plugins](../plugins.md#open-questions)).

## Jobs and periodic jobs

```go
type Worker interface {
    Kind() string                                     // "credits.checkpoint": must start with the plugin's name (slashes as dots: "git.github.")
    Work(ctx context.Context, h plugin.Host, job plugin.Job) error
}
r.Job(jobCheckpoint{})
r.Periodic("credits.checkpoint", time.Hour, jobCheckpoint{}, plugin.PerProject)
```

- Jobs run on the core's queue ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)), with its retries, leases, uniqueness and `LISTEN/NOTIFY` wake-ups. A plugin inserts them through `Host.Jobs()`, in a transaction or not, always tagged with its name and the project.
- Periodic jobs are scheduled by the leader. `PerProject` runs the job once per project where the plugin is on, with the project in its arguments; otherwise once for the instance.
- A job of a plugin that's off for its scope is discarded when it runs ([registry-and-lifecycle](registry-and-lifecycle.md#lifecycle-of-a-plugin)).
- Returning an error retries with the queue's backoff; returning `plugin.Snooze(d)` re-runs later without counting an attempt; `plugin.Cancel(err)` ends it for good.

## Permissions

```go
r.Permissions(plugin.Permission{
    Name: "profiles.credits", Title: "Manage credits", Description: "Credit buckets and transactions.",
    Area: plugin.Management, SandboxKeys: false, Roles: []string{"owner", "admin"},
})
```

- Added to the registry the API's permission checks, admin and profile key creation, sandbox key minting and the roles table use ([api/permissions](../api/permissions.md#the-registry)). `Roles` says which built-in roles include it. `SandboxKeys` says whether a sandbox key may hold it.
- A key made with "full access" gets every permission, plugins' included, including ones added by plugins compiled in later.
- Names are unique across core and plugins. New plugins use `<plugin>.` as the prefix (`gitlab.tokens`); first-party ones keep the names already in the plans (`profiles.credits`, `git.tokens`).
- A key holding a permission of a plugin that's off keeps it; it's just refused with `plugin_disabled`.

## Activity kinds and webhook events

```go
r.ActivityKinds(plugin.ActivityKind{Name: "credits.checkpoint", Group: "credits", Outcomes: ...})
r.WebhookEvents(plugin.WebhookEvent{Name: "profile.credits_exhausted", Schema: schemaJSON, Description: "…"})
```

- Activity kinds join the log's filters and groups ([activity-and-webhooks/activity-log](../activity-and-webhooks/activity-log.md)). Webhook events join the catalogue endpoints subscribe to ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)), with a JSON Schema for the payload that the catalogue publishes.
- Raised through `Host.Activity().Record(ctx, tx, …)` and `Host.Events().Raise(ctx, tx, …)`, **in the transaction of the change they describe** (the outbox rule, [foundation](../foundation.md#principles)).
- A plugin may only raise its own kinds and events.

## Error codes

```go
r.Errors(plugin.ErrorCode{Code: "credits_exhausted", Status: 402, Message: "…", Extra: []string{"profile"}})
```

Plugins' codes join the catalogue ([api/errors](../api/errors.md)) and the SDKs' error mapping. Codes are global and stable: a plugin can't redefine a core code, and a code once published is never reused.

## Id prefixes

```go
r.IDPrefixes("cbk", "txn")
```

A plugin with public ids ([foundation/ids-and-keys](../foundation/ids-and-keys.md#prefixes)) registers their prefixes and generates ids with `plugin.NewID("cbk")`, which refuses a prefix the plugin didn't register. Prefixes are unique across core and plugins, and are 2 to 4 lowercase letters.

## Settings

```go
r.Settings(plugin.InstanceSettings, instanceSchema) // from configuration: [plugins.credits.settings]
r.Settings(plugin.ProjectSettings,  projectSchema)  // GET/PATCH /v0/projects/{prj}/settings/credits
r.Settings(plugin.ProfileSettings,  profileSchema)  // GET/PATCH …/profiles/{prf}/settings/credits
```

- Each schema is JSON Schema (draft 2020-12) generated from a Go struct (`plugin.SchemaFor[T]()`), with UI hints: `x-lewbox-secret` (write-only, encrypted with the keyring), `x-lewbox-order`, `x-lewbox-help`, `x-lewbox-group`, `x-lewbox-depends-on`.
- Stored in `plugin_settings(scope, project_id, profile_id, plugin, settings jsonb, secrets_encrypted bytea, updated_at, updated_by)`.
- **Validated on every write** against the schema, and by the plugin's optional `Validate(ctx, scope, value) error` for checks a schema can't express (a bucket the credentials can reach). `422 validation_failed` with field errors.
- **Read through `Host.Settings()`**, typed: `plugin.Get[Rates](ctx, h.Settings(), scope)`. Defaults come from the schema.
- The dashboard renders forms from the schema, so a plugin needs no React for its settings ([dashboard-slots](dashboard-slots.md#settings-forms)).
- A plugin that needs richer settings (the GitHub App's manifest flow) exposes its own routes instead or as well.

## Admission

The one point inside the core's write transaction. Used by `limits` and `credits`.

```go
package admission

type Hook struct {
    Name   string
    After  []string                                  // other hooks that must run first ("limits")
    Kinds  []Kind                                    // which requests: Create, Start, Resize, Timers
    Check  func(ctx context.Context, tx pgx.Tx, req Request) error
}

type Request struct {
    Kind      Kind            // Create, Start, Resize (size change, Later), Timers (idle_timeout, lifetime)
    Project   plugin.ProjectRef
    Profile   plugin.ProfileRef   // row already locked FOR UPDATE by the core
    Cluster   plugin.ClusterRef
    Sandbox   SandboxProposal     // id (if it exists), cpu_millis, memory_mb, disk_gb, persist, isolation, template, timers
    Principal plugin.Principal
    Purpose   string              // "workload" | "sign_in": sign-in helpers are never limited or charged
}
```

- **When:** in the transaction that reserves a sandbox (create), inserts a `start` operation, or changes timers, after the core has locked the profile row, refused a suspended profile or project, resolved the cluster and the size, and before it inserts anything ([limits](../limits.md#enforcement-one-transaction)).
- **Order:** core checks, then hooks in plugin order, respecting `After`. The first refusal wins; later hooks aren't called.
- **Returning** `nil` admits; a `*plugin.Error` refuses (`429 limit_exceeded`, `402 credits_exhausted`) and the core rolls back; any other error or a panic rolls back with `500`.
- **May** read and write in `tx` (a credits reservation row). **Must not** make network calls or take long: the profile row is locked. The hook's budget is 250 ms; over it, the request fails `503 app_unavailable` and the overrun is logged as a plugin fault.
- A hook may also run on **resize** and **timer changes** (`PATCH /v0/sandboxes/{sbx}` with `idle_timeout`), where only `Kind` and the proposal differ.

## Setup contributors

How plugins put things into sandboxes: agents' logins, Git's tokens, telemetry's headers. The core's profile setup ([sandboxes/profile-setup](../sandboxes/profile-setup.md)) builds one manifest from every contributor, renders one script, applies it, and owns the scrub.

```go
package setup

type Contributor interface {
    Step() StepInfo // Name ("codex"), Label, Order, Requires (commands that must exist), Plugin
    // Build returns the step for this sandbox, or Skip when it doesn't apply. Called for every
    // full or partial apply; it reads the profile's current settings and credentials.
    Build(ctx context.Context, c Context) (Step, error)
    // Cleanup returns the commands that remove what Build wrote, given what the last report
    // recorded. Called even when the plugin is now off, and even when Build would return Skip.
    Cleanup(ctx context.Context, c Context, wrote Wrote) ([]Command, error)
    // ScrubPaths returns every path that may hold a secret this step wrote, given the last
    // report, for the node-side snapshot scrub. Called even when the plugin is off.
    ScrubPaths(ctx context.Context, c Context, wrote Wrote) ([]ScrubPath, error)
}

type Context struct {
    Project, Profile, Sandbox plugin.Ref
    Cluster   plugin.ClusterRef
    Isolation string               // container | gvisor | microvm
    Image     ImageFacts           // template id, whether it has node, the agent CLIs, a shell
    Reason    string               // create | start | setup_changed | refresh | node_back_online
    Host      plugin.Host
}

type Step struct {
    Configured bool
    Files      []File      // {Path, Content, Mode, Merge: JSON|None, CreateOnly, Secret}
    Env        map[string]string // goes into /run/lewbox/env; always secret
    Commands   []Command   // {Script, Timeout}
    Check      *Command
    Secrets    []string    // values to redact from every output
    Scrub      []ScrubPath // {Path, Harvest}: added to the manifest's scrub list
}
```

- **Order:** by `StepInfo.Order`, then plugin order. Core steps use fixed slots (`base` 0, `ssh` 900); agents 100–199, Git 200–299, telemetry 300–399; anything else 500–899.
- **Where secrets go:** a file marked `Secret` must be under `/run/lewbox/` (the memory-only mount) or be listed in `Scrub`; the core refuses a manifest otherwise, at `Build` time in tests and as a step failure in production. Symlinks from where a tool expects its file into `/run/lewbox` are written by the step (as Codex's `auth.json`).
- **Failure:** an error from `Build` or `Cleanup` is that step's `failed` status in the setup report, with the message. Setup never fails the operation ([sandboxes/profile-setup](../sandboxes/profile-setup.md#the-report)).
- **`Cleanup` and `ScrubPaths` are always called** for every step the last report says wrote something, enabled or not ([registry-and-lifecycle](registry-and-lifecycle.md#what-a-disabled-plugin-still-does)). The report records each step's plugin and paths, so the core can scrub the recorded paths even for a plugin removed from the build.
- **Partial applies** (`steps: ["git"]`) call only the named contributors' `Build` (a token refresh, one agent's credential change).
- **Re-applying on change:** a plugin whose settings or credentials change for a profile calls `Host.Sandboxes().ApplySetup(ctx, profile, steps...)`, which enqueues `apply_setup` (full) or a partial apply as the core decides ([sandboxes/profile-setup](../sandboxes/profile-setup.md#when-setup-runs)).

### Harvest

```go
type Harvester interface {
    // Harvest is given a path the step marked Harvest, read back from the sandbox before cleanup
    // (through the toolbox) or at the snapshot scrub (through the fork's scrub `read`). It adopts
    // the content if it's newer than what's stored. Called under the credential's row lock.
    Harvest(ctx context.Context, c Context, path string, content []byte) error
}
```

Optional, on the contributor. Today only `agents/codex` has one (ChatGPT refresh tokens rotate, [sandboxes/profile-setup](../sandboxes/profile-setup.md#harvest-first)). A failure keeps the stored value and is logged; it never blocks the stop.

## Lifecycle handlers

React to what happened, after it happened. Used by `credits` (runs, exhaustion), `git/github` (revoke tokens on delete), `telemetry` (nothing yet).

```go
package lifecycle

type Handler struct {
    Name   string // "credits.runs"
    On     []Kind
    Handle func(ctx context.Context, h plugin.Host, e Event) error
}

// Kinds:
// SandboxCreated, SandboxStarted, SandboxStopped, SandboxDeleted, SandboxFailed,
// OperationFinished, ProfileCreated, ProfileUpdated, ProfileSuspended, ProfileDeleted,
// ProjectCreated, ProjectDeleted, NodeOffline, NodeOnline, CredentialChanged

type Event struct {
    ID        string          // unique; handlers deduplicate by it
    Kind      Kind
    At        time.Time       // when the change committed
    Project, Profile plugin.Ref
    Sandbox   *plugin.SandboxRef  // with status_sequence, for ordering
    Operation *plugin.OperationRef
    Reason    string          // idle_timeout, credits_exhausted, …
}
```

- **Delivered as jobs.** The core inserts one `lifecycle.deliver` job per handler and event **in the transaction of the change** (the outbox rule), so a handler never misses a change and never sees one that rolled back.
- **At least once,** possibly out of order across sandboxes; in order per sandbox (the job for `status_sequence` n+1 waits for n's to finish, by the queue's uniqueness and a per-sandbox key).
- **A failure retries** with backoff; it can never undo the change. A handler that needs to act synchronously (refuse something) is an admission hook instead.
- Handlers aren't called for scopes where their plugin is off.

## Sandbox fields

A plugin can own a top-level field on sandbox create, `PATCH` and the sandbox object. The core's own fields (`git`, `hooks`, `scrub`) are built this way internally, so the point is exercised from the start; no first-party plugin needs one yet.

```go
type SandboxField struct {
    Name     string                     // "gpu_profile"; unique among core and plugin fields
    Schema   []byte                     // JSON Schema for the value
    OnCreate bool; OnPatch bool
    Validate func(ctx context.Context, p plugin.Principal, value json.RawMessage) (any, error) // no tx; may read
    Store    func(ctx context.Context, tx pgx.Tx, sbx plugin.SandboxRef, value any) error     // inside the create/patch tx
    Render   func(ctx context.Context, sbx plugin.SandboxRef) (any, error)                    // the field on the sandbox object
}
```

- Given while the plugin is off: `409 plugin_disabled`. Given for a plugin not compiled in: `422 validation_failed` (unknown field).
- `Validate` errors are field errors on the field's name. `Store` runs after the core's inserts in the same transaction; an error rolls back the create.
- The idempotency fingerprint of a create covers plugin fields too.

## Providers

Typed interfaces that core frameworks call; each has its own plan with the interface, the calls, the manifest, errors, contract tests and an "Adding a new …" checklist:

| Kind | Declared with | Core framework calling it | Plan |
|---|---|---|---|
| Agents | `r.AgentProvider(agent.Provider)` | Conversations, agentd, credentials, sign-ins, setup | [agent-providers](agent-providers.md) |
| Git hosts | `r.GitProvider(gitprovider.Provider)` | The sandbox's Git setting, token files, refresh, token API, repository lists, clones | [git-providers](git-providers.md) |
| DNS | `r.DNSProvider(dns.Provider)` | Node records, the DNS-01 relay, nightly reconcile, the connection test | [dns-providers](dns-providers.md) |
| Object storage | `r.StorageProvider(storage.Provider)` | Snapshots, template images, the node registry's config, GC, usage, the connection test | [storage-providers](storage-providers.md) |

A plugin may register more than one provider. `dns/libdns` does: one provider per libdns package listed in `config.json` ([dns-providers](dns-providers.md)). Git, storage and agent plugins register one each, so each can be turned off on its own.

## CLI commands

```go
package cli // plugins/credits/cli: imports only the API client and plugin/cli, never server code

func init() { plugincli.Register("credits", Commands) }

var Commands = []*plugincli.Command{
    {Use: "credits buckets list --profile PRF", Run: listBuckets},
    {Use: "credits transactions create --profile PRF --amount N --reference R", Run: createTransaction},
}
```

Commands live in a separate `cli` sub-package so the slim `lewbox-cli` build ([foundation/binary-and-commands](../foundation/binary-and-commands.md#one-binary-or-two)) can include them without server code; the generator imports every compiled plugin's `cli` package into it. They appear under `lewbox <plugin> …` ([sdks-and-cli/cli](../sdks-and-cli/cli.md#plugins)). A command calling a plugin that's off gets `plugin_disabled` and exits `1` with the message.

## UI

```go
//go:embed web/dist
var webDist embed.FS
r.UI(webDist)
```

The plugin's React code (`plugins/<name>/web/`) is built with the dashboard, not separately: the generator adds each plugin's `web/index.tsx` to the dashboard's plugin list, and its contributions mount into named slots (project settings, profile tabs, setup-page sections, sandbox tabs, the create dialog, cluster forms). Most plugins need none: their settings forms come from their schemas. Full detail: [dashboard-slots](dashboard-slots.md).

## agentd drivers

Agent plugins can contribute code that runs **inside sandboxes**, in `lewbox agentd`. It lives in the plugin's `agentd` sub-package, compiled only into the `lewbox-sandbox` build (`//go:build sandbox`), and registers a driver for agent behaviour a manifest can't describe (the Codex stand-in, Claude's background tasks). Agents that speak plain ACP need no driver: agentd's generic driver runs them from the spec the control plane sends with each prompt. Detail: [agent-providers](agent-providers.md#the-agentd-driver).

## Readiness

```go
r.Readiness("storage.s3", func(ctx context.Context) error { … })
```

Optional checks `/ready` runs ([foundation/observability](../foundation/observability.md)): a plugin that can't serve without something (a reachable external API it depends on for every request) can say so. Most plugins register none; a provider failing for one project must not make the whole instance unready.

## Testing

- `plugin/plugintest`: a harness that starts the core with fakes ([foundation/testing](../foundation/testing.md)) and a given set of plugins, with helpers to call each extension point directly (`AdmitCreate`, `BuildSetup`, `DeliverLifecycle`, `RenderSandbox`).
- The example plugin (`plugin/plugintest/example`) uses every point, and its tests are the documentation's examples.
- Per point, the core's own tests: admission order, `After`, budget overrun and rollback; setup contributors' secret-path rule, cleanup and scrub of a disabled plugin; lifecycle outbox delivery, deduplication and per-sandbox order; sandbox field validation, storage and fingerprinting.

## Open questions

1. **Admission budget.** 250 ms with the profile row locked: enough for `credits`' balance check (one indexed read and an insert)? Measure in M1 with `limits`.
2. **Lifecycle ordering across kinds.** Per-sandbox order is guaranteed; does any handler need per-profile order (credits' exhaustion across several sandboxes)? Proposed: no, credits recomputes from the ledger.
3. **Profile-level plugin settings.** Needed by any first-party plugin in the alpha? `telemetry` has its own routes. Proposed: build the scope, use it when a plugin needs it.
