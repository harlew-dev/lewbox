# Operations

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

Creating, starting, stopping, restarting and deleting a sandbox, re-applying its profile's setup, and building a template each take from seconds to many minutes, and must survive a restart of `lewbox serve` halfway through. Each is an **operation**: a row recording what was asked, how far it has got and how it ended, carried out by a job in Postgres. This plan covers the record, how operations are run, their steps, retries and timeouts, what a failure leaves, and how clients wait.

## Today

- **The record.** `operations` in the app: polymorphic `subject` (a sandbox or a template build), `kind`, `status`, `step`, `steps` (JSON timings), `attempts`, `claimed_by`, `lease_expires_at`, `error`, `error_code` and `finished_at`. Sources: `lewbox-legacy/database/migrations/2026_10_07_155038_create_operations_table.php` and `…_add_step_times_to_operations_table.php`.
- **Running.** Every gateway replica polls `GET /api/gateway/v1/operations` every second for operations that are pending or whose lease lapsed. It claims one with `PATCH operations/{op}` (`claimed_by`, 60 s lease), renews every 20 s, records each step by HTTP, and finishes it. Concurrency is 64 per replica. Sources: `lewbox-gateway-legacy/internal/operations/runner.go` and `docs/operations.md`.
- **Steps** are idempotent: a retried create adopts a sandbox tagged `lewbox_sandbox=<sbx>`; deletes treat "gone" as done. See `internal/operations/sandboxes.go`.
- **Retries** back off from 1 to 30 s on transient failures, until `GATEWAY_OPERATION_TIMEOUT` (30 minutes) from when the operation was asked for. Capacity refusals are retried for `GATEWAY_CAPACITY_WAIT` (2 minutes). A Dockerfile that fails is `build_failed` at once.
- **The app's own operations** come from three places:
  - the Admin API;
  - `sandboxes:meter` (credits and budgets);
  - a changed credential (`ApplyProfileSetup`).

  The app has no way to wake the gateway, hence the 1 s poll ("stands in for a best-effort wake-up call", `lewbox-legacy/docs/api.md`).

**What's wrong with it:**
- an HTTP round trip for every claim, step and renewal;
- every replica polling once a second;
- the app and gateway each owning half the state machine;
- the `apply_setup` and `build_template` kinds sharing a polymorphic table with nothing to keep one-at-a-time other than application code.

## Goals

- Operations survive crashes and deploys, and resume from their last step on any replica.
- New work starts within milliseconds of being asked for, with no polling.
- At most one operation in flight per sandbox, enforced by the database.
- Clients can wait for an operation without polling.
- Every failure ends with an `error` a person can read and an `error_code` a program can use, and leaves the sandbox in a state the docs describe.

## Keep, change, drop

| | |
|---|---|
| **Keep** | Kinds, steps and their names; status values; idempotent steps; adopt-by-tag; retry classes; the 2-minute capacity wait; the 30-minute deadline; the failure table; the snapshot name `<sbx>-<op>` |
| **Change** | Operations are run by Postgres jobs ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)), woken by `NOTIFY`. Leases are the job system's. Steps are written in a local transaction. One active operation per sandbox is a **unique partial index**. Cancelling a create stuck on capacity. Long-poll `?wait=` |
| **Drop** | `claimed_by` and `lease_expires_at` on the operation itself (the job carries them). The 1 s poll. The `operation_claimed` and `operation_not_claimed` codes |

## Design

### The record and the job

An operation is a **domain record**: it's what clients see and what the activity log refers to. Running it is a **job** in the job queue, with `kind = 'operation.run'` and args `{operation_id}`, unique by operation id. The two are written in the same transaction that asks for the operation, so either both exist or neither does. Enqueueing sends `NOTIFY jobs`, and an idle worker on any replica claims it at once.

The job system ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)) gives us:
- claiming with `FOR UPDATE SKIP LOCKED`;
- a heartbeat while the handler runs;
- the job made claimable again when the heartbeat lapses (a crashed replica);
- per-queue concurrency.

Operations run on their own queue, `operations`, so a flood of webhook deliveries can't hold them up.

When a job is picked up again after a crash, the handler reads the operation's `step` and resumes after it. **Every step is safe to repeat**, since the crash may have happened after a step's effect but before its record.

### Kinds and steps

| Kind | Subject | Sandbox status while running → after | Steps, in order |
|---|---|---|---|
| `create` | sandbox | `creating` → `running` | `created` → `running` → `setup_applied` → `repositories_cloned` → `ports_exposed` → `hooks_ran` |
| `start` | sandbox | `starting` → `running` | `created` (a resume) → `running` → `setup_applied` → `ports_exposed` → `hooks_ran` |
| `stop` | sandbox | `stopping` → `stopped` | `timers_disarmed` → `stop_hooks_ran` → `harvested` → `setup_cleaned` → `stopped` → `scrubbed` → `snapshotted` → `uploaded` → `removed` |
| `restart` | sandbox | `stopping` → `stopped` → `running` | `stop_hooks_ran` → `stopped` → `running` → `setup_applied` → `ports_exposed` → `hooks_ran` |
| `delete` | sandbox | `deleting` → `deleted` | `stop_hooks_ran` (a running sandbox) → `removed` → `snapshots_deleted` |
| `apply_setup` | sandbox | `running` → `running` | `setup_applied` |
| `build_template` | template build | (build `building` → `ready`) | `built` → `measured` |

New steps against legacy:
- `hooks_ran` and `stop_hooks_ran`, the sandbox's own [hooks](hooks.md), and `timers_disarmed`, so the fork's backstop can't stop a sandbox mid-stop ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop));
- `harvested`, `setup_cleaned` and `scrubbed`, so no snapshot holds an imported credential ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot));
- `uploaded` and `snapshots_deleted` (the snapshot's deletion moves out of the hourly registry sweep and into the delete, as far as the database is concerned; [persistence](persistence.md#deleting));
- `built` and `measured` on builds (legacy recorded none).

Steps are for progress; the API documents that new ones may appear.

#### `create`

1. **Find or make.** List the cluster's AerolVM sandboxes tagged `lewbox_sandbox=<sbx>` (and `lewbox_project`, `lewbox_profile`). Adopt one found; else `POST /v1/sandboxes` with:
   - the image (public or the build's registry reference);
   - `cpu`, `memory_mb` and `disk_gb`;
   - `env`, decrypted;
   - the lifecycle timers ([timers-and-activity](timers-and-activity.md));
   - the tags.

   Record `runtime_id` and the node (`owner_node_id` on the sandbox, F37) → `created`.
2. **Wait** until AerolVM reports it `started` with its toolbox ready (F4), then `Transition` it to `running` (opening the metering run) → `running`.
3. **Apply profile setup** ([profile-setup](profile-setup.md)) → `setup_applied`. Best effort: failures go in the setup report.
4. **Clone repositories** ([repositories](repositories.md)) → `repositories_cloned`. Best effort, within 5 minutes.
5. **Expose ports**: the template's and the create's, private ones behind their proxies ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md)) → `ports_exposed`. Best effort.
6. **Run hooks**: the template's and the sandbox's `on_create`, then `on_start`, in order; register the backstop's `pre_stop` script with AerolVM ([hooks](hooks.md#when-hooks-run)) → `hooks_ran`. Best effort, unless a hook is `required`: then the operation ends `failed` with `hook_failed`, and the sandbox stays `running`.
7. Finish: `succeeded`.

#### `start`

- **Stopped on its node** (no offload: a cluster without cloud storage, or a stop that failed after stopping):
  1. Reset the lifetime to count from now.
  2. Start it in AerolVM, and wait.
  3. `Transition` to `running` → `running`.
- **Offloaded:** create a new AerolVM sandbox from its latest snapshot ([persistence](persistence.md#resume)), tagged as a create is. The image's own command runs, as the fork keeps it (F10).
  1. Record the new `runtime_id` and node → `created`.
  2. Wait, then `Transition` → `running`.
- **Then, either way:**
  1. Apply setup → `setup_applied`.
  2. Re-expose persisted ports, restarting private proxies first, and record their new URLs. After an in-place start, close ports AerolVM kept that aren't persisted → `ports_exposed`.
  3. Run the `on_start` hooks, and register the backstop's `pre_stop` script again ([hooks](hooks.md#when-hooks-run)) → `hooks_ran`.

#### `stop`

The same steps whatever asked for the stop: a person or key, credits, a budget, a suspension, a node drain moving it, or the reconciler offloading a sandbox AerolVM stopped on a timer. No snapshot leaves the node holding a credential Lewbox imported ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)).

1. **Disarm the timers** on the node, if it's still running (`PUT …/lifecycle` with notify-mode timers off), so the fork's backstop can't fire while the stop runs its hooks ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop)) → `timers_disarmed`.
2. **Run the `on_stop` hooks**, if it's still running: the template's then the sandbox's, within `stop_grace` (shorter for credits, budgets and suspensions), with `LEWBOX_STOP_REASON` set; failures are reported, never fatal ([hooks](hooks.md#stops-are-bounded)) → `stop_hooks_ran`. Skipped when AerolVM already stopped it; its backstop ran them, and step 6 reads their results.
3. **Harvest**, if it's still running: read back the logins an agent may have refreshed in the sandbox (Codex's `auth.json`) through the toolbox, and adopt any newer than the profile's ([profile-setup](profile-setup.md#harvest-first)) → `harvested`. Skipped when AerolVM already stopped it; step 6 reads them from the node instead.
4. Run setup cleanup, best effort, if it's still running: every step's cleanup, and `/run/lewbox` emptied ([profile-setup](profile-setup.md#cleanup)) → `setup_cleaned`.
5. Stop it in AerolVM, unless already stopped → `stopped`.
6. **Scrub** the stopped container's filesystem on the node, with the paths from the sandbox's last setup report **and the sandbox's own `scrub` paths** ([hooks](hooks.md#scrub)), reading the harvest paths (and the backstop's hook results, if it stopped itself) first if steps 2–3 couldn't; check the report → `scrubbed`. With cloud storage this is part of the snapshot call (step 8); without, it's `POST /v1/sandboxes/{id}/scrub`. A failed scrub or check is retried; at the deadline the stop fails `scrub_failed` and nothing is uploaded.
7. **Without cloud storage:** `Transition` → `stopped` and finish. (Only a sandbox that doesn't persist could be on such a cluster, and its stop is a `delete`; this case is a sandbox whose cluster lost its storage, or a legacy one.)
8. Snapshot it as `<sbx>-<op>` (the fork lowercases the path, F11), with the scrub and `reset_config` ([persistence](persistence.md#snapshot-upload-remove)) → `snapshotted`.
9. Wait until the snapshot is uploaded (`active`) → `uploaded`. Waiting is unbounded within the deadline; a failed upload is retried by AerolVM.
10. Destroy the AerolVM sandbox, and remove the snapshot's local copy on the node that took it → `removed`.
11. Record the snapshot as the sandbox's latest ([persistence](persistence.md#data-model)); `Transition` → `stopped`, clearing `runtime_id` and `node_id`.

#### `restart`

On the sandbox's node, with no snapshot, no offload and no setup cleanup. It never deletes, even a sandbox that doesn't persist.

1. Run the `on_stop` hooks, with `LEWBOX_STOP_REASON=restart` → `stop_hooks_ran`.
2. Stop it in AerolVM. `Transition` → `stopped` (sessions lost, non-persisted ports forgotten) → `stopped`.
3. As an in-place start: lifetime from now, start, wait, `Transition` → `running` → `running` → `setup_applied` → `ports_exposed`, then the `on_start` hooks → `hooks_ran`. No scrub: a restart takes no snapshot.

A restart resumed at `stopped` only does the second half.

#### `delete`

1. If it's running and the delete didn't say `?hooks=false`: run the `on_stop` hooks, with `LEWBOX_STOP_REASON=delete` (the stop of a sandbox that doesn't persist is this delete, with the stop's reason) → `stop_hooks_ran`.
2. Destroy the AerolVM sandbox, and any other tagged with its id. "Gone" is done → `removed`.
3. Mark its snapshots for deletion ([persistence](persistence.md#deleting)) → `snapshots_deleted`.
4. `Transition` → `deleted`.

#### `apply_setup`

If the sandbox isn't `running` any more, finish (its start applies the setup). Otherwise apply → `setup_applied`.

#### `build_template`

See [templates/builds](../templates/builds.md#the-build-operation).

### One at a time

```sql
CREATE UNIQUE INDEX operations_one_active_per_sandbox
  ON operations (sandbox_id) WHERE status IN ('pending','running') AND sandbox_id IS NOT NULL;
```

Asking for an operation inserts the row inside the transaction that locks the sandbox. A conflict is `409 operation_in_progress`, carrying the operation in the way. The index makes it impossible to have two, even through a bug. Template builds have the same index on `template_build_id`.

Operations that **Lewbox** asks for skip a sandbox that's busy, and get retried by whoever asked:
- `apply_setup` on a credential change: the busy operation fetches setup fresh when it applies it;
- stops for credits or suspension: the next metering pass catches it;
- the reconciler's offload of an AerolVM stop: the next pass catches it.

### Retries and failure classes

Each step's errors are classified by `internal/operations.classify(err)`:

| Class | Examples | Handling |
|---|---|---|
| **Transient** | Node unreachable; `5xx`, `408` or `429` from AerolVM; no online node; incomplete cluster listing; toolbox warming up (`502`/`503`); Postgres serialization failure | Retry the step with backoff from 1 s, doubling to 30 s, with jitter. An unreachable node also refreshes the cluster's connection, so a node marked offline is dropped |
| **Capacity** | AerolVM refuses for room (`host capacity exceeded`, `target capacity exceeded…`, `no worker placement target…`) | Retry for `LEWBOX_CAPACITY_WAIT` (2 min) from the *first* refusal, then fail with `cluster_capacity_exceeded` |
| **Scrub** | A snapshot's credential scrub, or its check, failed or came back incomplete ([profile-setup](profile-setup.md#when-a-scrub-fails)) | Retry with backoff until the deadline; never upload. Then fail with `scrub_failed`, the sandbox stopped on its node |
| **Permanent** | A `4xx` from AerolVM (image can't be pulled, invalid request); cluster gone; plain-HTTP cluster in production; Dockerfile build failure | Fail at once: `operation_failed`, or `build_failed` for builds |
| **Deadline** | `LEWBOX_OPERATION_TIMEOUT` (30 min) since the operation was *asked for* passes, however many replicas worked on it | Fail with the last error, as `cluster_unavailable` when that error was transient, else `operation_failed` |

Capacity errors were recognised by phrase in legacy, which was fragile. **The fork** returns a machine-readable `code: "capacity_exceeded"` with a `reason` (F34, in the minimum release), and Lewbox never reads an error's wording ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#no-workarounds-in-lewbox)).

`attempts` counts handler runs, not step retries. A job that panics is recorded as a transient failure and retried; three panics in a row fail the operation with `internal_error` and an alert ([foundation/observability](../foundation/observability.md)).

### What a failure leaves

| Kind | Fails when | Sandbox left | Notes |
|---|---|---|---|
| `create` | Anywhere before `hooks_ran` | `failed` | Anything made in AerolVM is destroyed first. Delete it, then create another |
| `create`, `start`, `restart` | A `required` hook failed (`hook_failed`) | `running` | The hooks after it are skipped; the [hooks report](hooks.md#the-report) says which failed. Fix it in a terminal, or restart |
| `start` | Before running | `stopped`, as it was | A resumed AerolVM sandbox is destroyed; an in-place one is stopped again. Can be started again |
| `start` | No snapshot and gone from its node | `failed` | |
| `stop` | Before AerolVM stopped it | `failed` | It may still be running, unmetered; only a delete is allowed |
| `stop` | After stopping, before upload | `stopped` on its node | A start resumes it in place |
| `stop` | The scrub, or its check, failed (`scrub_failed`) | `stopped` on its node; no snapshot uploaded, the committed image deleted | A start resumes it in place, and setup writes the credentials again. An operator alert and a project notification; the reconciler doesn't retry the offload on its own |
| `restart` | Before stopping | `running` if AerolVM still has it started (metered again), `stopped` if stopped, else `failed` | |
| `restart` | After stopping | `stopped` | Startable |
| `delete` | Anywhere | `failed` | Deletable again |
| `apply_setup` | Never fails the operation | `running` | The setup report holds the errors |
| `build_template` | Anywhere | Build `failed`; the template's earlier ready build stays in use | |

Finishing a failed operation records the sandbox's state first, through `Transition`, then the operation. Clients reading the operation then find the sandbox already as it was left.

### Cancelling

**New.** A delete asked for while a `create` is retrying capacity (sandbox `creating`, no AerolVM sandbox yet) cancels it:
1. The create is marked `failed` with `cancelled`.
2. Its job's context is cancelled through `NOTIFY operation_cancel`.
3. The delete proceeds.

That stops a full cluster from making a sandbox undeletable for 2 minutes. Other operations can't be cancelled.

### Waiting

- `GET /v0/sandboxes/{sbx}/operations/{op}?wait=<seconds>` (up to 60) answers when the operation finishes or the wait runs out, whichever is first. The handler `LISTEN`s on `operation_finished` (payload: the operation id) through a shared listener per replica ([api/streaming](../api/streaming.md)), and re-reads the row. Clients loop on it, rather than polling every 1–5 s.
- The create's own wait uses the same mechanism.
- SDKs wait up to 10 minutes by default, long-polling.

### Lewbox's own operations

The `reason` column records why Lewbox asked:

| Reason | Asked by | Kind |
|---|---|---|
| `credits_exhausted`, `budget_reached` | Metering ([credits](../credits.md)) | `stop` (or `delete`) |
| `profile_suspended` | Suspending a profile ([profiles](../profiles.md)) | `stop` (or `delete`) |
| `idle_timeout`, `lifetime_reached` | Reconciler, after AerolVM's own stop | `stop` (offload) or `delete` |
| `node_lost` | Reconciler | none; a `failed` transition |
| `setup_changed` | A credential, Git or telemetry change | `apply_setup` |
| `node_back_online` | A node coming back | `apply_setup` |
| `admin` | An admin key or a dashboard user, recorded with the actor | any |

They appear in the activity log with `outcome: refused` for the first two groups, as legacy did, so a user can see why their sandbox stopped.

## Data model

```sql
CREATE TYPE operation_kind   AS ENUM ('create','start','stop','restart','delete','apply_setup','build_template');
CREATE TYPE operation_status AS ENUM ('pending','running','succeeded','failed');

CREATE TABLE operations (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id         text NOT NULL UNIQUE,              -- op_…
  project_id        bigint NOT NULL REFERENCES projects,
  sandbox_id        bigint REFERENCES sandboxes,
  template_build_id bigint REFERENCES template_builds,
  kind              operation_kind NOT NULL,
  status            operation_status NOT NULL DEFAULT 'pending',
  step              text,
  steps             jsonb NOT NULL DEFAULT '[]',       -- [{step, at}]
  attempts          integer NOT NULL DEFAULT 0,
  reason            text,
  error             text,
  error_code        text,
  requested_by_type text NOT NULL,                     -- profile_key | admin_key | user | app
  requested_by_id   bigint,
  deadline_at       timestamptz NOT NULL,
  started_at        timestamptz,
  finished_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CHECK ((sandbox_id IS NULL) <> (template_build_id IS NULL))
);
CREATE UNIQUE INDEX ON operations (sandbox_id)        WHERE status IN ('pending','running') AND sandbox_id IS NOT NULL;
CREATE UNIQUE INDEX ON operations (template_build_id) WHERE status IN ('pending','running') AND template_build_id IS NOT NULL;
CREATE INDEX ON operations (sandbox_id, created_at DESC);
```

Two nullable foreign keys with a `CHECK` replace the polymorphic `subject_type` and `subject_id`, so Postgres enforces the references.

### The operation object

| Field | Type | |
|---|---|---|
| `id` | string | `op_…` |
| `sandbox` | string or null | `sbx_…` |
| `template_build` | string or null | `bld_…` |
| `kind` | string | |
| `status` | string | |
| `step` | string or null | |
| `steps` | array | `[{step, at}]`. **New** in the API |
| `reason` | string or null | **New** in the API |
| `error`, `error_code` | string or null | |
| `requested_by` | object | `{type, id, name}`. **New** |
| `created_at`, `started_at`, `finished_at` | string or null | |

## API

| Endpoint | Does |
|---|---|
| `POST /v0/sandboxes/{sbx}/operations` | `start`, `stop`, `restart` ([lifecycle](lifecycle.md#start-stop-restart)) |
| `GET /v0/sandboxes/{sbx}/operations` | Its operations, newest first, paged |
| `GET /v0/sandboxes/{sbx}/operations/{op}` | One, with `?wait=` |
| `GET /v0/templates/{tpl}/builds/{bld}` | A build carries its operation ([templates/builds](../templates/builds.md)) |

## Security

- Handlers decrypt `env` and secrets only in memory, only for the call that needs them. Errors from AerolVM are redacted (the sandbox's env values and setup secrets) before they're stored in `error`.
- An admin's or Lewbox's operation on a profile's sandbox is visible to the profile, with `requested_by` showing a project actor without personal details (`{type: "admin_key"}`, or `user` with only a display name).

## Observability

- **Metrics:**
  - `lewbox_operations_total{kind,outcome,error_code}`;
  - `lewbox_operation_duration_seconds{kind}`;
  - `lewbox_operation_step_duration_seconds{kind,step}`;
  - `lewbox_operations_in_flight{kind}`;
  - `lewbox_operation_retries_total{kind,class}`;
  - `lewbox_operation_queue_latency_seconds`, from asked for to started.
- **Traces:** one span per operation, with a child span per step and per AerolVM call.
- **Logs:** step completion at `info`, retries at `warn` with the class, failures at `error`.
- **Webhooks:** `operation.started`, `operation.succeeded`, `operation.failed`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_OPERATION_TIMEOUT` | `30m` | Deadline from when it's asked for |
| `LEWBOX_CAPACITY_WAIT` | `2m` | |
| `LEWBOX_JOBS_CONCURRENCY` | `64` | Workers on the `operations` queue per replica |
| `LEWBOX_OPERATION_RETRY_MIN` / `_MAX` | `1s` / `30s` | |
| `LEWBOX_CLONE_TIMEOUT` | `5m` | [repositories](repositories.md) |
| `LEWBOX_AEROLVM_LONG_TIMEOUT` | `10m` | Create, start, stop, destroy, snapshot calls |

## Testing

- **Fake AerolVM** (ported from `lewbox-gateway-legacy/internal/aerolvm/aerolvmtest`), with fault injection:
  - `Fail`, `FailWith`;
  - `SetDown`;
  - `SetSnapshotPolls`;
  - `FailPush`;
  - capacity refusals;
  - "loses a create when the client disconnects".
- **Crash at every step:** for each kind, kill the handler after each step's effect but before its record, resume on another worker, and assert one AerolVM sandbox, one snapshot, and the right final state.
- **Deadline and capacity clocks** with a fake clock.
- **One at a time:** concurrent requests race on the unique index; exactly one wins.
- **Cancelling** a create stuck on capacity.
- **Failure table:** each row is a test.

## Build plan

1. Migration; `sqlc` queries; the operation object and handlers.
2. `internal/operations`:
   - the `Runner` registered as the `operation.run` job handler;
   - step bookkeeping (`advance(step)` in its own transaction);
   - `classify`;
   - backoff;
   - deadline.
3. Handlers per kind (`create.go`, `start.go`, `stop.go`, `restart.go`, `delete.go`, `apply_setup.go`), porting `lewbox-gateway-legacy/internal/operations/sandboxes.go` step for step.
4. Long-poll `?wait=` on the shared listener.
5. Cancelling creates on delete.
6. Tests above; then the live-cluster smoke test.

## Open questions

1. ~~**Operation history retention.**~~ Decided (2026-10-29): keep operations for the sandbox's life plus the activity retention; activity keeps a summary.
2. **Per-cluster concurrency.** Deferred (Beta): the alpha uses the job system's per-queue limits; per-cluster fairness is added if a burst on one cluster is seen to starve another.
3. ~~**Resuming mid-wait**~~ for a snapshot upload. Decided (2026-10-29): the fork's event stream reports snapshot pushes (F35, a P0 for the alpha), which the node agent forwards; a resumed stop waits for that event, and re-polls the snapshot only as the safety net.
