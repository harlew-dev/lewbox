# Request to AerolVM: archive a sandbox to the registry on stop, and start it on any node with the same ID

**Written on 2026-11-13, against AerolVM v0.7.0.** A feature request for `aerol-ai/microvm`. It can be posted as an issue, or used as the spec if we build it ourselves. The last section says what Lewbox does until this exists.

## The problem

A stopped sandbox keeps its disk on the node that ran it, and only that node can start it again. On small nodes (ours have 70 GB of disk), stopped sandboxes fill the disk, and a stopped sandbox can't move to a node that has room.

We want a stop to **archive** the sandbox:
1. its disk goes to the cluster's registry (backed by S3 or R2);
2. the node frees the space;
3. a later `start` brings it back on **any** node, with the **same ID**, public URL, ports and environment.

That is what E2B users expect from `pause` and `connect`.

## Why it can't be done from outside AerolVM

All the pieces are in v0.7.0, but no API joins them:

| What exists | Same ID | Disk kept | Another node can take it |
|---|---|---|---|
| `POST /v1/sandboxes/{id}/stop` and `/start` | Yes | Yes, on the same node | No |
| E2B `pause` and `connect` (`pkg/api/e2b/handlers.go`, `pauseSandbox` calls `PauseSandbox`, which is a stop) | Yes | Yes, on the same node | No |
| `failover: {policy: "recreate"}` (`internal/cluster/failover_policy.go`, `dead_owner.go`) | Yes, plus URL, ports and env from the sealed copy | **No** | Yes, but only when the owner node dies |
| Snapshot plus push (`SB_SNAPSHOT_PUSH_ENABLED`, `internal/service/snapshot_push.go`), then `create({image: <snapshot>})` | **No, a new sandbox** | Yes | Yes |

A client can snapshot, push, delete and create from the snapshot, but the result is a new sandbox with a new ID, URL and ports, and without its environment. Failover's rebuild keeps the identity but starts from the original image. **The request is failover's rebuild, started by `start` instead of a dead node, from the sandbox's pushed disk instead of its original image.**

## The proposal

### Turning it on

- **A create option,** `stop_policy: "keep" | "archive"`. The default is `keep`, which is today's behavior.
- **A server default,** `SB_DEFAULT_STOP_POLICY`, and a create policy that can **require** `archive`, so an operator can enforce it on a cluster whose nodes have little disk.
- **Requirements:**
  - snapshot push is enabled with a cluster registry (`SB_SNAPSHOT_PUSH_ENABLED`, `SB_MIRROR_PUSH_HOST`);
  - the sandbox is in a cluster with at least one backup node for its sealed environment, as `recreate` already requires.

  Otherwise a create with `archive` is refused with `422`.

### Stop

On the owner node, for a sandbox with `stop_policy: archive`:

1. Stop it, as today. Timers (`stopIfIdleFor`, `stopAtAge`), E2B `pause` and auto-pause all take this path.
2. Snapshot the stopped disk under an internal name, `_archive/<sandbox id>/<generation>`, in the sandbox's team namespace.
   - It is not a user snapshot: it isn't listed in `GET /v1/snapshots` and doesn't count against snapshot limits.
   - Like any snapshot, it holds no `env` values.
3. Push it, with the existing push job and its retries.
4. Once the push is `active`, record in the shared placement record, through Raft:
   - `archive_ref`: the registry reference by digest;
   - the generation;
   - no owner node.

   The status becomes **`archived`**, and `statusReason` keeps why it stopped.
5. Remove the container and its local image. The node's disk and its reservation are freed.

**If the push fails,** the sandbox stays `stopped` on its node and can still be started there. `archive_state: pending | pushing | error` and `archive_error` show the progress, and the push job retries as it does for snapshots. Nothing is removed from the node until step 4 is committed.

### Start

`POST /v1/sandboxes/{id}/start` (and E2B `connect`) on an `archived` sandbox:

1. The leader places it like a failover rebuild:
   - choose a node that holds the sealed environment copy and has room;
   - the image is `archive_ref`;
   - the spec (resources, timers, mounts, ports) comes from the shared record.
2. The new owner pulls the archive and starts the sandbox with the **same ID**. It opens the sealed copy to restore `env`, and exposes the same ports again, so the public URL is unchanged.
3. The status becomes `running`. Running programs and memory start fresh, as after any stop.
4. **The previous archive is retired** only after the next archive of the same sandbox is `active`, or when the sandbox is deleted (`RetireManifest`, by digest).

A `start` that arrives while an archive is in flight cancels it if the local copy still exists, and starts the sandbox in place. Otherwise it waits for step 4 of the stop, then restores.

### Delete

Deleting an `archived` sandbox removes its record and retires its archive manifests. Blobs are the registry's own garbage collection, as today.

### What else this needs

- **Layer growth.** On Docker each snapshot is a commit on the last one. A sandbox archived many times nears overlayfs's limit of about 125 layers, and deleted files keep taking space. The archive snapshot needs a `squash` mode: one layer over the base image, used every N generations, or when the layer count passes a threshold.
- **Pulled images.** When a restored sandbox is archived or deleted again, the archive image it was pulled from must be removed from that node. Otherwise the disk problem moves rather than going away.
- **Size.** `size_bytes` and `layer_count` on the archive, in `GET /v1/sandboxes/{id}`, so operators and billing can see stored bytes.
- **Events.** Audit records `sandbox.archived` and `sandbox.restored`, with the node, generation, size and duration.
- **Firecracker.** Its disk-only snapshots already push, and any node can start them (the "Save a Firecracker sandbox" docs), so the same flow applies.

### API changes, in short

- `CreateSandboxRequest.stop_policy`: `keep` (default) or `archive`.
- Sandbox status `archived`; fields `archive_state`, `archive_error`, `archive {generation, size_bytes, layer_count, archived_at}`.
- `SB_DEFAULT_STOP_POLICY`, and a create-policy rule to require `archive`.
- E2B facade: with `archive`, `pause` archives and `connect` restores.
- No new routes. `stop` and `start` change meaning only for `archive` sandboxes.

## Tests that would show it works

- Archive, then start on a **different** node: same ID, same public URL, `env` restored, files intact. The first node's disk is back to its level before the sandbox existed.
- A push that fails, then succeeds on retry: the sandbox stays startable in place the whole time.
- `start` racing an archive at each step: one sandbox, no lost writes.
- The owner node dying mid-archive: the sandbox is either still on that node's disk (and follows the failover rules), or fully archived.
- 50 archive and start cycles: layers squashed, start time stable.
- Deleting an archived sandbox: its manifests are retired, and the registry's garbage collection frees the blobs.

## Until then: Lewbox

Lewbox does a weaker version itself, as an operator setting per region:
- **Stop:** on stop, Lewbox snapshots the sandbox, waits for the push, and deletes the sandbox.
- **Restore:** creates a **new** sandbox from that snapshot, with a **new ID**.

If AerolVM ships this, the region setting maps onto `stop_policy: archive` and restores keep their ID.
