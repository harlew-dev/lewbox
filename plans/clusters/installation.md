# Installing a node

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

A project adds a server to a cluster by running one command on it. The command installs `lewbox`, which checks the machine, enrolls it with the control plane, installs AerolVM (our [fork](aerolvm-fork.md)) and the registry, sets up DNS and the firewall, joins the AerolVM cluster, and leaves the [node agent](node-agent.md) running. Each step is reported, so the dashboard can follow along. This replaces legacy's 1,246-line `install.sh` and its four systemd timers with a Go program that can also be upgraded once the node is installed.

## Today

- **The command:** `curl -fsSL https://<app>/install.sh | sudo bash -s -- --token <token>`, served from `lewbox-legacy/resources/scripts/install.sh` with the app's URL written in. The token belongs to a `cluster_installations` row:
  - it's single use, and expires 60 minutes after issue unless started;
  - it's stored hashed for lookup, and encrypted so the command can be shown again;
  - it stops working once cancelled or finished (`410`).
- **Statuses** (`App\Enums\Cluster\InstallationStatus`): `pending → started → configuring_dns → installing → provisioning_storage → awaiting_seed → joining → online | failed`, with cancellation as a soft delete. The script calls the node API in order (`lewbox-legacy/docs/private/node/installing-a-node.md`):
  1. `PATCH installation` (`started`);
  2. `GET installation` (what to check);
  3. `POST nodes` (enroll; the answer carries the cluster's secrets and the node's own token);
  4. `PUT dns`;
  5. AerolVM's `install.sh`;
  6. `GET cloud-storage` and the registry;
  7. the firewall;
  8. joining: on the seed, `cluster-init.sh` then `PUT bundles` and its first certificate; on the others, `awaiting_seed` polling `GET cluster`, then `cluster-join.sh` in two passes with `PUT certificate` between them;
  9. the metrics timer, then `online`.
- **Seed selection:** enrollment locks the cluster row, and the first node to enroll is the seed. If the seed fails, the next node to enroll becomes the seed and starts a new cluster with a new CA.
- **Preflight** before enrolling:
  - Linux, root, a supported architecture, Ubuntu 22.04+ or Debian 12+, systemd;
  - no AerolVM state already on the machine;
  - runtime needs: cgroup v2 for gVisor, `/dev/kvm` for Firecracker, `nvidia-smi -L` for NVIDIA, `/dev/kfd` plus Ubuntu for AMD;
  - ports free: TCP 7000–7002, 21212, 2019, 443 or 80, 5000 with storage; UDP 7001;
  - for joining nodes, the peers reachable;
  - firewalld not running.
- **Hard-won details:**
  - AerolVM's scripts run with stdin from `/dev/null`, as under `curl | bash` they'd swallow the rest of the script;
  - output goes through `hide_secrets`;
  - request bodies go to curl on stdin, not as arguments every process can read;
  - `DEBIAN_FRONTEND=noninteractive`, so apt doesn't prompt (`7c30ca6`);
  - `file` is installed before AerolVM's `install.sh` (F23);
  - failures are reported once, even from `$(…)` subshells, using a marker file.
- **Advanced settings on the command:** `--public-ip`, `--no-firewall`; name and subdomain saved on the installation (`0f58a35`).
- **Gaps:**
  - no resume after a failure: start a new installation;
  - "online" means `/health` answered and, for joiners, another node seen alive, not a membership check;
  - nodes installed before a change never get it (no self-update);
  - node removal is planned but not built (`docs/clusters.md` § *5. Remove a node*);
  - one public IP per node, so homelabs behind one router aren't supported.

## Goals

- One short command, still `curl … | sudo sh`, that works on Ubuntu and Debian, amd64 and arm64.
- The installer is `lewbox` itself (`lewbox node install`), so the code that installs a node is the code that keeps it up to date afterwards.
- Every step is idempotent, so a failed install can be **retried in place** with the same command rather than started again.
- Nothing secret on a command line, in logs, or in the dashboard.
- Removing a node is as easy as adding one.

### Non-goals

- Distros other than Ubuntu and Debian (AerolVM is tested on those), or non-systemd hosts.
- Provisioning the server itself (Terraform or cloud APIs). Projects bring their servers.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | The installation token and its rules (single use, expiry, cancel); the status machine; the node API order; seed selection; Lewbox signing join CSRs; the preflight checks; secrets kept out of output and argv; node name and subdomain chosen up front |
| **Change** | Bash → `lewbox node install` (Go). The served `install.sh` becomes a ~60-line bootstrap that downloads and verifies `lewbox`, then hands over. Timers → the node agent daemon. Failure → retry in place (new `lewbox node install --retry`, same token while the installation hasn't failed for good). "Online" → confirmed by AerolVM membership ([health-and-metrics](health-and-metrics.md#membership)). The registry config, firewall rules and timer settings move from install-time responses into the **node spec**, which can change later ([node-agent](node-agent.md#desired-state)) |
| **Add** | `lewbox node uninstall`, and Remove node in the dashboard. Installing behind NAT via the tunnel ([connectivity](connectivity.md), beta). A clock check (certificates and tokens need a sane clock). A dry-run preflight (`lewbox node check`) |
| **Drop** | The bash script's own firewall, metrics, certificate and cleanup scripts (`/usr/local/bin/lewbox-*`) and their timers |

## Design

### The command

```bash
curl -fsSL https://lewbox.example.com/install.sh | sudo sh -s -- --token lbi_…
```

`GET /install.sh` serves a small POSIX `sh` bootstrap, with the control plane's URL and the release's checksums written in. It:

1. Checks it's root on Linux, and maps `uname -m` to `amd64` or `arm64`.
2. Downloads `lewbox-linux-<arch>` from the control plane (`GET /downloads/lewbox/<version>/lewbox-linux-<arch>`, which serves the binary embedded in or next to the control plane, so nodes need no other host) and checks its SHA-256 against the one written into the script.
3. Installs it as `/usr/local/bin/lewbox`, and runs `lewbox node install --url <control plane> --token <token> [flags]` with stdin from `/dev/null`.

Anyone can read the bootstrap; the token is what authorises it. A project that doesn't pipe scripts to shells can download the binary and run `lewbox node install` directly; the docs show both.

Flags (`lewbox node install --help`):

| Flag | What |
|---|---|
| `--url` | The control plane (written in by the bootstrap) |
| `--token` | The installation token, `lbi_…` |
| `--public-ip` | The node's public IPv4/IPv6, when it differs from the address it uses outbound (1:1 NAT on cloud VMs) |
| `--no-firewall` | Leave the host's firewall alone |
| `--retry` | Carry on a failed attempt with the same token (see [Retrying](#retrying)) |
| `--yes` | Don't ask before changing the machine (the bootstrap passes it) |

Name, subdomain and connectivity are chosen in the dashboard, on the installation, so the command stays short.

### Statuses

| Status | Set by | Means |
|---|---|---|
| `pending` | Control plane | Token issued, nothing run yet |
| `started` | Installer | The installer called in |
| `checking` | Installer | Preflight running (new: so a long check shows) |
| `configuring_dns` | Control plane on enroll | Setting the node's DNS records (clusters with a domain) |
| `installing` | Control plane after DNS, or on enroll | Installing AerolVM and its dependencies |
| `provisioning_storage` | Installer | Installing the registry (clusters with storage) |
| `awaiting_seed` | Installer | A non-seed node waiting for the seed to be online |
| `joining` | Installer | Initialising (seed) or joining the cluster |
| `online` | Control plane, once membership confirms it | Finished. The node agent runs |
| `failed` | Installer or control plane | Stopped with `failure_reason` and `failed_step`. May be retried with `--retry` (unless the token has expired) |
| `cancelled` | Project | Cancelled from the dashboard; the token and any enrolled node's token stop working |

`online` is no longer the installer's word. The installer reports `joined`, and the control plane marks the installation `online` once the node appears alive in AerolVM's `GET /v1/cluster/members` and its first heartbeat has arrived ([health-and-metrics](health-and-metrics.md#membership)). A node with the wrong gossip key, or blocked cluster ports, is healthy on its own but never joins; legacy had to check this in bash.

### Steps

`internal/nodeagent/install` runs these in order, recording each in `/var/lib/lewbox/install.json` (step, result, time), so `--retry` starts at the first step not done:

1. **Start.** `PATCH /node/v0/installation {status: started}` with the installation token.
2. **Preflight.** `GET /node/v0/installation` returns what to check against (nothing secret): the cluster's architecture, isolation levels, GPU vendor, domain, storage, connectivity, and the AerolVM version and its asset checksums. Every problem is reported at once (`failed_step: preflight`, a list in `failure_reason`):
   - **OS:**
     - Linux;
     - Ubuntu 22.04+ or Debian 12+;
     - systemd running;
     - architecture matches the cluster;
     - at least 4 GB RAM and 40 GB free under `/var/lib` (configurable).
   - **Clock:** within 30 seconds of the control plane's `Date` header (certificates and tokens).
   - **Isolation levels and GPU:** the host must run at least one of the levels the cluster offers. Each level it can't run is reported as a warning, not a failure, and the node joins without it ([runtimes](runtimes.md#what-a-cluster-offers)):
     - `container`: Docker (always installable). The only level in the alpha;
     - `gvisor` (beta): cgroup v2;
     - `microvm` (later): a readable `/dev/kvm`;
     - NVIDIA: `nvidia-smi -L` lists a GPU;
     - AMD: `/dev/kfd` and Ubuntu.
   - **AerolVM release:** the cluster's version is at or above Lewbox's minimum fork release; otherwise `aerolvm_version_unsupported`, naming the minimum ([aerolvm-fork](aerolvm-fork.md#versions-in-lewbox)).
   - **A clean machine:** no AerolVM cluster state (`/etc/sandboxd/tls/node.crt`, Raft data), unless this is a `--retry` of this very node.
   - **Ports free** (skipped on retry once AerolVM is installed):
     - TCP 7000–7002, 21212, 2019;
     - 443 with a domain, or 80 without;
     - 5000 with storage;
     - UDP 7001.
   - **Firewall:** firewalld isn't running (unless `--no-firewall`).
   - **Outbound:** HTTPS to the control plane, the release host and the bucket's endpoint (with storage).

   `lewbox node check --url … --token …` runs only this, and changes nothing.
3. **Enroll.** `POST /node/v0/nodes` with host facts:
   - hostname, IPs, public IP;
   - architecture, OS, kernel, CPU model and cores;
   - memory, disk;
   - KVM and GPU presence.

   The control plane locks the cluster row, refuses an architecture mismatch or a public IP another live node in the cluster has (in direct mode), creates the `nodes` row, picks the seed, and answers once with:
   - the node (`id`, `name`, `is_seed`);
   - the **node token** (`lbn_…`, [ids](../foundation/ids-and-keys.md)), stored hashed;
   - the cluster's secrets: the PAT and gossip key; for non-seed nodes these come later, from `GET /node/v0/cluster`.

   The installer writes the node token to `/etc/lewbox/node-token` (`0600`) and the URL to `/etc/lewbox/node.json`. From here on, every call uses the node token; the installation token only reports status.
4. **DNS** (clusters with a domain, direct connectivity). `PUT /node/v0/dns`: the control plane creates `node-<id>.<domain>` and `*.node-<id>.<domain>` (or the chosen subdomain) pointing at the node's public IP ([pki-and-dns](pki-and-dns.md#records)). With the tunnel, the records point at the instance's relay instead.
5. **Spec.** `GET /node/v0/spec`: the node's desired state ([node-agent](node-agent.md#desired-state)):
   - AerolVM version and assets;
   - `sandboxd.env`;
   - runsc flags;
   - registry config;
   - firewall rules;
   - timers.

   Everything after this step is "apply the spec", the same code the agent runs later.
6. **Install AerolVM.**
   - Download the fork's assets for the cluster's version and verify `checksums.txt` and its signature ([aerolvm-fork](aerolvm-fork.md#release-assets)).
   - Run the fork's `install.sh` (see [AerolVM's own installer](#aerolvms-own-installer)) with stdin from `/dev/null`, output redacted, and flags from the spec: `--version`, `--pat-token-file` (a file, not an argument; a fork change), `--node-name`, `--public-host`, `--with-*-gpu`, and `--domain` with Cloudflare's zone-scoped token for Caddy's DNS-01, as legacy does (the alpha; DNS-01 through the control plane, F43, comes after it). `--with-gvisor` from the beta. The node agent doesn't install `file` first or patch anything around the script: the fork's `install.sh` is fixed instead (F23).
   - Then write the spec's `sandboxd.env` values and runsc flags.
7. **Registry** (clusters with storage). Report `provisioning_storage`; install CNCF Distribution and its config from the spec ([storage-and-registry](storage-and-registry.md#the-registry-on-each-node)); wait for `http://127.0.0.1:5000/v2/` (30 s).
8. **Firewall** (unless `--no-firewall`). Apply the spec's rules ([node-agent](node-agent.md#firewall)) before the node opens any cluster port: SSH allowed first, then default-deny.
9. **Join.**
   - **The seed:**
     1. Report `joining`.
     2. Run `cluster-init.sh --node-id <node id> --gossip-key-file …` with the advertise addresses.
     3. Upload `ca.crt`, `ca.key` and the credential key: `PUT /node/v0/bundles`. The control plane checks the key matches the certificate and the credential key is 32 bytes.
     4. Replace the self-signed node certificate with one from the control plane (`PUT /node/v0/certificate`, [pki-and-dns](pki-and-dns.md#node-certificates)).
   - **The others:**
     1. Report `awaiting_seed`.
     2. Long-poll `GET /node/v0/cluster?wait=60s` until `ready`, for up to 30 minutes. The answer carries `peers` (every online node's cluster address), `ca_certificate`, `credential_key` and the gossip key.
     3. Report `joining`.
     4. Run `cluster-join.sh` pass 1 (expect exit `2`, "CSR ready").
     5. `PUT /node/v0/certificate` with the CSR.
     6. Run pass 2 with `--signed-cert`.

     `check_peers` retries for 2 minutes, while the other nodes' agents let it through their firewalls.
10. **Start the agent.** Install and start `lewbox-node.service`; it connects, sends its first heartbeat, and the installer reports `joined`.
11. **Wait for online.** The installer waits up to 2 minutes for the control plane to confirm membership, then prints the node's dashboard link. If membership isn't confirmed, it reports `failed` with what the agent sees (members list, gossip errors from `journalctl -u sandboxd`).

### Seed selection

Unchanged from legacy, and done in the enroll transaction:

- The first node to enroll in a cluster without an online seed is the seed (`nodes.is_seed`).
- A seed that fails before uploading its bundles hands the role on: the next node to enroll becomes the seed and starts a new cluster with a new CA. Nodes waiting on the failed seed fail too, with "the seed failed: add this node again".
- After the bundles are uploaded, the seed is an ordinary member. Lewbox holds the CA, so nodes join through any online peer, and losing the seed matters only for Raft quorum.

### Retrying

A failure no longer means starting over:

- `lewbox node install --retry` (the same command with `--retry`) reads `/var/lib/lewbox/install.json` and carries on from the failed step, with the same installation token and, once enrolled, the node token.
- Allowed while the installation is `failed` with a retryable `failed_step`, and the token hasn't expired: 60 minutes since issue, extended while an attempt is running.
- **Not retryable:**
  - a seed that failed after `cluster-init.sh`, as its CA may differ from what was uploaded, so the node is reset;
  - an architecture mismatch;
  - a cancelled installation.
- `lewbox node install --reset` undoes a partial install: it stops sandboxd, removes AerolVM's state and Lewbox's files, and asks the control plane to delete the half-enrolled node. Then a new installation can run.

### AerolVM's own installer

AerolVM's `install.sh` installs Docker, gVisor, Caddy and sandboxd, with systemd units. Two ways to treat it:

1. **Keep running it** (recommended for the alpha). Upstream maintains it, and dependency changes reach us with each rebase. The fork fixes its bugs: F23 (`file`), F24 (merging `daemon.json`), PAT and token from files rather than arguments. The node agent wraps it, writes the spec on top, and re-asserts the spec afterwards.
2. **Port it into the node agent** later, if the fork's patches to it grow, or when we want installs without `curl | bash` on the node at all.

### Removing a node

New. From the dashboard (*Remove node*), the Admin API, or on the node (`lewbox node uninstall`):

1. **Drain.** The control plane calls AerolVM `POST /v1/cluster/nodes/{id}/drain`, so nothing new is placed there, and marks the node `draining`.
2. **Move its sandboxes off.**
   - Persisted sandboxes are stopped (offloaded to the registry) and, if they were running, started again elsewhere: the consumer sees a restart, with a `stop` then a `start` operation (reason `node_removed`).
   - Non-persisted sandboxes are deleted.
   - The project can choose **Wait** instead: no new sandboxes, and the node is removed once its last sandbox stops by itself.
3. **Leave the cluster.** `DELETE /v1/cluster/members/{id}` on another node, checking quorum first: refused if removing it would leave fewer than half the voters online, unless forced.
4. **Clean up the control plane:**
   - delete the node's DNS records;
   - revoke the node token;
   - remove it from the other nodes' firewall rules (next spec);
   - the certificate runs out within its lifetime, the only revocation AerolVM has.
5. **On the node** (if reachable): the agent runs AerolVM's `uninstall.sh`, removes the registry, Lewbox's firewall rules, `/etc/lewbox` and itself, then exits. An unreachable node is removed from Lewbox anyway, with a note to wipe the machine.

Statuses: `draining → removing → removed`. A removed node is kept, soft-deleted, for its history.

## API

### Admin API

Project-scoped; paths per [api](../api.md).

| Method and path | What |
|---|---|
| `POST /v0/projects/{prj}/clusters/{clu}/installations` | Start adding a node: `name`, `subdomain`, `connectivity` (`direct` or `tunnel`, default the cluster's). Answers with the installation, its token (shown again until it's used) and the command |
| `GET …/installations`, `GET …/installations/{ins}` | Progress: status, current step, `failure_reason`, `failed_step`, the node once enrolled, timestamps |
| `PATCH …/installations/{ins}` | Change `name` or `subdomain` before enrollment |
| `DELETE …/installations/{ins}` | Cancel; deletes a half-enrolled node |
| `POST /v0/projects/{prj}/nodes/{node}/removal` | Remove a node: `{mode: "move" \| "wait", force: false}`. `202` with the node `draining` |
| `DELETE /v0/projects/{prj}/nodes/{node}/removal` | Cancel a removal that's still draining (uncordon) |

Installation statuses changing also reach the dashboard live, and the project's webhooks as `node.installation.updated` ([activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)).

### Node API

`/node/v0`, separate from the public API, rate limited per token ([node-agent](node-agent.md#node-api)).

| Method and path | Token | What |
|---|---|---|
| `GET /install.sh` | none | The bootstrap |
| `GET /downloads/lewbox/{version}/lewbox-linux-{arch}` | none | The binary |
| `PATCH /node/v0/installation` | install | Report `started`, `checking`, `provisioning_storage`, `awaiting_seed`, `joining`, `joined`, `failed` (+ `failure_reason`, `failed_step`) |
| `GET /node/v0/installation` | install | What preflight checks against |
| `POST /node/v0/nodes` | install | Enroll; answers once with the node token |
| `PUT /node/v0/dns` | node | Create the node's DNS records |
| `GET /node/v0/spec` | node | The node's desired state |
| `PUT /node/v0/bundles` | node (seed) | Upload the CA, its key and the credential key |
| `GET /node/v0/cluster?wait=` | node | Long-poll until the seed is online; then peers, CA certificate, credential key, gossip key |
| `PUT /node/v0/certificate` | node | Sign a CSR as `node:<id>` ([pki-and-dns](pki-and-dns.md#node-certificates)) |
| `DELETE /node/v0/nodes/self` | node | Used by `--reset` and `uninstall` |

Errors keep the [API's shape](../api/errors.md):

| Status | Code | When |
|---|---|---|
| 401 | `invalid_token` | Unknown, cancelled, or a removed node's |
| 403 | `node_failed` | A node token whose installation failed (until `--retry`) |
| 409 | `installation_started`, `installation_finished`, `already_enrolled`, `seed_not_ready`, `cluster_has_no_bundles` | |
| 410 | `installation_expired` | |
| 422 | `architecture_mismatch`, `ip_in_use`, `ip_not_public`, `validation_failed` | |

## Security

- **The installation token** is single use, expires, and is shown only to the project. It's stored hashed and encrypted with the keyring ([foundation/secrets-and-encryption](../foundation/secrets-and-encryption.md)), so it can be shown again until used.
- **Secrets never on argv.**
  - The PAT, gossip key, DNS token and bucket keys are written to `0600` files and passed by path. The fork's scripts take `--*-file` flags.
  - The Go installer redacts every secret it knows from its own output and from the output of the scripts it runs, as `hide_secrets` did.
- **The bootstrap downloads `lewbox` from the control plane** over HTTPS and checks its SHA-256, which is written into the script, so a tampered download is refused. The fork's assets are checked against their signature as well.
- **The node id always comes from the control plane.** CSRs only supply a public key; names in them are ignored, so a node can't impersonate another over cluster mTLS.
- **The seed uploads `ca.key`.** Lewbox holds it so nodes can join and renew without the seed ([pki-and-dns](pki-and-dns.md#the-cluster-ca)). The project has root on its nodes, so this changes nothing about who controls the cluster.

## Observability

- **The dashboard's progress page** shows each step, its timing, and on failure the reason, the step, and the command to retry.
- **The installer's log** goes to `/var/log/lewbox-install.log` (redacted), and the last 200 lines are sent with a failure.
- **Metrics:**
  - `lewbox_node_installations_total{result}`;
  - install duration by step.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_INSTALL_TOKEN_TTL` | `60m` | Time to start an installation |
| `LEWBOX_NODE_MIN_MEMORY_MB`, `LEWBOX_NODE_MIN_DISK_GB` | `4096`, `40` | Preflight minimums |
| `LEWBOX_NODE_SEED_WAIT` | `30m` | How long joiners wait for the seed |

## Testing

- **Unit:**
  - each step against a fake node API, and against a fake host (an interface over the filesystem, systemd and exec);
  - preflight with each failure;
  - resuming from each step.
- **The control plane:**
  - the installation state machine, with every refusal;
  - seed selection under concurrent enrollments (two goroutines enrolling at once);
  - a failed seed handing over.
- **End to end (nightly):**
  - install a 3-node cluster on fresh VMs (Ubuntu 24.04 amd64, Debian 12 arm64) for Docker (gVisor from the beta);
  - remove one node and see its sandboxes move;
  - break a step (block port 7001) and retry after unblocking.

## Build plan

1. `GET /install.sh` bootstrap and binary downloads.
2. Node API for installations and enrollment; the state machine; seed selection.
3. `lewbox node install`: preflight, enroll, spec, AerolVM, registry, firewall, join, start agent. Built on the node agent's spec appliers ([node-agent](node-agent.md#build-plan)).
4. Membership-confirmed `online`.
5. `--retry`, `--reset`, `lewbox node check`.
6. Removal: drain, move, leave, clean up, uninstall.
7. Dashboard: Add node (requirements, command, progress), Remove node ([dashboard](../dashboard.md)).

## Open questions

1. **Retry rules.** Spike (M1): on a real seed and joiner, rerun each step after a failure (especially after `cluster-init.sh`) and record which are idempotent; the steps that aren't become *cancel and reinstall*.
2. ~~**Moving sandboxes on removal.**~~ Decided (2026-10-29): the project chooses per removal, `mode: move` (running persisted sandboxes restart elsewhere, and consumers see a restart) or `wait` (until they stop), as the removal API has.
3. ~~**Minimum hardware.**~~ Decided (2026-10-29): 4 GB RAM and 40 GB under `/var/lib` as the preflight floor, configurable (`LEWBOX_NODE_MIN_MEMORY_MB`, `LEWBOX_NODE_MIN_DISK_GB`).
4. **Distros.** Deferred (Later): RHEL-family once AerolVM's installer supports it; Debian and Ubuntu for the alpha.
5. ~~**AerolVM's `install.sh`.**~~ Decided (2026-10-29): keep running the fork's, fixed in the fork ([AerolVM's own installer](#aerolvms-own-installer)).
