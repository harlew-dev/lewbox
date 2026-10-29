# Persistence

> Status: Draft · Milestone: Alpha (M1); Firecracker when the fork supports it · Updated: 2026-10-29

A sandbox created with `persist: true` keeps its files when it stops: Lewbox snapshots its filesystem, uploads the snapshot to the cluster's registry (in the project's bucket), and removes it from the node. Starting it again creates a new AerolVM sandbox from the snapshot on whichever node has room. One that doesn't persist is deleted when it stops. This plan covers the semantics, the snapshot, offload and resume mechanics, the AerolVM bugs around them and how the fork fixes them, layer growth, which snapshots are kept, and Firecracker.

## Today

- **`persist`** (default `false`), fixed at create (`lewbox-legacy/docs/profiles.md` § Persistence and storage):
  - A persistent sandbox needs cloud storage on its cluster (`422 persistence_unsupported`).
  - Its `disk_gb` counts against `storage_gb` until it's deleted.
  - AerolVM's timers for it are `stop_*`.
  - A sandbox that doesn't persist gets `destroy_*` timers, and its stop is a delete.
- **Offload**, built in the gateway (`lewbox-gateway-legacy/internal/operations/sandboxes.go`, `docs/operations.md` § Snapshots and offload):
  1. Snapshot (a `docker commit` of the writable layer) as `<sbx>-<op>`, lowercased.
  2. Poll `GET /daytona/snapshots/{name}` on the node that took it until `active`.
  3. Destroy the sandbox.
  4. `DELETE /daytona/snapshots/{name}` on that node.

  Checked live on R2: a stop took 8 s and a resume 5 s. Lewagent measures about 65 s to stop and 17 s to resume.
- **Resume** creates an AerolVM sandbox with the snapshot's name as its image; AerolVM resolves it to `127.0.0.1:5000/cluster/<clu>/snapshots/<name>` in the node's registry.
- **The nested toolbox.** A snapshot's `docker commit` bakes `toolboxd` in as the entrypoint, so a resume ran `toolboxd toolboxd <cmd>`. The inner toolbox couldn't bind `:2280` and exited, and the image's command never ran; a desktop template lost its desktop (seen live 2026-10-22, still so in v0.6.8).

  The workaround: on each create and start, read `/proc/1/cmdline`, record the command (`sandboxes.command`), and send it as `container_command` on resume. Sandboxes offloaded before that get the command started as the session `lewbox-command` (`internal/operations/command.go`, commit 6e36f0a).
- **Snapshot images on the node** pulled by resumes were never removed, which left about 5 GB after a few test resumes. `lewbox-snapshot-cleanup.timer`, installed by `install.sh`, removes them once unused, after a 300 s grace.
- **Registry clean-up.** `clusters:clean-up-registries` (`lewbox-legacy/app/Actions/Clusters/CleanUpRegistry.php`), hourly, works directly in the bucket. It deletes snapshot repositories that aren't the latest of a live sandbox, then unlinked blobs, with a 6-hour grace.
- **Risks noted in `lewbox-legacy/docs/clusters.md` § Risks:**
  - layers stack up with every stop, towards overlayfs's ~127 limit, and deleted files keep using space in older layers;
  - state is files only: memory and processes are lost.
- **Firecracker:** AerolVM's snapshot API returns "not implemented". Upstream plans Firecracker snapshots with registry push and pull ([discussion #785](https://github.com/aerol-ai/microvm/discussions/785)).
- **Credentials in snapshots.** Setup cleanup runs before every stop Lewbox asks for, so snapshots hold no credentials. An AerolVM timer stop can't be cleaned (the sandbox is already stopped), and that was accepted as the bucket is the project's.

## Goals

- `persist: true` means a stop keeps every file on the sandbox's disk, and a start brings it back on any node of the cluster with the same id.
- Fix the AerolVM issues **in the fork** (entrypoint reset, snapshot status on `/v1`, layer squashing), and keep the workarounds only for fork versions without the fix.
- Layer count never reaches the overlay limit; space from deleted files comes back.
- Snapshots no longer needed are deleted promptly, and nothing still needed ever is.
- Firecracker sandboxes persist once the fork supports their snapshots.
- **No snapshot holds a credential Lewbox imported**, whatever stopped the sandbox ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)).

**Non-goals:** keeping memory or processes across a stop: memory can't be scrubbed of credentials, so it's never snapshotted ([profile-setup](profile-setup.md#memory-is-never-snapshotted)); moving a stopped sandbox to another cluster.

## Keep, change, drop

| | |
|---|---|
| **Keep** | `persist` semantics and defaults. Snapshot per stop, named `<sbx>-<op>`. Destroy only after the upload is `active`. Resume on any node. `storage_gb` counting reserved `disk_gb`. Cleanup before a stop |
| **Change** | Snapshot records in their own table (`sandbox_snapshots`), not a string on the sandbox. Snapshot status and deletion through the fork's `/v1` routes, not the Daytona facade per node. The entrypoint fixed in the fork, with `container_command` kept as a belt and braces. Layers flattened past a threshold. Every snapshot scrubbed of imported credentials on the node, its image config reset (no create-time `env`), and checked before upload; legacy accepted credentials in snapshots after timer stops. Old snapshots deleted by the stop or delete that supersedes them, with the bucket sweep as a backstop. The node's image clean-up moves from a bash timer into the [node agent](../clusters/node-agent.md), or into the fork |
| **Drop** | Starting `lewbox-command` as a session for nested toolboxes, once no snapshot from before command recording is left (migration from legacy only; [migration](../migration.md)) |

## Design

### Semantics

|  | `persist: false` (default) | `persist: true` |
|---|---|---|
| A stop (asked for, credits, budget, suspension) | Becomes a `delete` | Snapshot, upload, remove from node; `stopped` |
| Its timers in AerolVM | `destroy_if_idle_for`, `destroy_at_age` | `stop_if_idle_for`, `stop_at_age`; then Lewbox offloads it |
| A restart | Restarts in place; never deletes | Same |
| A cluster without cloud storage | Allowed | `422 persistence_unsupported` |
| Isolation `microvm` (Firecracker) | Allowed | `422 persistence_unsupported` (`reason: isolation`) until Firecracker snapshots are in the fork ([Firecracker](#firecracker)) |
| Isolation `container` or `gvisor` | Allowed | Allowed, on a cluster with storage |
| `storage_gb` limit | Not counted | `disk_gb` counts, running or stopped, until deleted. Checked on create only |
| Metered | Running only | Running only. Stopped storage isn't charged (later, maybe; [credits](../credits.md)) |

### Snapshot, upload, remove

The `stop` operation ([operations](operations.md#stop)), from `stopped`. Before it gets here, a sandbox that was still running has had its timers disarmed, its `on_stop` [hooks](hooks.md) run, its refreshed logins harvested and its setup cleaned up, in that order; one AerolVM's backstop stopped ran its hooks there ([hooks](hooks.md#when-lewbox-cant-act)). So the order of a stop is always: **hooks → harvest → in-sandbox cleanup → stop → node scrub (Lewbox's paths and the user's) → verify → snapshot → upload**.

1. **Snapshot.** `POST /v1/sandboxes/{runtime_id}/snapshot` with `name = lower("<sbx>-<op>")`. AerolVM answers a repeated snapshot of the same sandbox with the first, so a resumed stop is safe. The fork's snapshot call also:
   - **scrubs imported credentials, and the sandbox's own `scrub` paths** ([hooks](hooks.md#scrub)), from the stopped container's filesystem before committing, reads back the paths Lewbox harvests, and **checks** the committed layer before it pushes anything ([F8](#fork-changes); the design is in [profile-setup](profile-setup.md#credentials-never-enter-a-snapshot));
   - **resets the image config**: the entrypoint and command go back to the image's own, `toolboxd` is not baked in ([fork fix F1](#fork-changes)), and `Env` is the base image's, so the create-time `env` isn't copied into the registry ([F9](#fork-changes));
   - returns the snapshot's **layer count** and **size** ([F3](#fork-changes)).

   Lewbox checks the scrub report (every path accounted for, config reset) and adopts any harvested login newer than the profile's → `scrubbed`. A failed scrub or check means the snapshot is never uploaded: the stop retries, then fails with `scrub_failed`, leaving the sandbox stopped on its node ([profile-setup](profile-setup.md#when-a-scrub-fails)).
2. **Insert** a `sandbox_snapshots` row (`pending`, with the scrub report), then → `snapshotted`.
3. **Wait for upload.** Poll `GET /v1/snapshots/{name}` on the fork (any node; forwarded to the owner, [F2](#fork-changes)) until `active`. On legacy fork versions, fall back to `GET /daytona/snapshots/{name}` on each online node in turn. On `error`, keep waiting: AerolVM retries the push itself every `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL` (30 s). The operation's deadline bounds the wait; at the deadline, the stop fails *after* stopping, leaving the sandbox `stopped` on its node, as legacy does.
4. **Mark it** `uploaded`, with its size, → `uploaded`.
5. **Remove** the AerolVM sandbox, and the snapshot's local image on the node that took it. "Gone" counts as done → `removed`.
6. **Record** `latest_snapshot_id`, `Transition` → `stopped` with no runtime id or node. Mark the previous snapshot `superseded` ([which snapshots are kept](#which-snapshots-are-kept)).

### Resume

The `start` operation, for a sandbox whose latest snapshot is `uploaded`:

1. `POST /v0/sandboxes` with:
   - `image` set to the snapshot's name, which AerolVM resolves to the cluster registry on whichever node it places it;
   - `container_command` set to the recorded command;
   - the runtime of the sandbox's [isolation level](../clusters/runtimes.md#isolation-levels), so it resumes at the level it was created at, on a node that runs it; a snapshot is only resumed by the runtime that took it;
   - the size, the env and the lifecycle timers;
   - the tags.
2. Adopt-by-tag as a create does, so a resumed start doesn't make two.
3. Record the new `runtime_id` and node.
4. Ports get **new URLs** ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md#starts-and-stops)).

A sandbox that was stopped on its node without offload (no storage, or a stop that failed after stopping) is started in place instead.

### Container command

What the sandbox's image runs as its main process: a desktop, a code editor, a service.

- **On every create and start**, once running: read `/proc/1/cmdline` through the toolbox. Strip the leading `toolboxd` (and a nested second one). Record the rest as `container_command` the first time (never overwrite a recorded one with a nested reading).
- **On resume**, send it as `container_command`.
- **With fork fix F1**, a snapshot keeps the image's own entrypoint, so the command is right without it. We keep sending it anyway: it costs nothing, and it covers snapshots taken by older fork versions.
- **Nested toolbox found** (`toolboxd toolboxd …`): only possible for snapshots taken before command recording (legacy migration). Start the command as the session `lewbox-command`, as legacy does. Remove this path once [migration](../migration.md) has re-snapshotted every legacy sandbox.

Lewagent depends on this: its template's CMD starts the desktop, and it must come back after every start.

### Layers

Every snapshot is a `docker commit` on top of the last, so a sandbox stopped *n* times has *n* layers above its image. Overlayfs caps the stack at about 127, and a deleted file still takes space in the layer that wrote it.

- **Track** `layer_count` on each snapshot (from the fork, F3).
- **Flatten** when a stop's snapshot would have more than `LEWBOX_SNAPSHOT_MAX_LAYERS` (default 32) layers above the base image: the fork's snapshot call takes `squash: true` ([F4](#fork-changes)), which exports the container's merged filesystem into a single new layer on top of the base image, instead of committing another layer.

  This takes longer and uploads more (the whole changed filesystem, not just the last delta), so we do it rarely. The step is still `snapshotted`; the snapshot records `squashed: true`.
- **Before the fork has F4:** refuse nothing, but warn in logs and metrics at 64 layers, and at 100 run a squash through the toolbox as a fallback:
  1. stop the sandbox;
  2. export through `docker export` on the node, via the node agent ([clusters/node-agent](../clusters/node-agent.md));
  3. import as a one-layer image.

  This is a contingency; F4 is the plan.

### Which snapshots are kept

A stopped sandbox needs only its **latest** snapshot, which links all earlier layers it builds on. In the registry, each snapshot is its own repository linking those blobs, so deleting an older snapshot's *repository* is safe and the shared blobs stay.

| Snapshot | Kept? |
|---|---|
| Latest `uploaded` snapshot of a sandbox not deleted | Yes |
| A snapshot of a sandbox with an operation in flight | Yes, until it finishes (a stop records its snapshot only after upload) |
| An older snapshot, superseded by a newer upload | Deleted once superseded for `LEWBOX_SNAPSHOT_GRACE` (6 h) |
| A snapshot of a failed stop (`pending`, never uploaded) | Deleted after the grace |
| Every snapshot of a deleted sandbox | Deleted after the grace |

Deleting is a job, `snapshot.delete`, enqueued when a snapshot becomes superseded or its sandbox is deleted, and run after the grace (job `scheduled_at`). It deletes the snapshot's **repository** through the registry API. The registry listens only on each node's localhost, so this goes through the node agent, which forwards a small set of registry calls ([clusters/storage-and-registry](../clusters/storage-and-registry.md)). Blobs no longer linked are reclaimed by the periodic bucket sweep (legacy's pass 3), which stays as a backstop and also catches anything the jobs missed. That plan owns the sweep.

### Deleting

A sandbox's delete marks all its snapshots `deleting` and enqueues their `snapshot.delete` jobs (with the grace, so a mistaken delete within 6 h can be recovered by an instance admin; see [open questions](#open-questions)). Its row keeps `latest_snapshot_id` for the record.

### Node image clean-up

A resume pulls the snapshot's image onto the node, and AerolVM never removes it. Legacy ran a bash timer every 60 s. New: the fork removes the pulled snapshot image when the sandbox created from it is destroyed ([F5](#fork-changes)). Until then, the [node agent](../clusters/node-agent.md) does what the timer did: remove snapshot images (`127.0.0.1:5000/cluster/<clu>/snapshots/*`) that no container uses and that weren't tagged in the last 5 minutes.

### Credentials in snapshots

**None, by any route.** Legacy cleaned credentials before the stops it asked for and accepted that AerolVM's timer stops left them in the snapshot. The rebuild doesn't accept that: credentials Lewbox imports live on a memory-only mount, the stop's in-sandbox cleanup removes them when it can, the snapshot call scrubs them from the stopped container's filesystem on the node whatever stopped it, and the result is checked before anything is uploaded. The full design, the list of what's imported, the harvest of refreshed logins, failure handling and the fallback for older fork releases are in [profile-setup](profile-setup.md#credentials-never-enter-a-snapshot).

- The create-time `env` is passed to AerolVM as the container's `Env`, and `docker commit` would copy it into the snapshot's image config. The fork's snapshot resets the config to the base image's (F9), and a resume sends `env` again ([resume](#resume)).
- Git remotes never carry tokens, so clones in snapshots hold none ([git/tokens](../git/tokens.md)).
- Notify-mode timers (F11) make timer stops Lewbox's own stop operations, so the in-sandbox cleanup and the `on_stop` hooks run before them; the registered pre-stop script (F6) does the same when the node's backstop stops a sandbox itself. Both are on top of the node-side scrub, which is what the rule depends on.

### Firecracker

- AerolVM snapshots Firecracker microVMs as a disk image plus memory snapshot. Upstream is adding registry push and pull, and `install.sh` support ([#785](https://github.com/aerol-ai/microvm/discussions/785)).
- **Until it ships** in our fork, `persist: true` at isolation `microvm` is `422 persistence_unsupported` (`reason: isolation`), with a message saying Firecracker persistence isn't available yet. Ephemeral `microvm` sandboxes work, and on a cluster that also offers `gvisor`, persistent sandboxes can use that ([clusters/runtimes](../clusters/runtimes.md#firecracker-microvm)).
- **When it ships:** the same operation steps, with these differences:
  - The snapshot is the **disk only**, so the semantics match Docker and gVisor: files kept, memory lost. The scrub works on the stopped VM's `rootfs.ext4` from the host ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)), and `/run/lewbox` is a tmpfs inside the guest, so it's never on that disk.
  - Firecracker memory snapshots (which Lewbox doesn't use for sandboxes) restore only on the same CPU model, kernel and Firecracker version ([clusters/runtimes](../clusters/runtimes.md)). Disk-only snapshots don't have that constraint.
  - Snapshots stay with their runtime: a `microvm` sandbox's snapshot resumes only as a `microvm`. Docker's and gVisor's are both OCI layers, but a sandbox keeps its level, so they never cross either.
  - Firecracker disk snapshots don't stack layers; each is a whole `rootfs.ext4`, perhaps a sparse or compressed diff. Layer flattening doesn't apply; size does.
- **Never memory snapshots.** A memory snapshot would hold every credential in every process's memory and in `/run/lewbox`, and memory can't be scrubbed. So there's no "pause that keeps processes" (decided 2026-10-29; [profile-setup](profile-setup.md#memory-is-never-snapshotted)).

### Fork changes

Changes we'll make in the [AerolVM fork](../clusters/aerolvm-fork.md) for persistence, and send upstream:

| # | Change | Replaces |
|---|---|---|
| F1 | Snapshot commits with the image's own `ENTRYPOINT` and `CMD` (`docker commit --change`), and create drops a leading `toolboxd` from an image's entrypoint | The nested-toolbox bug; `container_command` becomes optional |
| F2 | Snapshot status and deletion on `/v1` (`GET`/`DELETE /v1/snapshots/{name}`), forwarded to the owning node like other per-sandbox calls | The Daytona facade, asked node by node |
| F3 | Snapshot responses report `layer_count`, `size_bytes` and `base_image` | Not knowing how deep a sandbox's layers are |
| F4 | `squash: true` on snapshot: one layer over the base image | Layers stacking to the overlay limit |
| F5 | Remove a pulled snapshot image when its sandbox is destroyed | The node clean-up timer |
| F6 | A registered pre-stop script that AerolVM runs before any stop it makes itself ([aerolvm-fork F49](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)): the sandbox's `on_stop` hooks and the in-sandbox cleanup ([hooks](hooks.md#when-lewbox-cant-act)) | Hooks skipped on timer stops |
| F11 | Notify-mode timers ([aerolvm-fork F50](../clusters/aerolvm-fork.md#api-clustering-and-lifecycle)): timers tell Lewbox, which runs the stop; AerolVM stops only as a backstop ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop)) | AerolVM's silent timer stops, which give hooks and the harvest no chance |
| F7 | Firecracker snapshot push, pull and resume (upstream's own work; we test it) | `persistence_unsupported` on Firecracker |
| F8 | Snapshot scrub: `scrub: {remove, read}` on the snapshot call (and `POST /v1/sandboxes/{id}/scrub` without one), working on the stopped container's filesystem from the host (overlay upper directory with whiteouts; `rootfs.ext4` for Firecracker), the harvest read, and a check of the committed layer before any push ([aerolvm-fork F46](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)) | Credentials left in snapshots by timer stops; trusting the in-sandbox cleanup alone |
| F9 | `reset_config`: commit with the base image's `Env`, `Entrypoint` and `Cmd` ([aerolvm-fork F47](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry); includes F1) | The create-time `env`, which may hold secrets, copied into the snapshot's image config |
| F10 | A `tmpfs` at `/run/lewbox` in every sandbox ([aerolvm-fork F48](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)) | Secrets on the container's disk at all |

## Data model

```sql
CREATE TYPE snapshot_status AS ENUM ('pending','uploaded','superseded','deleting','deleted','failed');

CREATE TABLE sandbox_snapshots (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  project_id    bigint NOT NULL REFERENCES projects,
  sandbox_id    bigint NOT NULL REFERENCES sandboxes,
  cluster_id    bigint NOT NULL REFERENCES clusters,
  operation_id  bigint NOT NULL REFERENCES operations,   -- the stop that took it
  name          text NOT NULL,                            -- lower(<sbx>-<op>)
  node_id       bigint REFERENCES nodes,                  -- the node that took it
  status        snapshot_status NOT NULL DEFAULT 'pending',
  layer_count   integer,
  size_bytes    bigint,
  squashed      boolean NOT NULL DEFAULT false,
  scrub         jsonb,                                    -- the fork's scrub report: each path absent|removed|whited_out, config_reset; checked before accepting
  scrub_method  text,                                     -- node (F8) | restart (fallback on older forks)
  uploaded_at   timestamptz,
  superseded_at timestamptz,
  deleted_at    timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (cluster_id, name)
);
CREATE INDEX ON sandbox_snapshots (sandbox_id, created_at DESC);
```

`sandboxes.latest_snapshot_id` points to the one a start resumes from. `sandboxes.container_command text[]` holds the command.

## API

No endpoints of its own. On the sandbox object:

- `persist` (boolean).
- **New, read-only:** `snapshot: {size_bytes, taken_at} | null`, the latest snapshot's size, so users see what a stopped sandbox holds. Proposed; it doesn't expose names or layers.

Errors: `422 persistence_unsupported` (carries `cluster`), `429 limit_exceeded` (`storage_gb`).

## Security

- Snapshots live in the project's own bucket, through the cluster's registry ([clusters/storage-and-registry](../clusters/storage-and-registry.md)). Lewbox's control plane never stores sandbox contents.
- **No imported credential is ever in a snapshot**, after any kind of stop: memory-only storage, in-sandbox cleanup, a filesystem scrub on the node, and a check before upload ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)). A snapshot that fails the check never leaves the node.
- The snapshot's image config carries the base image's `Env`, not the sandbox's create-time `env` (F9).
- Snapshot names include ids but no secrets.

## Observability

- **Metrics:**
  - `lewbox_snapshot_duration_seconds{phase=commit|upload|remove}`;
  - `lewbox_snapshot_size_bytes` (histogram);
  - `lewbox_snapshot_layers` (histogram);
  - `lewbox_snapshot_squashes_total`;
  - `lewbox_snapshots{status}`;
  - `lewbox_snapshot_deletes_total{outcome}`.
- `lewbox_snapshot_scrub_failures_total{reason}`; `lewbox_snapshot_scrub_method_total{method=node|restart}`.
- **Alerts:** any scrub failure; an upload waiting over 10 minutes; any sandbox at more than 64 layers; snapshot deletes failing repeatedly.
- **Activity:** the stop's operation event carries `snapshot.size_bytes` and `squashed`.

## Configuration

| Variable | Default | |
|---|---|---|
| `LEWBOX_SNAPSHOT_MAX_LAYERS` | `32` | Squash past this |
| `LEWBOX_SNAPSHOT_GRACE` | `6h` | Before deleting superseded or orphaned snapshots |
| `LEWBOX_SNAPSHOT_POLL_INTERVAL` | `2s` | Upload status polling |

AerolVM settings on nodes (written by the node agent): `SB_SNAPSHOT_PUSH_ENABLED`, `SB_MIRROR_PUSH_HOST`, `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL=30s`, `SB_SNAPSHOT_PUSH_MAX_IN_FLIGHT=2`, `SB_AUTO_IMPORT_CLUSTER_ID` lowercased ([clusters/storage-and-registry](../clusters/storage-and-registry.md)).

## Testing

- **Fake AerolVM:**
  - snapshots that live on the taking node until pushed;
  - `SetSnapshotPolls` (slow uploads);
  - `FailPush`;
  - layer counts;
  - squash;
  - a resume on another node.
- **Crash a stop** after each step and resume it: one snapshot, one destroy, the sandbox `stopped`.
- **Resume** with and without `container_command`, against both an F1 fork and a pre-F1 fork (nested toolbox in the fake), plus a legacy-shaped snapshot (nested, no command recorded): `lewbox-command` is started.
- **Retention:** a superseded snapshot is deleted after the grace and the latest never is; a stop in flight protects its snapshot.
- **Live (nightly):** stop and start a Docker and a gVisor sandbox 40 times; files intact, layers squashed at 32, start time stable.
- **Canaries:** canary credentials and a canary create-time `env` are never found in any layer or the image config of a pulled snapshot, after every kind of stop ([profile-setup](profile-setup.md#whats-tested)).
- **Fake AerolVM** scrub failures (a path left behind, config not reset, the check failing): the stop retries, then fails `scrub_failed`, nothing is uploaded, and the sandbox is stopped on its node.

## Build plan

1. `sandbox_snapshots` migration; snapshot handling in the `stop` and `start` handlers.
2. AerolVM client: snapshot, snapshot status (`/v1` with Daytona fallback), delete, squash, layer metadata; version-gated on the fork's capabilities ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox)).
3. `container_command` recording and resume; the nested-toolbox fallback.
4. `snapshot.delete` jobs and their grace; hook into stop and delete.
5. Fork work: F1, F2, F3, **F8, F9** for the alpha (F8 and F9 are P0), F10 for the alpha if it fits (else on-disk paths with F8's scrub); F6 and F11 for the alpha (P0 and P1, for [hooks](hooks.md)); F4 and F5 before beta; F7 with upstream.
6. Node agent: snapshot image clean-up until F5.

## Open questions

1. **Recoverable deletes.** Keep a deleted sandbox's latest snapshot for the grace period so an instance admin can undelete it? Proposed: yes for admins only, no API for it.
2. ~~**Pause with memory.**~~ Decided (2026-10-29): no. Memory can't be scrubbed of credentials, so it's never snapshotted ([profile-setup](profile-setup.md#memory-is-never-snapshotted)).
3. **Squash threshold.** 32 is a guess. Measure stop and start times against layer depth on a live cluster.
4. **Credits for stopped storage.** Charge credits for snapshot size while stopped ([credits](../credits.md))? That needs `size_bytes`, which F3 gives.
5. **Changing `persist`** after create: from `false` to `true` is cheap (it changes the timers and the storage check). Allow it?
