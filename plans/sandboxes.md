# Sandboxes

> Status: Draft · Milestone: Alpha (M1, sandbox keys M2) · Core · Updated: 2026-10-29

A sandbox is an isolated Linux machine that a profile creates on one of its project's clusters. It runs from a public image or a template, is reached through Lewbox's API, and keeps the same `sbx_` id through every stop and start. This plan covers a sandbox's life from the outside: creating it, starting, stopping, restarting and deleting it, what a stop keeps, what Lewbox puts in it before anyone uses it, the timers that stop it, how Lewbox notices what AerolVM did on its own, and the short-lived keys that let a browser into one. What happens *inside* a running sandbox (commands, sessions, files, ports) is [inside-a-sandbox](inside-a-sandbox.md).

## Parts

| Plan | What it covers | Milestone |
|---|---|---|
| [lifecycle](sandboxes/lifecycle.md) | Statuses and every transition, `status_sequence`, the create request and its fields, choosing a cluster and template, sizes and limits, waiting (`201`/`202`), start, stop, restart, delete, settings and metadata, listing and filtering, who may do what | M1 |
| [operations](sandboxes/operations.md) | Long-running work as `operations` rows run by Postgres jobs: kinds and steps, one at a time, resuming after a crash, idempotent steps, retries, capacity waits, timeouts, failure codes, what a failure leaves | M1 |
| [persistence](sandboxes/persistence.md) | `persist`: snapshot, offload, resume on any node, the image's command (kept by the fork), layer stacking and flattening, which snapshots are kept, Firecracker (later) | M1 |
| [profile-setup](sandboxes/profile-setup.md) | The setup manifest: agent credentials, Git token and identity, telemetry, the profile env file, SSH keys; applying it on create and start, cleaning it before a stop, re-applying it on change, the setup report | M1 (manifest), M3 (agent and Git steps) |
| [repositories](sandboxes/repositories.md) | Cloning repositories on create: fields, paths, tokens, the 5-minute budget, statuses | M3 |
| [timers-and-activity](sandboxes/timers-and-activity.md) | `idle_timeout`, `lifetime`, what counts as activity, touches; **Lewbox decides when to stop**: the fork's notify-mode timers, idle warnings, the re-check, the node's backstop; stop reasons | M1, M2 |
| [hooks](sandboxes/hooks.md) | `hooks.on_create`, `on_start`, `on_stop`: the sandbox's own commands at creation, every start and before every stop (timer stops included), with `stop_grace`; `scrub` paths removed on the node before any snapshot; the hooks report; templates' hooks | M2 |
| [reconciler](sandboxes/reconciler.md) | Comparing Lewbox's records with AerolVM's, on the leader: AerolVM's own stops, lost sandboxes, lost nodes, wrong nodes, orphans | M1 |
| [sandbox-keys](sandboxes/sandbox-keys.md) | `lbs_` keys: one sandbox, 15 minutes, scopes, minted by profile keys, admin keys and the dashboard | M2 |

## How the parts fit

```
            POST /v0/sandboxes
                   │  one transaction: key, cluster, template build, size,
                   │  limits, idempotency → sandbox row (creating) + create op
                   ▼
┌────────────┐  job: operation.run  ┌──────────────────────────────────────────────┐
│ operations │ ───────────────────▶ │ create: AerolVM create → running → command → │
│  (Postgres)│ ◀─ step, heartbeat ─ │ setup → repositories → ports → hooks → done  │
└────────────┘                      └──────────────────────────────────────────────┘
       ▲                                         │ status changes (status_sequence)
       │ start · stop · restart · delete ·       ▼
       │ apply_setup                    activity log · webhooks · metering runs
       │
┌──────┴──────┐  every 60 s on the leader: AerolVM stopped one on its timer?
│ reconciler  │  lost one with its node? on the wrong node? → report, and finish
└─────────────┘  the stop as an operation (offload, or delete)
```

- Every change to a sandbox's state goes through **one function** that records it (`sandboxes.Transition`): it checks the transition is allowed, bumps `status_sequence`, starts or ends a metering run, clears runtime state, writes the activity event and the webhook event, all in the transaction that changes the row. Legacy spread this over the gateway's reports, `RecordSandboxProgress` and model hooks (`lewbox-legacy/app/Actions/Sandboxes/RecordSandboxProgress.php`, `app/Models/Sandbox.php`).
- **Operations do the slow work.** Requests that start one answer at once (or wait for it, for a create). Operations are rows; running them is a job in Postgres, woken at once with `LISTEN/NOTIFY` ([foundation/jobs-and-scheduling](foundation/jobs-and-scheduling.md)), not found by polling every second.
- **Profile setup** and **repositories** are steps of create and start: best effort, reported on the sandbox, never failing it.
- **Timers** are counted by AerolVM on the node, which tells Lewbox when one runs out; **Lewbox decides** whether to stop and runs a normal stop, hooks first. If Lewbox doesn't act within a grace period, the node stops the sandbox itself, after running the same hooks ([timers-and-activity](sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop)). The **reconciler** notices what the node did, says why, and finishes the stop.
- **Hooks** run the sandbox's own commands at creation, every start and before every stop, and **scrub paths** are removed on the node before any snapshot ([hooks](sandboxes/hooks.md)).
- **Sandbox keys** let a browser work inside one sandbox without a profile key.

## What changes from legacy

| Legacy | New |
|---|---|
| Two processes: the app reserves the sandbox and records reports; the gateway claims operations over HTTP, runs them, and reports back through ~15 internal endpoints (`lewbox-gateway-legacy/internal/lewbox/client.go`) | One process. Reserving, running and recording are function calls in the same binary, inside transactions |
| Operations found by every gateway replica polling `GET operations` every second (`GATEWAY_OPERATION_POLL_INTERVAL`) | A job per operation, claimed with `SKIP LOCKED`, woken by `NOTIFY` the moment it's enqueued |
| Lease of 60 s renewed every 20 s by HTTP `PATCH operations/{op}` | The job system's heartbeat on the job row |
| Reconciler and session watcher run on every replica; duplicates refused by the app's preconditions | Run once, on the leader. Preconditions kept, as a defence, not the mechanism |
| Status changes raised webhooks from Eloquent model hooks (`saving`/`saved`) | One `Transition` function, which writes the event rows in the same transaction |
| Start and stop as `PATCH status`, and also as `POST …/operations` | One way: `POST /v0/sandboxes/{sbx}/operations` with `kind` ([lifecycle](sandboxes/lifecycle.md#api); open question) |
| Admin API couldn't create sandboxes; the dashboard reserved them through its own path (`ReserveSandbox::forProfile`) | One create. A profile key creates for its profile; an admin key or the dashboard names the `profile` |
| Workarounds for AerolVM in the gateway: `container_command`, exit-status traps, phrase-matched capacity errors | Fixed in [our fork](clusters/aerolvm-fork.md); Lewbox requires the fork release with the fixes and carries no workarounds ([D15](README.md#decisions)) |
| Snapshots never flattened; layers stack up to overlayfs's ~127 limit | Flattened when a sandbox's snapshot passes a layer threshold ([persistence](sandboxes/persistence.md#layers)) |

## Who can do what

| Actor | Reaches | Creates | Start, stop, restart, delete | Settings | Inside |
|---|---|---|---|---|---|
| **Profile key** (`lbp_`) | Its profile's sandboxes on the clusters the key reaches | For its profile, with `sandboxes.create` | With `sandboxes.start`, `.stop`, `.delete` | With `sandboxes.update` | With `sandboxes.exec`, `.ports`, … |
| **Sandbox key** (`lbs_`) | One sandbox | No | No | No | Within its scopes ([sandbox-keys](sandboxes/sandbox-keys.md)) |
| **Admin key** (`lba_`) | Every sandbox in its project | For a profile it names | Yes | Yes | Yes. Every action is logged with the admin key as its actor |
| **Dashboard user** (project member) | Every sandbox in the project, by role ([projects](projects.md)) | For a profile they pick | By role | By role | Through a sandbox key minted for them |
| **Lewbox itself** | Any | Sign-in helper sandboxes ([profiles/agent-sign-ins](profiles/agent-sign-ins.md)) | Stops for credits, budgets, suspensions; finishes AerolVM's stops | Never | Directly, without minting keys |

Lewbox itself acting on sandboxes is the biggest simplification: legacy minted sandbox keys for its own jobs and called its own public API through the PHP SDK (`ConnectToSandbox`). Now the control plane calls the toolbox client directly, and records `app` as the actor.

## Data at a glance

Detailed in each part. All tables have `project_id`, and every query is scoped by it.

| Table | Holds | Plan |
|---|---|---|
| `sandboxes` | Placement, size, timers, `persist`, `status`, `status_sequence`, runtime ids, snapshot, command, setup and repository reports | [lifecycle](sandboxes/lifecycle.md#data-model) |
| `operations` | Kind, status, step, step times, error, reason, actor | [operations](sandboxes/operations.md#data-model) |
| `sandbox_snapshots` | Each snapshot a stop took: name, layer count, size, status | [persistence](sandboxes/persistence.md#data-model) |
| `sandbox_keys` | Hashes of `lbs_` keys, their scopes and expiry | [sandbox-keys](sandboxes/sandbox-keys.md#data-model) |
| `sandbox_runs` | Metering intervals (written by `Transition`; used by [credits](credits.md)) | [credits/metering](credits/metering.md) |

Ports, sessions, SSH keys and activity have their own tables, in [inside-a-sandbox](inside-a-sandbox.md) and [activity-and-webhooks](activity-and-webhooks.md).

## Go packages

| Package | Holds |
|---|---|
| `internal/sandboxes` | The domain: create (reserve), transitions, settings, listing, the `Transition` function, idempotency hooks |
| `internal/operations` | The runner: one handler per kind, steps, retries, failure mapping |
| `internal/setup` | Building the manifest from a profile, and applying it through the toolbox |
| `internal/reconcile` | The reconciler (and the session watcher, which [inside-a-sandbox/sessions](inside-a-sandbox/sessions.md) describes) |
| `internal/aerolvm` | The client for our fork: sandboxes, snapshots, images, toolbox |
| `internal/httpapi/sandboxes` | Handlers for `/v0/sandboxes/…` |

Sandboxes are **core**. Plugins reach into them only through extension points ([plugins/extension-points](plugins/extension-points.md)): **admission** in the create and start transaction (`limits`, `credits`), **lifecycle** after status changes (`credits` metering), **setup contributors** for what profile setup writes and scrubs (agent plugins, `git/github`, `telemetry`), and **fields** they own on the create body (`git`). Hooks, operations, persistence and the reconciler have no plugin-specific code.

## Milestone

All of it is in the alpha. M1 builds lifecycle, operations, persistence, the setup *mechanism* (with the env file and SSH keys), timers and the reconciler, against the fake AerolVM and on a live cluster. M2 adds sandbox keys with the inside routes. M3 adds the agent, Git and telemetry steps of setup, and repositories.

## Open questions

1. **Start and stop as `PATCH status`, or only `POST …/operations`?** The public docs show both. Proposed: only operations, as one way is simpler for SDKs and docs ([lifecycle](sandboxes/lifecycle.md#open-questions)).
2. **Moving a sandbox to another cluster.** Lewagent asked for it later. A stopped, persisted sandbox is a snapshot in a cluster's registry; moving it means copying layers between buckets. Later.
3. ~~**Memory snapshots.**~~ Decided (2026-10-29): no; memory can't be scrubbed of credentials ([persistence](sandboxes/persistence.md#open-questions)).
4. **Upgrading a sandbox to a newer template build.** Sandboxes keep their build for life; Lewagent's computers never get template updates ([templates/builds](templates/builds.md#open-questions)).
