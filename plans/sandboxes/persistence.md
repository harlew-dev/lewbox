# Persistence

> Status: Draft · Milestone: Alpha (M1, Docker); gVisor Beta; Firecracker Later · Updated: 2026-10-29

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
- Fix the AerolVM issues **in the fork** (entrypoint reset, snapshot status on `/v1`, image clean-up, layer squashing), and carry **no workarounds** in Lewbox: it requires the fork release with the fixes ([D15](../README.md#decisions)).
- Layer count never reaches the overlay limit; space from deleted files comes back.
- Snapshots no longer needed are deleted promptly, and nothing still needed ever is.
- Docker (`container`) in the alpha; gVisor in the beta; Firecracker once upstream ships its snapshots.
- **No snapshot holds a credential Lewbox imported**, whatever stopped the sandbox ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)).

**Non-goals:** keeping memory or processes across a stop: memory can't be scrubbed of credentials, so it's never snapshotted ([profile-setup](profile-setup.md#memory-is-never-snapshotted)); moving a stopped sandbox to another cluster.

## Keep, change, drop

| | |
|---|---|
| **Keep** | `persist` semantics and defaults. Snapshot per stop, named `<sbx>-<op>`. Destroy only after the upload is `active`. Resume on any node. `storage_gb` counting reserved `disk_gb`. Cleanup before a stop |
| **Change** | Snapshot records in their own table (`sandbox_snapshots`), not a string on the sandbox. Snapshot status and deletion through the fork's `/v1` routes, not the Daytona facade per node. The entrypoint fixed in the fork (F10), so Lewbox records and sends no command. Layers flattened past a threshold. Every snapshot scrubbed of imported credentials on the node, its image config reset (no create-time `env`), and checked before upload; legacy accepted credentials in snapshots after timer stops. Old snapshots deleted by the stop or delete that supersedes them, with the bucket sweep as a backstop. The node's image clean-up moves from a bash timer into the fork (F16) |
| **Drop** | Every workaround: recording `/proc/1/cmdline` and sending `container_command`; starting `lewbox-command` for nested toolboxes (the fork fixes legacy snapshots too, F10); asking each node's Daytona facade for snapshot status; the node's image clean-up timer; a Lewbox-side squash; scrubbing by restarting in place ([D15](../README.md#decisions)) |

## Design

### Semantics

|  | `persist: false` (default) | `persist: true` |
|---|---|---|
| A stop (asked for, credits, budget, suspension) | Becomes a `delete` | Snapshot, upload, remove from node; `stopped` |
| Its timers in AerolVM | `destroy_if_idle_for`, `destroy_at_age` | `stop_if_idle_for`, `stop_at_age`; then Lewbox offloads it |
| A restart | Restarts in place; never deletes | Same |
| A cluster without cloud storage | Allowed | `422 persistence_unsupported` |
| Isolation `container` (the alpha's only level) | Allowed | Allowed, on a cluster with storage |
| Isolation `gvisor` (beta) | Allowed | Allowed, on a cluster with storage |
| Isolation `microvm` (later) | Allowed | Allowed once offered: `microvm` is offered only with upstream's Firecracker snapshots ([Firecracker](#firecracker)) |
| `storage_gb` limit | Not counted | `disk_gb` counts, running or stopped, until deleted. Checked on create only |
| Metered | Running only | Running only. Stopped storage isn't charged (later, maybe; [credits](../credits.md)) |

### Snapshot, upload, remove

The `stop` operation ([operations](operations.md#stop)), from `stopped`. Before it gets here, a sandbox that was still running has had its timers disarmed, its `on_stop` [hooks](hooks.md) run, its refreshed logins harvested and its setup cleaned up, in that order; one AerolVM's backstop stopped ran its hooks there ([hooks](hooks.md#when-lewbox-cant-act)). So the order of a stop is always: **hooks → harvest → in-sandbox cleanup → stop → node scrub (Lewbox's paths and the user's) → verify → snapshot → upload**.

1. **Snapshot.** `POST /v1/sandboxes/{runtime_id}/snapshot` with `name = lower("<sbx>-<op>")`. AerolVM answers a repeated snapshot of the same sandbox with the first, so a resumed stop is safe. The fork's snapshot call also:
   - **scrubs imported credentials, and the sandbox's own `scrub` paths** ([hooks](hooks.md#scrub)), from the stopped container's filesystem before committing, reads back the paths Lewbox harvests, and **checks** the committed layer before it pushes anything ([F46](#fork-changes); the design is in [profile-setup](profile-setup.md#credentials-never-enter-a-snapshot));
   - **resets the image config**: the entrypoint and command go back to the image's own, `toolboxd` is not baked in ([fork fix F10](#fork-changes)), and `Env` is the base image's, so the create-time `env` isn't copied into the registry ([F47](#fork-changes));
   - returns the snapshot's **layer count** and **size** ([F14](#fork-changes)).

   Lewbox checks the scrub report (every path accounted for, config reset) and adopts any harvested login newer than the profile's → `scrubbed`. A failed scrub or check means the snapshot is never uploaded: the stop retries, then fails with `scrub_failed`, leaving the sandbox stopped on its node ([profile-setup](profile-setup.md#when-a-scrub-fails)).
2. **Insert** a `sandbox_snapshots` row (`pending`, with the scrub report), then → `snapshotted`.
3. **Wait for upload.** Poll `GET /v1/snapshots/{name}` on the fork (any node; forwarded to the owner, [F12](#fork-changes)) until `active`. On `error`, keep waiting: AerolVM retries the push itself every `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL` (30 s). The operation's deadline bounds the wait; at the deadline, the stop fails *after* stopping, leaving the sandbox `stopped` on its node, as legacy does.
4. **Mark it** `uploaded`, with its size, → `uploaded`.
5. **Remove** the AerolVM sandbox, and the snapshot's local image on the node that took it. "Gone" counts as done → `removed`.
6. **Record** `latest_snapshot_id`, `Transition` → `stopped` with no runtime id or node. Mark the previous snapshot `superseded` ([which snapshots are kept](#which-snapshots-are-kept)).

### Resume

The `start` operation, for a sandbox whose latest snapshot is `uploaded`:

1. `POST /v0/sandboxes` with:
   - `image` set to the snapshot's name, which AerolVM resolves to the cluster registry on whichever node it places it;
   - the runtime of the sandbox's [isolation level](../clusters/runtimes.md#isolation-levels), so it resumes at the level it was created at, on a node that runs it; a snapshot is only resumed by the runtime that took it;
   - the size, the env and the lifecycle timers;
   - the tags.
2. Adopt-by-tag as a create does, so a resumed start doesn't make two.
3. Record the new `runtime_id` and node.
4. Ports get **new URLs** ([inside-a-sandbox/ports](../inside-a-sandbox/ports.md#starts-and-stops)).

A sandbox that was stopped on its node without offload (no storage, or a stop that failed after stopping) is started in place instead.

### The image's command

What the sandbox's image runs as its main process: a desktop, a code editor, a service. Lewagent depends on it: its template's CMD starts the desktop, and it must come back after every start.

- **The fork keeps it right** (F10, in the minimum release): a snapshot commits with the image's own `ENTRYPOINT` and `CMD`, and a create drops a leading `toolboxd` from an image's entrypoint, so a resume runs the image's command, including from legacy snapshots whose toolbox nested.
- **Lewbox records nothing and sends nothing.** Legacy read `/proc/1/cmdline`, recorded it, sent it as `container_command` on resume, and started a `lewbox-command` session for nested legacy snapshots; all of that goes ([D15](../README.md#decisions)).

### Layers

Every snapshot is a `docker commit` on top of the last, so a sandbox stopped *n* times has *n* layers above its image. Overlayfs caps the stack at about 127, and a deleted file still takes space in the layer that wrote it.

- **Track** `layer_count` on each snapshot (from the fork, F14).
- **Flatten** when a stop's snapshot would have more than `LEWBOX_SNAPSHOT_MAX_LAYERS` (default 32) layers above the base image: the fork's snapshot call takes `squash: true` ([F14](#fork-changes)), which exports the container's merged filesystem into a single new layer on top of the base image, instead of committing another layer.

  This takes longer and uploads more (the whole changed filesystem, not just the last delta), so we do it rarely. The step is still `snapshotted`; the snapshot records `squashed: true`.
- **If F14 misses the alpha** (it's P1), there's no Lewbox-side squash: the sandbox's `warnings` and the dashboard say, past 100 layers, that it will soon stop being able to persist ([clusters/storage-and-registry](../clusters/storage-and-registry.md#flattening)).

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

A resume pulls the snapshot's image onto the node, and AerolVM never removes it. Legacy ran a bash timer every 60 s. New: the fork removes the pulled snapshot image when the sandbox created from it is destroyed ([F16](#fork-changes), in the minimum release). Neither Lewbox nor the node agent cleans images up.

### Credentials in snapshots

**None, by any route.** Legacy cleaned credentials before the stops it asked for and accepted that AerolVM's timer stops left them in the snapshot. The rebuild doesn't accept that: credentials Lewbox imports live on a memory-only mount, the stop's in-sandbox cleanup removes them when it can, the snapshot call scrubs them from the stopped container's filesystem on the node whatever stopped it, and the result is checked before anything is uploaded. The full design, the list of what's imported, the harvest of refreshed logins and failure handling are in [profile-setup](profile-setup.md#credentials-never-enter-a-snapshot).

- The create-time `env` is passed to AerolVM as the container's `Env`, and `docker commit` would copy it into the snapshot's image config. The fork's snapshot resets the config to the base image's (F47), and a resume sends `env` again ([resume](#resume)).
- Git remotes never carry tokens, so clones in snapshots hold none ([git/tokens](../git/tokens.md)).
- Notify-mode timers (F50) make timer stops Lewbox's own stop operations, so the in-sandbox cleanup and the `on_stop` hooks run before them; the registered pre-stop script (F49) does the same when the node's backstop stops a sandbox itself. Both are on top of the node-side scrub, which is what the rule depends on.

### Firecracker

**Later.** `microvm` isn't offered until upstream ships Firecracker with snapshots and registry push and pull ([#785](https://github.com/aerol-ai/microvm/discussions/785)) and a fork release includes it ([clusters/runtimes](../clusters/runtimes.md#firecracker-microvm-later)). Then it persists like the other levels, with these differences:
  - The snapshot is the **disk only**, so the semantics match Docker and gVisor: files kept, memory lost. The scrub works on the stopped VM's `rootfs.ext4` from the host ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)), and `/run/lewbox` is a tmpfs inside the guest, so it's never on that disk.
  - Firecracker memory snapshots (which Lewbox doesn't use for sandboxes) restore only on the same CPU model, kernel and Firecracker version ([clusters/runtimes](../clusters/runtimes.md)). Disk-only snapshots don't have that constraint.
  - Snapshots stay with their runtime: a `microvm` sandbox's snapshot resumes only as a `microvm`. Docker's and gVisor's are both OCI layers, but a sandbox keeps its level, so they never cross either.
  - Firecracker disk snapshots don't stack layers; each is a whole `rootfs.ext4`, perhaps a sparse or compressed diff. Layer flattening doesn't apply; size does.
- **Never memory snapshots.** A memory snapshot would hold every credential in every process's memory and in `/run/lewbox`, and memory can't be scrubbed. So there's no "pause that keeps processes" (decided 2026-10-29; [profile-setup](profile-setup.md#memory-is-never-snapshotted)).

### Fork changes

Changes we'll make in the [AerolVM fork](../clusters/aerolvm-fork.md) for persistence, and send upstream:

| # | Change | Replaces |
|---|---|---|
| F10 | Snapshot commits with the image's own `ENTRYPOINT` and `CMD` (`docker commit --change`), and create drops a leading `toolboxd` from an image's entrypoint | The nested-toolbox bug, and legacy's `container_command` and `lewbox-command` |
| F12 | Snapshot status and deletion on `/v1` (`GET`/`DELETE /v1/snapshots/{name}`), forwarded to the owning node like other per-sandbox calls | The Daytona facade, asked node by node |
| F14 | `squash: true` on snapshot: one layer over the base image; snapshot responses report `layer_count`, `size_bytes` and `base_image` | Layers stacking to the overlay limit, unseen |
| F16 | Remove a pulled snapshot image when its sandbox is destroyed | The node clean-up timer |
| F49 | A registered pre-stop script that AerolVM runs before any stop it makes itself ([aerolvm-fork F49](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)): the sandbox's `on_stop` hooks and the in-sandbox cleanup ([hooks](hooks.md#when-lewbox-cant-act)) | Hooks skipped on timer stops |
| F50 | Notify-mode timers ([aerolvm-fork F50](../clusters/aerolvm-fork.md#api-clustering-and-lifecycle)): timers tell Lewbox, which runs the stop; AerolVM stops only as a backstop ([timers-and-activity](timers-and-activity.md#lewbox-decides-when-to-stop)) | AerolVM's silent timer stops, which give hooks and the harvest no chance |
| F26 | Firecracker snapshot push, pull and resume (upstream's own work; we test it) | Firecracker not offered |
| F46 | Snapshot scrub: `scrub: {remove, read}` on the snapshot call (and `POST /v1/sandboxes/{id}/scrub` without one), working on the stopped container's filesystem from the host (overlay upper directory with whiteouts; `rootfs.ext4` for Firecracker), the harvest read, and a check of the committed layer before any push ([aerolvm-fork F46](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)) | Credentials left in snapshots by timer stops; trusting the in-sandbox cleanup alone |
| F47 | `reset_config`: commit with the base image's `Env`, `Entrypoint` and `Cmd` ([aerolvm-fork F47](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry); includes F10) | The create-time `env`, which may hold secrets, copied into the snapshot's image config |
| F48 | A `tmpfs` at `/run/lewbox` in every sandbox ([aerolvm-fork F48](../clusters/aerolvm-fork.md#snapshots-resume-and-the-registry)) | Secrets on the container's disk at all |

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
  uploaded_at   timestamptz,
  superseded_at timestamptz,
  deleted_at    timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (cluster_id, name)
);
CREATE INDEX ON sandbox_snapshots (sandbox_id, created_at DESC);
```

`sandboxes.latest_snapshot_id` points to the one a start resumes from.

## API

No endpoints of its own. On the sandbox object:

- `persist` (boolean).
- **New, read-only:** `snapshot: {size_bytes, taken_at} | null`, the latest snapshot's size, so users see what a stopped sandbox holds. Proposed; it doesn't expose names or layers.

Errors: `422 persistence_unsupported` (carries `cluster`), `429 limit_exceeded` (`storage_gb`).

## Security

- Snapshots live in the project's own bucket, through the cluster's registry ([clusters/storage-and-registry](../clusters/storage-and-registry.md)). Lewbox's control plane never stores sandbox contents.
- **No imported credential is ever in a snapshot**, after any kind of stop: memory-only storage, in-sandbox cleanup, a filesystem scrub on the node, and a check before upload ([profile-setup](profile-setup.md#credentials-never-enter-a-snapshot)). A snapshot that fails the check never leaves the node.
- The snapshot's image config carries the base image's `Env`, not the sandbox's create-time `env` (F47).
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
| `LEWBOX_OPERATION_WAIT_INTERVAL` | `2s` | Upload status polling |

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
- **Resume** runs the image's command, including from a legacy-shaped snapshot (nested `toolboxd` in its entrypoint), as the fork (F10) does; Lewbox sends no command.
- **Retention:** a superseded snapshot is deleted after the grace and the latest never is; a stop in flight protects its snapshot.
- **Live (nightly):** stop and start a Docker sandbox 40 times (and a gVisor one from the beta); files intact, layers squashed at 32, start time stable.
- **Canaries:** canary credentials and a canary create-time `env` are never found in any layer or the image config of a pulled snapshot, after every kind of stop ([profile-setup](profile-setup.md#whats-tested)).
- **Fake AerolVM** scrub failures (a path left behind, config not reset, the check failing): the stop retries, then fails `scrub_failed`, nothing is uploaded, and the sandbox is stopped on its node.

## Build plan

1. `sandbox_snapshots` migration; snapshot handling in the `stop` and `start` handlers.
2. AerolVM client: snapshot (with scrub and `reset_config`), snapshot status and delete on `/v1`, squash, layer metadata; against the minimum fork release, no fallbacks ([clusters/aerolvm-fork](../clusters/aerolvm-fork.md#versions-in-lewbox)).
3. `snapshot.delete` jobs and their grace; hook into stop and delete.
4. Fork work for the alpha (P0): F10 entrypoint (legacy snapshots too), F12 snapshots on `/v1`, F16 image clean-up, F46 scrub, F47 `reset_config`, F48 `/run/lewbox`, F49 pre-stop, F50 notify-mode timers. P1: F14 squash with layer counts. Beta: gVisor's F21. Later: F26 with upstream.

## Open questions

1. ~~**Recoverable deletes.**~~ Decided (2026-10-29): keep a deleted sandbox's latest snapshot for the grace period so an instance admin can undelete it; admins only, no API for it.
2. ~~**Pause with memory.**~~ Decided (2026-10-29): no. Memory can't be scrubbed of credentials, so it's never snapshotted ([profile-setup](profile-setup.md#memory-is-never-snapshotted)).
3. **Squash threshold.** Spike (M1): measure stop and start times against layer depth on a live Docker cluster; the knee of the curve sets `LEWBOX_SNAPSHOT_MAX_LAYERS` (32 until then).
4. **Credits for stopped storage.** Deferred (Beta, with the `credits` plugin): charge for snapshot size while stopped, using `size_bytes` from the fork.
5. **Changing `persist`** after create. Deferred (Beta): allow `false` → `true` only (it changes the timers and the storage check); `true` → `false` stays refused.
