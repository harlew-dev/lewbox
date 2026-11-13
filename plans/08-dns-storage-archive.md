# Phase 8: DNS profiles, storage profiles, and archiving stopped sandboxes

**Asked for on 2026-11-13.** Nodes have little disk (70 GB). A stopped sandbox must not stay on its node: it is archived to the operator's bucket, and any node can bring it back. To set that up, the operator saves Cloudflare and S3 credentials once, as **profiles**, and chooses them when starting a cluster or adding a node.

Decisions: D34 (DNS profiles), D35 (storage profiles and a registry on each node), D36 (archive on stop, with a new ID on restore). The AerolVM change that would keep the ID is in [aerolvm-push-on-stop-request.md](aerolvm-push-on-stop-request.md).

## What AerolVM v0.7.0 gives us

All verified in the v0.7.0 source; see D35 and D36 for the details.

- **DNS:**
  - `install.sh --domain D --dns-provider cloudflare --dns-api-token T` gets a wildcard certificate by DNS-01.
  - Cloudflare is the only provider, and the token stays on the node for Caddy.
  - The installer creates no DNS records.
- **Snapshot push:**
  - `SB_SNAPSHOT_PUSH_ENABLED=true` with `SB_MIRROR_PUSH_HOST` pushes every snapshot to an OCI registry, in the background.
  - It needs `SB_AUTO_IMPORT_CLUSTER_ID` and `SB_AUTO_IMPORT_CLUSTER_PAT_PATH`, even with auto-import off.
  - Snapshots carry `push_state` (`pending`, `pushing`, `active` or `error`) and `image_registry_ref`.
  - Any node can start a pushed snapshot.
- **Restore:** `POST /v1/sandboxes` with `image` set to a snapshot name starts a new sandbox from it, with a new ID. A snapshot keeps no `env` values, CPU, memory or disk.
- **Pause:** E2B `pause` is a stop. A stopped sandbox's disk stays on its node.

## Profiles

Instance-wide. Instance admins only. Secrets are sealed with the keyring, like cluster tokens. Never shown again after saving; the CLI fetches them only to configure a node.

### DNS profile (D34)

| Field | |
|---|---|
| `name` | Label-style, looked up like an ID |
| `provider` | `cloudflare` (the only one AerolVM's installer supports) |
| `zone` | The zone, e.g. `example.com`. The token must reach it |
| `api_token` | Write-only. Zone:Read and DNS:Edit on that zone |
| `acme_email` | Optional. Passed as `--acme-email` |
| `version` | Goes up on every change to the token or the zone |
| `tested_at`, `test_error` | The last test |

**Test** (on create, on change, and on demand):
1. verify the token;
2. find the zone;
3. create and delete the TXT record `_lewbox-test.<zone>`.

It reports the step that failed. **Delete** is refused while a cluster uses the profile.

### Storage profile (D35)

| Field | |
|---|---|
| `name` | As above |
| `endpoint` | Empty for AWS S3; `https://<account>.r2.cloudflarestorage.com` for R2; any S3-compatible URL (MinIO, B2, Wasabi) |
| `region` | `auto` for R2 |
| `bucket`, `prefix` | `prefix` is optional; each cluster stores under `<prefix>/clusters/<cluster id>/` |
| `path_style` | Defaults to true when `endpoint` is set |
| `access_key_id`, `secret_access_key` | The secret is write-only |
| `version`, `tested_at`, `test_error` | As for DNS profiles |

**Test:** put, get, list and delete `lewbox-test/<random>`, with a small S3 client on the standard library (SigV4).

**Delete and changes:**
- Delete is refused while a cluster uses the profile.
- Changing `bucket`, `endpoint` or `prefix` is refused while archives exist on it (they would become unreachable).

## Clusters

New cluster fields:

| Field | |
|---|---|
| `domain` | The cluster's domain in domain mode, from the install |
| `dns_profile` | The DNS profile used for TLS and records. Optional |
| `dns_records` | `auto` (default when there is a DNS profile) or `off`, when the operator points DNS at a load balancer |
| `storage_profile` | Optional |
| `stop_policy` | `keep` (default) or `archive`. `archive` needs a storage profile |

Set when the cluster is registered (`node add --new-cluster` reads them from the install), and later with `PATCH /v0/clusters/{ref}` and `lewbox cluster set`.

**DNS records**, when `dns_records` is `auto`:
- Each live `mixed` or `ingress` member whose `public_host` is an IP gets an A or AAAA record for `<domain>` and for `*.<domain>`, TTL 60, DNS only (never proxied).
- Lewbox records each record's Cloudflare ID in `dns_records`, and deletes only those.
- The cluster check reconciles them: records are added for new members and removed for members that are gone.

## Nodes

### `lewbox install`

- `--dns-profile NAME` replaces `--dns-api-token-stdin` (which stays, for use without an orchestrator). The CLI fetches the token from the orchestrator and passes it to the installer, masked, as today.
- The zone must contain `--domain`.
- `--acme-email` defaults to the profile's.

### `lewbox node add` and the new `lewbox node configure`

Both apply the cluster's **node settings**, fetched from `GET /v0/clusters/{ref}/node-settings` (admin only, holds secrets):

- **With a storage profile:**
  - Install CNCF Distribution (pinned version, SHA-256 checked) as `lewbox-registry` on `127.0.0.1:5000`, backed by the bucket under the cluster's prefix.
  - Its config goes in `/etc/lewbox-registry/config.yml`, `0640 root:lewbox-registry`. A hardened systemd unit runs it.
  - Add to `/etc/sandboxd/lewbox.env`:
    - `SB_SNAPSHOT_PUSH_ENABLED=true`
    - `SB_MIRROR_PUSH_HOST=127.0.0.1:5000`
    - `SB_IMAGE_DISTRIBUTION_AOCR_HOST=127.0.0.1:5000`
    - `SB_AUTO_IMPORT_CLUSTER_ID=<cluster id>`
    - `SB_AUTO_IMPORT_CLUSTER_PAT_PATH=/etc/sandboxd/lewbox-registry.token`
    - `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL=30s`
  - The token file holds a random value, never the PAT. The registry has no auth (loopback only), so the PAT never leaves the node.
  - Install a cleanup timer that removes archive images the node no longer needs (from the step 3 test).
- **Without one:** remove the registry and those env lines, if present.
- **When the DNS profile's token changed:** re-run the verified installer with `--version <installed>` and the new `--dns-api-token`. The installer keeps its other saved options.

After applying, the CLI reports the versions it applied (`PUT /v0/nodes/{ref}/settings`).

**Outdated nodes:**
- A node whose applied versions differ from its cluster's profiles is **outdated**.
- The cluster page and node list show this with the command to run: `lewbox node configure`, on that node.
- The cluster check reports it as a warning.

## Archiving (D36)

On a cluster with `stop_policy: archive`, **every stopped customer sandbox is archived**. Stopped sandboxes don't persist there: the portal and docs say so.

1. **Found by the sync loop:**
   - A customer sandbox is `stopped` on an archive cluster and has no live archive.
   - Lewbox inserts an `archives` row (`pending`) and hands it to the archiver.
   - The archiver runs beside the sync loop, at most 2 at once per cluster.
2. **Snapshot:**
   - Mint a `runner` token for the workspace team, for 1 hour, revoked afterwards (the D23 pattern).
   - Read the sandbox (`GET /v1/sandboxes/{id}`) for its name, tags, image, runtime and size.
   - Snapshot it as `archive-<sandbox id>`. The status becomes `pushing`.
3. **Wait** until the snapshot's `push_state` is `active`. A push `error` is retried by AerolVM; Lewbox keeps waiting, up to 30 minutes, then marks the row `failed` and retries the whole archive later.
4. **Check** the sandbox is still `stopped`. If it was started again, mark the row `cancelled` and delete the snapshot.
5. **Delete the sandbox.** The status becomes `archived`, with `archived_at` and `registry_ref`.

**Restore** (`POST /v0/archives/{id}/restore`; owners, admins, members, the workspace API key, instance admins):
- Creates a sandbox with the workspace's runner token:
  - `image` is the snapshot's name;
  - name, tags, runtime, CPU, memory and disk are the archived sandbox's;
  - the tag `lewbox.restored_from=<old id>` is added.
- Returns the new sandbox. The archive is kept, so a sandbox can be restored again.
- Customers can do the same with the SDK: `create({image: "archive-<old id>"})`.

**Delete** (`DELETE /v0/archives/{id}`) deletes the snapshot through AerolVM, which retires its registry copy, then marks the row `deleted`.

Archives are **kept until the customer or an operator deletes them** (no retention for now).

### The `archives` table

| Column | |
|---|---|
| `id`, `org_id`, `cluster_id` | |
| `sandbox_id`, `sandbox_name`, `image`, `runtime`, `cpu`, `memory_mb`, `disk_gb`, `tags` (JSON) | The archived sandbox |
| `snapshot_name`, `registry_ref` | |
| `status` | `pending`, `pushing`, `archived`, `failed`, `cancelled` or `deleted` |
| `error`, `attempts`, `next_attempt_at` | |
| `created_at`, `archived_at`, `deleted_at` | |
| `last_restored_sandbox_id`, `last_restored_at` | |

A unique index on `(cluster_id, sandbox_id)` while `status` isn't `cancelled` or `deleted` keeps the sync loop from archiving one sandbox twice.

### API and CLI

| Route | CLI |
|---|---|
| `GET /v0/archives?region=` | `lewbox archive list [--region] [--org]` |
| `GET /v0/archives/{id}` | `lewbox archive show ID` |
| `POST /v0/archives/{id}/restore` | `lewbox archive restore ID` |
| `DELETE /v0/archives/{id}` | `lewbox archive delete ID [--yes]` |
| `GET/POST /v0/dns-profiles`, `GET/PATCH/DELETE /v0/dns-profiles/{ref}`, `POST …/test`, `POST …/credentials` | `lewbox dns-profile list\|show\|create\|update\|delete\|test\|credentials` |
| The same under `/v0/storage-profiles` | `lewbox storage-profile …` |
| `PATCH /v0/clusters/{ref}` | `lewbox cluster set` |
| `GET /v0/clusters/{ref}/node-settings`, `PUT /v0/nodes/{ref}/settings` | used by `node add` and `node configure` |

## Web

- **Console, Settings:**
  - *DNS profiles* and *Storage profiles* tabs: tables, a create or edit dialog with a Test button, delete.
  - The storage dialog has presets (AWS S3, Cloudflare R2, MinIO, Other) that fill the endpoint, region and path style.
- **Console, Start cluster and Add node:** two steps.
  1. **Configure:**
     - Mode: IP, domain or local.
     - For domain mode: the domain, a DNS profile (with a link to add one), the ACME email and DNS records.
     - Runtime, a storage profile, and "When a sandbox stops" (keep it on the server, or archive it to storage).
     - For a new node: its role.
     - Under Advanced: node name, AerolVM version and installer flags passed through.
  2. **Install commands:** the commands those choices produce, to copy.
- **Console, cluster page:** a Settings card (domain, profiles, stop policy, DNS records) with Edit, and an outdated badge on nodes.
- **Portal, Sandboxes:**
  - an *Archived* tab: name, region, archived time, original size, and Restore and Delete;
  - on an archive region, a note under the list: "Stopped sandboxes are archived to storage and come back as new sandboxes."

## Build order (one commit each)

1. Plan and decisions (this file).
2. DNS profiles: migration, Cloudflare client, service, routes, Backend, client, CLI, parity tests.
3. Cluster DNS settings, `install --dns-profile`, records on the cluster check.
4. Storage profiles: migration, S3 client, service through CLI, parity tests.
5. **Dev cluster test:** MinIO and a registry on the dev nodes. Snapshot, push, restore on another node, find what is left on the first node's disk. Findings go in `docs/design/snapshot-archive.md`. Steps 6 and 7 depend on it.
6. Node settings: the registry, env, cleanup timer, `node configure`, reported versions, outdated nodes. `e2e.sh` step.
7. Archiving: table, archiver, restore, delete, API, CLI, parity tests. `e2e.sh` step.
8. Web: profiles, cluster settings, the two-step dialogs, the archived tab.
9. Docs: both books and `cli.md`.
