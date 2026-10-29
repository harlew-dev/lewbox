# Storage and the registry

> Status: Draft · Milestone: Alpha (M1) · Core registry and GC; object storage through storage provider plugins (`storage/s3` by default) · Updated: 2026-10-29

A cluster can keep images in a bucket the project provides: Cloudflare R2, Amazon S3, or any S3-compatible store. Every node runs a small OCI registry on `127.0.0.1:5000`, backed by that bucket, so an image pushed on one node can be pulled on any other. Two features depend on it:

- **Persistence:** a stopped sandbox is snapshotted and offloaded to the registry, and resumed on any node.
- **Templates:** each template is built once per cluster and pulled by every node.

Lewbox keeps the bucket tidy itself, as nothing in AerolVM or the registry does.

## Today

- **The credential:** per cluster, optional: `provider` (`r2` or `s3`), `account_id` (R2), `region` (S3), `bucket`, `access_key_id`, `secret_access_key`. Tested before saving by writing, reading and deleting a small file (`Cluster\StorageConnectionTestController`, 10 a minute) (`lewbox-legacy/docs/clusters.md` § *Registry*).
- **The registry on each node:** CNCF Distribution v3.1.2 (`REGISTRY_VERSION`, SHA-256 pinned per arch in the install script), installed as `/usr/local/bin/lewbox-registry`.
  - Its config is `/etc/lewbox-registry/config.yml`, rendered by the app as JSON, with a random `http.secret`.
  - It runs as user `lewbox-registry` with `MemoryMax=512M` and `GOMEMLIMIT=400MiB`, sized for 100 MiB R2 chunks, under `ProtectSystem=strict`.
  - It has no auth: it listens on localhost only, its bucket is the cluster's, and root on the node can read the key anyway.
  - Blob downloads redirect to presigned bucket URLs, so nodes need outbound HTTPS to the bucket.
- **Storage settings:**

| Setting | R2 | S3 |
|---|---|---|
| `region` | `auto` | the credential's |
| `regionendpoint` | `https://<account>.r2.cloudflarestorage.com` | unset |
| `forcepathstyle` | `true` (required with `regionendpoint` in v3) | unset |
| `chunksize` | `104857600`, because R2 refuses multipart parts of unequal size | default |
| `storage.delete.enabled` | `true` | `true` |

- **AerolVM settings:**
  - `SB_SNAPSHOT_PUSH_ENABLED=true`;
  - `SB_MIRROR_PUSH_HOST=127.0.0.1:5000`, `SB_IMAGE_DISTRIBUTION_AOCR_HOST=127.0.0.1:5000` (it defaults to `aocr.aerol.ai`, and the PAT is sent to every host listed: [F18](aerolvm-fork.md#snapshots-resume-and-the-registry));
  - `SB_AUTO_IMPORT_CLUSTER_ID=<clu id, lowercased>` (F11), `SB_AUTO_IMPORT_CLUSTER_PAT_PATH`;
  - `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL=30s` (AerolVM's 5 minutes held stops; a bare number is ignored: F17);
  - `SB_SNAPSHOT_PUSH_MAX_IN_FLIGHT=2`;
  - `SB_AUTO_IMPORT_ENABLED` off.
- **The layout in the bucket:**
  - snapshots at `cluster/<id>/snapshots/<sbx>-<op>` (one repository per stop);
  - templates at `cluster/<id>/templates/<template>/<bld>:<bld>` (one per build);
  - blobs shared under `docker/registry/v2/blobs/sha256/…`.
- **Garbage collection:**
  - `clusters:clean-up-registries` runs hourly, straight on the bucket, as Distribution's `garbage-collect` can't run while any registry is pushing.
  - Three passes: snapshot repositories that aren't a live sandbox's latest; template builds nothing can use; then unlinked blobs.
  - A 6-hour grace period; sandboxes with operations in progress are kept.
  - One narrow race is documented: a push reusing an unlinked blob just as it's deleted.
- **Problems found live:**
  - Offloading 512 MB of new data took ~65 s to stop (29 snapshot, 35 upload) and 17 to resume, limited by single-core gzip at ~26 MB/s (F15).
  - Snapshot images pulled for a resume stayed on nodes (~5 GB after a few resumes) until a cleanup timer was added (F16).
  - Layers stack up with every stop, to Docker's ~125-layer cap (F14).
  - Never checked: resuming on another node; plain HTTP to `127.0.0.1:5000` under containerd; sandboxes not reaching the host's registry.
- **Without storage:** no persistence (`422 persistence_unsupported`), no templates (public images only). Without a registry, AerolVM's image janitor would delete a node-local build after a day.

## Goals

- Keep the design: a registry per node on the project's own bucket, no shared registry service, no single point of failure.
- Support any S3-compatible store, not just R2 and S3.
- Make offload faster and snapshots smaller over time, through fork fixes (F14, F15, F16).
- Keep the bucket clean with no races that lose data, and show the project what they're storing.
- Make storage optional but obvious: a cluster without it says what it can't do.

### Non-goals

- Storing anything else in the bucket: no files, artifacts or volumes. Sandboxes store their own files.
- Lewbox-hosted storage for self-hosted instances. On lewbox.com, instance-provided storage is open question 1.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | The per-node Distribution registry, its config and limits, the bucket layout, presigned downloads, no registry auth on localhost, the connection test, GC on the bucket in three passes with a grace period |
| **Change** | Installed by the install script → part of the node spec, so registry upgrades and config changes reach existing nodes ([node-agent](node-agent.md#desired-state)). `r2`/`s3` → also `s3_compatible` with an endpoint (MinIO, Backblaze B2, Wasabi, Ceph, Hetzner Object Storage). GC from a PHP command → a Go job on the leader. GC race → closed with a mark phase recorded in Postgres ([below](#garbage-collection)) |
| **Add** | Flattening snapshots (F14), compression settings (F15), usage per profile and template, a storage overview on the cluster page, a bucket-wide size check before deleting |
| **Drop** | The node-side snapshot-image cleanup timer: the fork removes those images itself (F16, P0), so neither a script nor the node agent does it |

## Design

### The bucket

Object storage is behind a **storage provider** extension point ([plugins/storage-providers](../plugins/storage-providers.md)). Core owns `cluster_storage`, the registry wiring, offload and GC; a driver plugin at `plugins/storage/<name>/` supplies the client (put, get, list, delete, multipart, presign), the credential fields as a JSON Schema, the connection test's provider-specific checks, and the registry storage settings it hands to nodes. The default build ships **`storage/s3`**, which covers AWS S3, Cloudflare R2 and any S3-compatible store (MinIO, Backblaze B2, Wasabi) through `provider` presets. A future `storage/gcs` or `storage/azure` would be another plugin, and needs a matching registry storage driver on nodes. Turning a storage plugin off refuses new persistent sandboxes and template builds on its clusters (`409 plugin_disabled`); existing snapshots stay where they are.

`cluster_storage` (one row per cluster, optional):

| Column | What |
|---|---|
| `provider` | The storage provider plugin: `s3` (`storage/s3`) |
| `preset` | For `s3`: `aws`, `r2` or `compatible` (was legacy's `r2`/`s3`, plus `s3_compatible`) |
| `endpoint` | For `compatible` (and derived for `r2` from `account_id`) |
| `region` | S3 and compatible stores; `auto` for R2 |
| `bucket` | The bucket's name |
| `prefix` | Optional, so clusters can share a bucket (default: none; the layout already namespaces by cluster id) |
| `credentials` | Encrypted: `access_key_id`, `secret_access_key` |
| `force_path_style`, `chunk_size` | Derived per provider; overridable for odd stores |
| `tested_at` | |

- **The test before saving** (dashboard and Admin API): write, read, list and delete `lewbox-connection-test/<random>`; a multipart upload of two equal parts (catches R2's rule and stores that refuse multipart); a presigned GET. It reports which step failed, and why.
- **Changing storage on a cluster with persisted sandboxes** is refused (`409 cluster_has_snapshots`). Moving a cluster to a new bucket means copying the bucket first (open question 2).
- **Credentials reach nodes,** as the registry needs them. The project should give a key scoped to the one bucket; the test notes when the key can list other buckets.

### The registry on each node

Part of the node spec ([node-agent](node-agent.md#desired-state)):

```jsonc
"registry": {
  "version": "3.1.2",
  "sha256": { "amd64": "…", "arm64": "…" },
  "memory_max": "512M", "gomemlimit": "400MiB",
  "config": {
    "version": "0.1",
    "storage": {
      "s3": { "region": "auto", "regionendpoint": "https://<acct>.r2.cloudflarestorage.com",
              "forcepathstyle": true, "bucket": "…", "chunksize": 104857600,
              "accesskey": "@file:/etc/lewbox/registry/access-key",   // the agent writes secrets to files
              "secretkey": "@file:/etc/lewbox/registry/secret-key" },
      "delete": { "enabled": true },
      "redirect": { "disable": false }
    },
    "http": { "addr": "127.0.0.1:5000", "secret": "@generate" }
  }
}
```

- **Installed and upgraded by the node agent:** download, check SHA-256, install, write the config (`0640`, root and the registry's user), and restart.
- **Health:** wait for `GET /v2/` within 30 s; the agent reports registry health with each heartbeat.
- **`MemoryMax` scales with `SB_SNAPSHOT_PUSH_MAX_IN_FLIGHT`,** as each upload holds a chunk in memory.
- **To verify, from legacy:**
  - a smaller R2 `chunksize` (16 MiB) works with v3, which would allow ~256M;
  - containerd allows plain HTTP to `127.0.0.1:5000` without `hosts.toml`;
  - sandboxes can't reach the host's `127.0.0.1:5000`.

### What's stored

| Artifact | Repository | Written by | Read by |
|---|---|---|---|
| Template build (OCI image) | `cluster/<clu>/templates/<template>/<bld>:<bld>` | `POST /v1/images/build` with `push`, on the building node ([templates/builds](../templates/builds.md)) | Any node creating a sandbox from it |
| Firecracker template (rootfs + memory snapshot) | `cluster/<clu>/templates/<tid>` (AerolVM's layout) | AerolVM `POST /v1/templates` | Peers, lazily; placement prefers nodes that have it cached |
| Sandbox snapshot | `cluster/<clu>/snapshots/<sbx>-<op>` (lowercased) | AerolVM's snapshot push, after `POST /v1/sandboxes/{id}/snapshot` | The node resuming it |

All names are lowercase, as Docker refuses repository names with capitals. The fork lowercases the whole path, cluster id included (F11), so Lewbox passes ids as they are.

### Offload and resume

The steps are in [sandboxes/persistence](../sandboxes/persistence.md). In short:

- **Stop:**
  1. stop;
  2. snapshot `<sbx>-<op>`;
  3. wait for the push to be `active` on the snapshotting node;
  4. destroy the sandbox;
  5. delete the local snapshot image;
  6. record the snapshot on the sandbox.
- **Start:** create from the snapshot name with `container_command` (until F10); any node pulls it.

What this plan owns is how long that takes, and how big it grows:

- **Upload speed (F15):** zstd, or parallel gzip, in the fork. The node spec sets the compression once the fork supports it (`SB_SNAPSHOT_COMPRESSION=zstd`). zstd layers need containerd or Docker 23+ to pull, which AerolVM's supported hosts have.
- **Waiting for a push:** with the fork's `/v1/snapshots/{name}` (F12) and the event stream (F35), the operation waits for a `snapshot.pushed` event, not a poll every few seconds.

### Flattening

Each stop adds a layer on top of the last. Overlay caps an image at ~125 layers, files deleted in the sandbox still take space in older layers, and every resume downloads them all.

- **The sandbox records `snapshot_layers`:** the count after each stop, read from the snapshot's manifest.
- **Past `LEWBOX_SNAPSHOT_MAX_LAYERS` layers** (default 32, [sandboxes/persistence](../sandboxes/persistence.md)), the next stop asks for `flatten: true` (F14). The fork exports the container's filesystem and imports it as one layer.
- **That stop is slower,** as it uploads everything, but every later resume is faster and the old snapshots become collectable.
- **If F14 misses the alpha** (it's P1), there's no Lewbox-side flatten: past 100 layers the dashboard and the sandbox's `warnings` say it will soon stop being able to persist, and to stop it on a release with F14. No workaround ([D15](../README.md#decisions)).

### Garbage collection

A leader-only job, `registry_gc`, hourly per cluster with storage ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)). It works on the bucket directly, from the registry's layout, as legacy's `CleanUpRegistry` did, in three passes:

1. **Snapshots:** delete each `cluster/<clu>/snapshots/<name>` repository that isn't the latest snapshot of a sandbox that isn't deleted. Keep:
   - anything written within the grace period (`LEWBOX_REGISTRY_GC_GRACE`, 6 h);
   - every snapshot of a sandbox with an operation in progress, as a stop records its snapshot only once its upload is done.
2. **Template builds:** delete `cluster/<clu>/templates/<template>/<bld>` for:
   - a build replaced by a newer ready one more than the grace period ago;
   - a failed build;
   - every build of a template deleted more than the grace period ago.

   Keep builds in progress, and builds some sandbox was created from and still runs on. Its snapshots link the build's layers, so those survive anyway, but a sandbox *running* from a build re-pushes its layers on stop if they're gone. Repositories not named like a build are left alone.
3. **Blobs:** delete blobs no repository in the bucket links to, across every cluster sharing the bucket, written longer ago than the grace period. Blobs are listed **before** links are read, so a blob written after the links were read is never deleted.

**Closing legacy's race.** A push can reuse an unlinked blob that's past the grace period, between pass 3 reading links and deleting it. Two changes close this:

- **Snapshots are recorded as pending before they're taken:** `sandbox_snapshots(sandbox_id, name, status: pending|pushed|deleted, created_at)`. A blob is deleted only if no pending snapshot or build was started before its link-listing began.
- **After deleting,** pass 3 re-lists links for the blobs it deleted. A blob that turned out to be linked is logged as an incident, and the snapshot that needed it is marked for re-push: a stop retries its push, which the node still has until the snapshot is confirmed `active`.

Other rules:

- **Dry run:** `lewbox admin registry-gc --cluster clu_… --dry-run` reports what would go.
- **Safety:**
  - GC refuses to run if the bucket listing fails part-way;
  - GC refuses to run if it would delete more than 50% of a cluster's bytes in one pass, unless forced.

  Either way it alerts the instance's operators.
- **Accounting:** each pass records bytes and objects deleted per cluster.

### Usage

The bucket's size is the project's cost, so Lewbox shows it:

- **Per cluster:** bytes in snapshots, templates and unlinked blobs; recomputed by GC each pass.
- **Per sandbox:** its latest snapshot's size, and layers.
- **Per template build:** `size_mb`, read from the manifest after push; it counts towards `template_storage_mb` ([limits](../limits.md)).
- **Per profile:** its persisted sandboxes' snapshot bytes, the real use behind its `storage_gb` limit (which today counts declared `disk_gb`, open question 4).

### Without storage

A cluster without a bucket:

- refuses `persist: true` (`422 persistence_unsupported`, with `reason: no_cloud_storage`): stopping a sandbox deletes it;
- can't build templates (`template_unavailable`, `reason: no_cloud_storage`), so sandboxes there use public images;
- has no registry on its nodes, and none of the AerolVM push settings.

The dashboard says so on the cluster page and at Create cluster.

### Warm images

AerolVM keeps popular images ready (`SB_DOCKER_POOL_IMAGES`). The node spec can list a cluster's most-used template builds there, so a node pulls them before the first create needs them (Beta, open question 5).

## API

Admin API (paths per [api](../api.md)):

| Method and path | What |
|---|---|
| `PUT /v0/projects/{prj}/clusters/{clu}/storage` | Set the bucket (tested first; `422 storage_connection` with the failed step). Refused with `409 cluster_has_snapshots` when changing a bucket in use |
| `DELETE …/clusters/{clu}/storage` | Only with no persisted sandboxes or ready template builds |
| `POST /v0/projects/{prj}/storage-tests` | Test without saving |
| `GET …/clusters/{clu}/storage/usage` | Bytes by kind; last GC pass |

Consumers see the effects only through `persistence_unsupported`, `template_unavailable`, and their limits.

## Security

- **Bucket keys:**
  - encrypted at rest in the control plane;
  - written to nodes in `0640` files readable only by the registry's user;
  - never shown again after saving;
  - never in the spec or in logs.
- **The registry listens only on localhost.** Sandboxes must not reach it: verify that a sandbox can't connect to the host's `127.0.0.1:5000` (Docker's default bridge doesn't route to host loopback; check gVisor and Firecracker likewise when they come). Add a firewall rule anyway.
- **Presigned URLs** are short-lived (the registry's default, 20 minutes), and only for blobs.
- **GC deletes only within `cluster/<id>/` prefixes** of clusters the bucket belongs to, and blobs nothing links to. It never touches other keys in the bucket.

## Observability

- **Cluster page:** storage provider and bucket; usage by kind; last GC (deleted, kept, errors).
- **Sandbox:** snapshot size, layers, last offload and resume durations.
- **Metrics:**
  - `lewbox_registry_gc_deleted_bytes_total{cluster}`;
  - `lewbox_snapshot_push_seconds`, `lewbox_snapshot_size_bytes`, `lewbox_snapshot_layers`;
  - `lewbox_resume_pull_seconds`.
- **Alerts:**
  - a node's registry unhealthy;
  - snapshot pushes failing;
  - a GC pass that refused to run.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_REGISTRY_VERSION` | `3.1.2` | Distribution version in the spec |
| `LEWBOX_REGISTRY_GC_INTERVAL` | `1h` | |
| `LEWBOX_REGISTRY_GC_GRACE` | `6h` | |
| `LEWBOX_NODE_SNAPSHOT_PUSH_MAX_IN_FLIGHT` | `2` | |
| `LEWBOX_SNAPSHOT_MAX_LAYERS` | `32` | Layers above the base image before a stop flattens ([sandboxes/persistence](../sandboxes/persistence.md)) |

## Testing

- **GC against an in-memory S3 fake,** with a fixed registry layout:
  - each pass's keep and delete rules;
  - grace periods;
  - the race (a link appears between listing and delete);
  - the 50% safety stop;
  - partial listings.
- **Storage tests** against MinIO (in CI) and recorded R2 and S3 responses, including R2's unequal-parts refusal.
- **End to end:**
  - offload and resume on another node;
  - flattening after N stops;
  - a template pulled on a second node;
  - GC after deleting a sandbox frees its bytes.

## Build plan

1. **M1:**
   - the storage provider interface and the `storage/s3` driver plugin (`plugins/storage/s3`);
   - `cluster_storage` and the connection test;
   - the registry in the node spec;
   - AerolVM push settings.
2. **M1:** GC job (three passes, grace, safety), with `sandbox_snapshots` closing the race.
3. **M2:** usage accounting; cluster page storage panel.
4. **M3:** flattening (with F14); compression (with F15).
5. **Beta:** warm images; per-profile snapshot bytes in limits.

## Open questions

1. ~~**Instance-provided storage on lewbox.com.**~~ Decided (2026-10-29): not in the alpha or the beta; every project brings its own bucket. Later.
2. ~~**Moving a cluster to another bucket.**~~ Decided (2026-10-29): not supported: changing a bucket in use is refused (`409 cluster_has_snapshots`), so a new bucket means a new cluster. Copy-and-switch is Later.
3. ~~**Flattening before F14.**~~ Decided (2026-10-29): no Lewbox-side flatten; wait for the fork ([D15](../README.md#decisions)).
4. ~~**`storage_gb`.**~~ Decided (2026-10-29): keep counting declared `disk_gb` of persisted sandboxes, which is predictable; revisit with real snapshot bytes once F14 reports them.
5. **Warm images.** Deferred (Beta): which builds to keep pre-pulled, and how much node disk to give them.
