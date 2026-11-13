# Snapshot archive: what AerolVM v0.7.0 does

> [!NOTE]
> **Design notes.** Findings from a test on the dev cluster on 2026-11-13, for [plans/08-dns-storage-archive.md](../../plans/08-dns-storage-archive.md) (DECISIONS D35, D36). To *use* Lewbox, start at the [docs home](../README.md).

## Setup

- **AerolVM:** stock v0.7.0, two Docker nodes (`node-1`, `node-2`) in cluster `lab`, from `deploy/dev-cluster`.
- **Bucket:** an S3-compatible server on the dev network (RustFS: MinIO's images are no longer public on Docker Hub or quay.io), bucket `lewbox`.
- **Registry:** CNCF Distribution v3.1.2 on each node, at `127.0.0.1:5000`.
  - S3 driver: `regionendpoint` set, `forcepathstyle: true`, `rootdirectory: /clusters/<cluster id>`, `delete.enabled: true`.
  - Run as user `lewbox-registry` under a hardened systemd unit.
  - The release hashes match the legacy pins: amd64 `40df2224…3acc8`, arm64 `09d26f88…e95e73`, which are also the release's own `.sha256` files.
- **Lines added to `/etc/sandboxd/lewbox.env`, then `systemctl restart sandboxd`:**
  ```
  SB_SNAPSHOT_PUSH_ENABLED=true
  SB_MIRROR_PUSH_HOST=127.0.0.1:5000
  SB_IMAGE_DISTRIBUTION_AOCR_HOST=127.0.0.1:5000
  SB_AUTO_IMPORT_CLUSTER_ID=<cluster id>
  SB_AUTO_IMPORT_CLUSTER_PAT_PATH=/etc/sandboxd/lewbox-registry.token   # random bytes, not the PAT
  SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL=30s
  ```
- **Tokens:** every call used a `runner` token bound to team `spike`, minted with the PAT over loopback, as Lewbox's team tokens are.

## Findings

| # | What | Result |
|---|---|---|
| 1 | Snapshot a **stopped** sandbox (`POST /v1/sandboxes/{id}/snapshot {"name":"archive-<id>"}`) | `201`. `push_state` went `pending`, then `pushing`, then `active` in about 10 s for a 20 MB change. A random token file works: the registry has no auth |
| 2 | What the push stores | `image_registry_ref` = `127.0.0.1:5000/cluster/<cluster id>/snapshots/tenant-<hash>/archive-<id>:latest`. The **whole image** goes to the bucket, base layers included (50 MB for `ubuntu:22.04` plus 20 MB). Blobs are content-addressed, so snapshots on one base share its layers |
| 3 | Delete the sandbox after the push | `204`. The snapshot's record **and its local image** (`aerolvm-snapshot/<hash>:latest`, 160 MB) stay on the node that took it |
| 4 | Restore by snapshot **name** on another node (`POST /v1/sandboxes {"image":"archive-<id>"}`, through node-2's API, node-1 cordoned) | `201` on node-2: it pulled from its own registry, so from the bucket. Files intact (`marker.txt` and a 20 MB file) |
| 5 | What a restore leaves behind | The pulled image `127.0.0.1:5000/…/archive-<id>` (160 MB) stays on the node **after the restored sandbox is deleted**. Every restore leaves a copy |
| 6 | Restore by **name** on the snapshotting node after `docker rmi` of its local image | `400 image "aerolvm-snapshot/…" is local-only and is not present on this node` |
| 7 | Restore by **registry ref** on that same node (`"image":"127.0.0.1:5000/…:latest"`) | `201`: pulled from the bucket. **So Lewbox restores by ref, and nodes can drop local copies** |
| 8 | Another team's runner token, by name | Refused: the name resolves within that team's own tenant path, which doesn't exist |
| 9 | Another team's runner token, by the first team's ref | `400 sandbox not found: image "127.0.0.1:5000/…"`. AerolVM keeps snapshot refs to their team |
| 10 | `DELETE /v1/snapshots/archive-<id>` with the team token | `204`. The record is gone from `GET /v1/snapshots`. The manifest in the bucket: see 11. Pulled copies on both nodes stayed |
| 11 | Does deleting the snapshot retire its manifest in the bucket? | **No.** Two minutes and several push sweeps later (`retired=0` in sandboxd's log), all three manifest links and both blobs were still in the bucket |
| 12 | Cordon and uncordon | `POST /v1/cluster/nodes/{id}/cordon` and `/uncordon` exist (204); `drain` too |
| 13 | A full node | The dev host was short of memory. Placement refused with `503 host capacity exceeded: live memory floor breached` rather than choosing the other node. Not specific to archives |
| 14 | A snapshot named `archive-<name>-<id>` (`archive-keeper-sb-…`) | Accepted: pushed, archived and restored like `archive-<id>` (e2e, 2026-11-13) |
| 15 | When did a node pull an image? (Docker 29.1.3, containerd image store) | `docker image inspect` gives `Created` = when the snapshot was committed and `Metadata.LastTagTime` = when this node pulled it (13:42:57 and 13:43:10 for a restore). So a pulled copy's age can be read from its last tag time |

## What Lewbox does with this

- **Archive** = snapshot (`archive-<sandbox id>`), wait for `push_state: active`, check the sandbox is still `stopped`, delete it, and record `image_registry_ref`.
- **Restore** = create with `image` set to the **registry ref**, not the name (finding 7), with the workspace's runner token. AerolVM keeps the ref to that team (finding 9).
- **Node disk** has to be freed on the node itself (findings 3 and 5). Lewbox has no agent, so `node configure` installs a root systemd timer, `lewbox-archive-cleanup`. Every 5 minutes it:
  - lists the node's snapshots with the PAT over loopback;
  - for each `archive-*` snapshot whose `push_state` is `active`, removes its local `image` if it is still on the node (the record outlives the image, so later passes skip it);
  - removes pulled `127.0.0.1:5000/cluster/<id>/snapshots/*/archive-*` images.

  The cluster's `archive_local_retention` (D37) can hold copies back: `--older-than` ages a snapshot's copy from the snapshot's `created_at` and a pulled one from its last tag time (finding 15); `never` installs no timer.

  `docker rmi` fails, harmlessly, for an image a container still uses. Customers' own snapshots (not `archive-*`) are left alone, so restoring them by name keeps working on their node.
- **Deleting an archive:**
  1. `DELETE /v1/snapshots/{name}` removes AerolVM's record.
  2. Lewbox deletes the repository's keys from the bucket itself (`<prefix>/clusters/<cluster id>/docker/registry/v2/repositories/<repo>/`), because AerolVM doesn't (finding 11).

  Blobs no repository links any more stay until a bucket sweep, which isn't built yet. Blobs are shared between snapshots, so only a sweep that reads every link may delete them.
- **Firecracker:** these tests are all `docker` runtime. AerolVM v0.6.11 added Firecracker snapshot push and cross-node starts ([aerol-ai/microvm#785](https://github.com/aerol-ai/microvm/discussions/785)), so Lewbox allows `archive` on Firecracker clusters too. A Firecracker snapshot saves the disk only: not memory, which a plain Firecracker stop keeps.

## Firecracker (v0.7.0, dev cluster, 2026-11-13)

Two Firecracker nodes in privileged Docker containers with the host's `/dev/kvm` (`./dev.sh cluster --runtime firecracker`), then `archives.sh`.

| # | What | Result |
|---|---|---|
| F1 | Snapshot a stopped Firecracker sandbox | `201`. A helper VM exports the disk; the snapshot is a Docker image `aerolvm-snapshot/<hash>:latest` (one flat layer), pushed like a container's. The cleanup timer's `docker image rm` frees it |
| F2 | Restore by registry ref with `runtime: firecracker`, before any change | **Fails**: AerolVM builds the VM's disk with `skopeo copy docker://127.0.0.1:5000/…`, which tries HTTPS: `server gave HTTP response to HTTPS client`. Docker trusts a loopback registry by itself; skopeo doesn't |
| F3 | The same, with a containers `registries.conf.d` drop-in marking `127.0.0.1:5000` `insecure = true` | `201`, files intact. `node configure` now writes it |
| F4 | What a Firecracker restore leaves behind | Nothing outside the sandbox's own run dir (`/srv/jailer/firecracker/<id>`, removed with it): skopeo's work dir is cleaned, and no image is left in Docker |
| F5 | Extra disks | In v0.7.0 a VM has `rootfs.ext4` only, plus `export.ext4` on the export helper. `SB_FIRECRACKER_OVERLAY_ENABLED` exists but neither the installer nor Lewbox sets it |
| F6 | Stop a 512 MB Firecracker sandbox through Lewbox | Took 13 s: a Firecracker stop writes the VM's memory to disk. Lewbox's 10 s call timeout cancelled the request, and AerolVM then **abandoned the stop** (`PUT /snapshot/create: context canceled`), leaving the sandbox running. Stops, deletes and restores now get 5 minutes and don't depend on the caller staying connected; the archiver's snapshot gets 15 minutes. This affects every Firecracker cluster, archiving or not, including the stops a workspace suspension makes |
