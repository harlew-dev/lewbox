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

## What Lewbox does with this

- **Archive** = snapshot (`archive-<sandbox id>`), wait for `push_state: active`, check the sandbox is still `stopped`, delete it, and record `image_registry_ref`.
- **Restore** = create with `image` set to the **registry ref**, not the name (finding 7), with the workspace's runner token. AerolVM keeps the ref to that team (finding 9).
- **Node disk** has to be freed on the node itself (findings 3 and 5). Lewbox has no agent, so `node configure` installs a root systemd timer, `lewbox-archive-cleanup`. Every 5 minutes it:
  - lists the node's snapshots with the PAT over loopback;
  - for each `archive-*` snapshot whose `push_state` is `active`, removes its local `image`;
  - removes pulled `127.0.0.1:5000/cluster/<id>/snapshots/*/archive-*` images.

  `docker rmi` fails, harmlessly, for an image a container still uses. Customers' own snapshots (not `archive-*`) are left alone, so restoring them by name keeps working on their node.
- **Deleting an archive:**
  1. `DELETE /v1/snapshots/{name}` removes AerolVM's record.
  2. Lewbox deletes the repository's keys from the bucket itself (`<prefix>/clusters/<cluster id>/docker/registry/v2/repositories/<repo>/`), because AerolVM doesn't (finding 11).

  Blobs no repository links any more stay until a bucket sweep, which isn't built yet. Blobs are shared between snapshots, so only a sweep that reads every link may delete them.
- **Docker only** for now: these tests are all `docker` runtime. A Firecracker snapshot is a disk image, and its push and pull weren't tested.
