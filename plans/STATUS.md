# Status (2026-11-13)

## What exists now

Lewbox is a self-hosted E2B: you run AerolVM clusters, and Lewbox is the account and UI layer around them. It is **one Go binary with one SQLite file**.

**For you (console, `/admin`, and the CLI):**
- **Overview**: customers, running sandboxes, capacity, 30-day uptime per cluster, top customers.
- **Customers**:
  - **New customer** in one form: account, workspace and a plan per region.
  - Approve, suspend and restore.
  - Per-region plans with optional per-region overrides.
  - Each customer's usage, sandboxes, keys and members.
- **Fleet sandboxes**: across every customer, for spotting abuse.
- **Plans**: named limit sets.
- **Servers**:
  - Clusters and nodes.
  - 30-day uptime bars and node metrics (sandboxes, vCPU allocated, memory, disk).
  - A warning when a node's cluster certificate is near expiry.
- **Users**, and **Settings** (sign-up mode, default plan and regions).
- **Access tokens** (D38): `lbp_…` tokens that act as an account, made in Account settings or with `lewbox token create`, for scripts and CI. An instance admin's token can add clusters and nodes; `lewbox login --token` saves one.
- **Machines**: `lewbox install`, `node add`, `update` and `firewall enable` set them up.

**For customers (portal, `/`):**
- **Home**:
  - Get started: create a key, copy the SDK snippet, then "waiting for your first sandbox…".
  - After that: 30-day numbers and per-region limit meters.
- **Sandboxes**, with a **Snapshots** tab.
- **API keys**: per region, shown once, with AerolVM SDK (TypeScript and Python), E2B SDK and curl settings.
- **Usage** charts.
- **Team**: members, management keys, rename.

**How it maps to AerolVM** (DECISIONS D29): a workspace is one AerolVM team. Its plan in a region is that team's quota on that cluster, and AerolVM enforces it. API keys are AerolVM runner tokens bound to the team. Lewbox is never in the sandbox request path.

**Deliberately not built:**
- Billing (D24).
- Per-member limits (D29; built, then reverted).
- Email, invitation links, webhooks.
- A CPU-utilisation agent on nodes (D11).

## Phase 8: DNS profiles, storage profiles, archiving (2026-11-13)

Branch `feat/dns-storage-archive`. Plan: [08-dns-storage-archive.md](08-dns-storage-archive.md); decisions D34–D36.

**Built:**
- **DNS profiles** (Cloudflare): saved, sealed, tested before saving (find the zone, then write and delete a TXT record).
  - `lewbox install --dns-profile`.
  - Clusters record their domain and profile.
  - The cluster check keeps `<domain>` and `*.<domain>` A/AAAA records pointing at live ingress nodes, and deletes only records Lewbox made.
- **Storage profiles** (S3, R2, MinIO and other S3-compatible stores): saved, sealed, tested (write, read, list, delete), with a standard-library SigV4 client.
- **Node settings:**
  - `lewbox node configure` (also run by `node add`) installs the pinned CNCF Distribution registry on the bucket, AerolVM's snapshot push settings and the archive cleanup timer, set by the cluster's `archive_local_retention` (D37: `instant`, `1h`, `1d`, `7d` or `never`). `archive_snapshot_name` names archive snapshots `archive-<id>` or `archive-<name>-<id>`.
  - It re-runs the installer when a DNS token changed.
  - Nodes report what they applied, and outdated nodes are flagged on the cluster.
- **Archiving:**
  - On a cluster with `stop_policy: archive`, every stopped customer sandbox is snapshotted, pushed to the bucket, checked unchanged, and deleted.
  - Restore makes a **new** sandbox (new ID) from the registry ref, on any node.
  - Delete removes the snapshot and its repository from the bucket.
  - API, CLI (`lewbox archive`), and portal and console tabs.
- **Web:**
  - Profile tabs in Settings.
  - Start cluster and Add node as two steps (Configure, then Install commands).
  - A cluster Settings card and outdated badges.
  - Archived tabs in the portal and on the fleet Snapshots page.

**Verified:**
- `make test` passes: Go, and 32 web test files with 122 tests.
- The S3 client against a real S3-compatible server (RustFS), and against AWS's published SigV4 example.
- The AerolVM behaviour archiving depends on, tested by hand on real v0.7.0 ([docs/design/snapshot-archive.md](../docs/design/snapshot-archive.md)):
  - push to a loopback registry on a bucket;
  - restore by registry ref on another node;
  - other teams refused;
  - what's left on nodes and in the bucket;
  - `updated_at` unchanged by a snapshot.
- The new console screens, in a browser.

- `deploy/dev-cluster/archives.sh`, inside the full `e2e.sh` on real AerolVM v0.7.0 (2026-11-13, all 18 steps): registry install on real nodes, archive as `archive-<name>-<id>`, local copies kept under a 1d retention and freed under `instant`, restore on any node, delete from the bucket, and back to `keep`. That run found and fixed a bug: `node add` in local mode reported settings for a node the orchestrator calls `standalone`.

**Not verified yet:**
- Domain mode with a real Cloudflare zone hasn't been run.

**Gaps:**
- Blobs in the bucket are never swept (only an archive's repository is deleted).
- Restores get a new ID (the AerolVM change that would fix it: [aerolvm-push-on-stop-request.md](aerolvm-push-on-stop-request.md)).
- Archiving is only for Docker clusters.
- Bucket keys and the DNS token live on every node.

## What was verified, and how

| Check | Result |
|---|---|
| `make test` (gofmt, go vet, Go tests, web typecheck and 67 web tests) | Pass |
| `ui` library tests (125) and build | Pass |
| `make release VERSION=v0.2.0-rc.1` | Builds static linux/amd64 and arm64 binaries, `release.json` (AerolVM v0.7.0) and `checksums.txt` |
| `deploy/dev-cluster/e2e.sh` on real AerolVM v0.7.0, 2–3 nodes in Docker: install, cluster, join, firewall, then `customers.sh` (below), node re-add, failure and recovery, node removal, an AerolVM upgrade v0.6.13 → v0.7.0 | **All 13 steps and 100 checks pass.** Steps 1–11 passed on a fresh cluster with the final code; the final pass ran with `--reuse` after two test-only fixes |
| `customers.sh`: sign-up and operator-created customers; per-region plans (a workspace with no plan in a region can't see it or make keys there); a key works only on its region; a sandbox run and exec with the customer's key straight on AerolVM; Lewbox mirrors it for that workspace only (other workspaces get 404 from AerolVM and from Lewbox); snapshot, list, delete; the plan's limit enforced by AerolVM (`429`); stop and delete through Lewbox; usage recorded; overview, health and metrics; suspend stops sandboxes and blocks creates, restore allows them; key revoke → `401` on the cluster | Pass |
| Web pages in a real browser (Chromium screenshots against the dev orchestrator with demo data) | Console overview, customers, customer page, cluster page (uptime, checks, metrics), portal home, sandboxes, keys, usage, team: all render real data |

Bugs that only real AerolVM caught:
- A temporary token name one character over AerolVM's 64-character limit. The test fake now enforces the rule.
- Health history wasn't available until the first check after a restart. The test now waits for it.

## Known gaps and caveats

- **The sandbox list in Lewbox can lag a few seconds.** Once during testing, AerolVM's list briefly omitted a running sandbox, so Lewbox showed it gone for one pass (15 s), then back. Quotas aren't affected, because limits come from the plan, not the mirror.
- **Usage is polled every 15 s** (D9), so it's accurate to about ±15 s per sandbox. Time while Lewbox itself is down isn't counted.
- **No CPU-utilisation graphs.** AerolVM doesn't report them; the console shows vCPU *allocated* (D11).
- **The sign-up limit is in memory** (5 per IP per hour) and resets when Lewbox restarts. Behind a proxy, set `LEWBOX_TRUSTED_PROXIES` (RELEASING.md does).
- **Single-node AerolVM** (`lewbox install --mode local`) can't attribute sandboxes to customers (D8). Use `node add --new-cluster`, the default.
- **Snapshots have no size in AerolVM**, so none is shown.
- **The E2B SDK only works up to 2.50.0**, on Docker regions (AerolVM's facade).
- **A member's keys are revoked when they leave** (D30). A production app using a departed member's key stops working.
- **Node metrics average in samples from a node's first minutes** (when AerolVM reports 0 cores while it joins), so a fresh node's cores can read low, e.g. 2.4 instead of 4, until the window moves past them.
- **`MVP.md`** describes a bigger product (many operators, billing). It's left unedited; `plans/` is the current spec.

## Next steps, if you want them

1. **Email**: verification, password reset and invitation links (D4, D18).
2. **Rename the API and CLI to "workspace"** before the first public release (D28). Mechanical.
3. **Billing on the existing ledger design** (`05-billing.md`), with Stripe Checkout for top-ups.
4. **AerolVM's audit export** for exact start and stop times (D9).
5. **Per-member limits**: `git revert a9fa739` brings them back, if AerolVM never grows per-token quotas.

## How it was built

The plans in this folder were written first, then each phase was implemented:
- **Go phases**: by OpenCode Go agents (DeepSeek V4.1 Flash). That was 14 runs for about $2 in total.
- **Web app and UI library components**: by Claude sub-agents.
- **Research and reviews**: by Claude sub-agents. Independent reviews found 3 security gaps (fixed) and a fair amount of leftover code (deleted).

Every phase was checked with `make test` and, for cluster work, the real-AerolVM e2e before it was committed.

The repo (`harlew-dev/lewbox`, branch `main`) has about 25 commits on top of the baseline import. **Nothing was pushed.**
