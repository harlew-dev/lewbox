# The node agent

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

`lewbox node` runs on every node as a systemd service, beside AerolVM. It keeps the node as the control plane wants it: AerolVM's version and settings, the registry, firewall rules, certificates, and DNS-01 challenges. It sends heartbeats with the node's and its sandboxes' metrics, cleans up what AerolVM leaves behind, and upgrades AerolVM and itself. For nodes behind NAT it also carries the tunnel ([connectivity](connectivity.md)). It replaces legacy's four bash scripts and their systemd timers, which could never be changed once a node was installed.

## Today

Legacy's install script writes four scripts and timers (`lewbox-legacy/docs/private/node/installed-nodes.md`, `docs/clusters.md` §§ 6–8). Each calls the app's node API with the node's token, read from `/etc/lewbox/node-token`:

| Timer | Every | Does |
|---|---|---|
| `lewbox-cert-renewal` | Day (`Persistent`) | Renews the node's cluster certificate when it has under 30 days left: CSR from the existing key → `PUT /api/node/v1/node/certificate` → checks the answer, then replaces `node.crt` in one rename |
| `lewbox-firewall` | Minute | `GET /api/node/v1/node/firewall` → ufw rules commented `lewbox`, added before old ones are removed, never touching others; refuses rules of unexpected shape; skips if unchanged |
| `lewbox-metrics` | Minute (±10 s) | Checks AerolVM's `/health`, reads `/proc` and `df`, `POST /api/node/v1/node/metrics`. No retry or buffering |
| `lewbox-snapshot-cleanup` | Minute | Removes resume-pulled snapshot images no container uses, older than 5 minutes (AerolVM never does, [F16](aerolvm-fork.md#snapshots-resume-and-the-registry)) |

What went wrong with this approach:

- **Nothing updates a node after it's installed.** Every improvement needed a by-hand fix on old nodes:
  - the TCP port pool in the firewall, which the old script refused (`docs/todo.md` l.74);
  - the snapshot cleanup timer, on nodes from before it (l.45);
  - `SB_IMAGE_BUILD_TIMEOUT` and `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL` (l.49, 111);
  - the gVisor overlay and host runtimes (`docs/examples/fix-gvisor-*.sh`);
  - nodes from before heartbeats "need reinstalling" (`docs/clusters.md` § 8).
- **Polling:** the firewall picks up a new node within a minute at best, and the app has no way to tell a node anything sooner.
- **Settings are frozen at enroll time.** Timer intervals and AerolVM env come from config when the node enrolls.
- **No upgrade path** for AerolVM itself: "Upgrading existing clusters is a later, separate flow".
- **Per-sandbox stats were planned** as another script change (`docs/todo.md` l.244), which old nodes would never get.

## Goals

1. One long-running daemon, `lewbox node`, from the same binary as the control plane, that brings the node to a **desired state** the control plane holds, and keeps it there.
2. Changes reach every node within seconds, with no person logging in.
3. The agent can only do a fixed set of things. The control plane can't make it run arbitrary commands.
4. AerolVM and the agent itself upgrade in place, verified and reversible, one node at a time.
5. If the control plane is unreachable, the node carries on as it is: sandboxes keep running, certificates keep renewing as long as they can, and metrics are buffered.

### Non-goals

- Running sandboxes. AerolVM does that.
- Being reachable from outside. The agent only dials out.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Every timer's job, and its care: certificates checked against the CA and key before replacing; firewall rules added before old ones go, tagged and never touching others; SSH always allowed first; metrics from `/proc` with nulls for what can't be read; cleanup grace periods |
| **Change** | Four bash scripts and timers → one Go daemon. Poll every minute → a persistent connection with pushes, and long-polling as a fallback. Settings fixed at enroll → a versioned node spec. "Reinstall to get the fix" → apply the new spec |
| **Add** | Upgrades of AerolVM and the agent; drain handling; per-sandbox stats; delegated DNS-01; the tunnel; buffered metrics; a local status command |
| **Drop** | `/usr/local/bin/lewbox-{cert-renewal,firewall,metrics,snapshot-cleanup}` and their units. On upgrade from a legacy node, the agent removes them ([migration](../migration.md)) |

## Design

### Process

- **Binary:** `/usr/local/bin/lewbox` (the same binary as the control plane, [foundation/binary-and-commands](../foundation/binary-and-commands.md)).
- **Service:** `lewbox-node.service` runs `lewbox node run`.
- **Runs as root,** because it writes `/etc/sandboxd`, `/etc/docker/daemon.json` and firewall rules, and runs AerolVM's scripts. systemd hardening narrows that:
  - `ProtectSystem=strict`, with `ReadWritePaths=/etc/sandboxd /etc/docker /etc/lewbox /var/lib/lewbox /usr/local/bin /etc/ufw /run`;
  - `ProtectHome=yes`, `PrivateTmp=yes`;
  - `NoNewPrivileges=yes`, `RestrictSUIDSGID=yes`;
  - `CapabilityBoundingSet` limited to what ufw, docker and systemctl need.
- **Files:**

| Path | What |
|---|---|
| `/etc/lewbox/node.json` | Control plane URL, node id, cluster id |
| `/etc/lewbox/node-token` | `lbn_…`, `0600` |
| `/var/lib/lewbox/spec.json` | The last spec applied, and its version |
| `/var/lib/lewbox/metrics/` | Buffered heartbeats while offline (capped, e.g. 24 h) |
| `/var/lib/lewbox/install.json` | Install progress ([installation](installation.md#retrying)) |

- **Logs:** JSON to journald, redacted. `lewbox node status` prints the connection, spec version, the result of the last apply, AerolVM health and versions, certificate expiry and firewall state, for someone on the node.

### Connection

The agent holds one connection to the control plane: a WebSocket at `wss://<control plane>/node/v0/connect`, authenticated with the node token.

```
agent ──── hello {node, agent_version, aerolvm_version, spec_version, capabilities} ───▶ control plane
      ◀─── spec {version, …} (when newer)
      ──── heartbeat {metrics, sandboxes, health} every 30 s ───▶
      ──── event {seq, type, sandbox, at, …} as AerolVM emits them ───▶   (lifecycle events)
      ◀─── event_ack {seq}
      ◀─── action {id, kind, args}                      (drain, upgrade, cleanup, renew now…)
      ──── action_result {id, ok, error, output} ───▶
      ◀─── acme_challenge_result …                      (DNS-01, see pki-and-dns)
      ◀═══ tunnel streams (yamux), tunnel mode only ═══▶
```

- **One connection per node, to any control-plane replica.** The replica records it in `node_connections` (replica id, connected at), and other replicas send it messages through Postgres `LISTEN/NOTIFY` ([foundation/jobs-and-scheduling](../foundation/jobs-and-scheduling.md)). A node that loses its replica reconnects to another.
- **Reconnects** back off from 1 s to 60 s, with jitter.
- **If WebSockets can't get through** (a corporate proxy), the agent falls back to HTTPS long-polling: `GET /node/v0/spec?after=<version>&wait=60s`, `POST /node/v0/heartbeats`, and `GET /node/v0/actions?wait=60s`. The tunnel needs the WebSocket.
- **Messages** are JSON, one per WebSocket text frame; tunnel streams are binary frames on a multiplexer ([connectivity](connectivity.md#the-tunnel)).

### Desired state

The control plane computes each node's **spec** from its cluster, its project and the instance's defaults. Every change to an input bumps the spec version, and the new spec is pushed to connected nodes.

```jsonc
{
  "version": 42,
  "aerolvm": {
    "version": "v0.6.8-lewbox.2",
    "assets": { "base_url": "…", "checksums_sig": "…" },
    "install_flags": ["--with-gvisor"],
    "env": {
      "SB_CONTAINER_RUNTIME": "gvisor",
      "SB_HOST_RUNTIMES": "docker,gvisor",
      "SB_L4_PORT_RANGE_START": "22000", "SB_L4_PORT_RANGE_END": "23000",
      "SB_IMAGE_BUILD_TIMEOUT": "20m",
      "SB_SNAPSHOT_PUSH_ENABLED": "true",
      "SB_MIRROR_PUSH_HOST": "127.0.0.1:5000",
      "SB_IMAGE_DISTRIBUTION_AOCR_HOST": "127.0.0.1:5000",
      "SB_AUTO_IMPORT_CLUSTER_ID": "clu_abc…",            // lowercased (F11)
      "SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL": "30s",
      "SB_SNAPSHOT_PUSH_MAX_IN_FLIGHT": "2",
      "SB_HOST_DISK_GB": "900", "SB_DISK_RESERVATION_RATIO": "1.0"
    },
    "runsc_flags": ["--host-uds=open", "--overlay2=none"],
    "firecracker": null
  },
  "registry": { "version": "3.1.2", "sha256": { "amd64": "…", "arm64": "…" }, "config": { /* storage, see storage-and-registry */ } },
  "firewall": { "enabled": true, "public": [ … ], "public_ranges": [ … ], "cluster": [ … ], "nodes": ["203.0.113.10", …] },
  "certificates": { "renew_within": "720h" },
  "cleanup": { "snapshot_images": { "interval": "60s", "grace": "300s" } },
  "heartbeat": { "interval": "30s", "sandbox_stats": true },
  "agent": { "version": "1.0.3", "sha256": { … } },
  "upgrade_window": null
}
```

- **Isolation levels are per node.** The cluster offers levels ([runtimes](runtimes.md#what-a-cluster-offers)); each node runs the ones its host can. The agent reports what the host supports in its preflight and heartbeats (`host_isolation`: Docker always; `gvisor` with cgroup v2; `microvm` with a readable `/dev/kvm`), and the control plane renders each node's spec from the cluster's levels intersected with the host's. So `SB_HOST_RUNTIMES` is per node (`docker,gvisor` on a node offering `container` and `gvisor`; `docker,gvisor,firecracker` on a KVM node offering all three; Docker is always listed, for builds, F22), `SB_CONTAINER_RUNTIME` is the cluster's default where the node runs it, `install_flags` add `--with-gvisor` only where needed, and `firecracker` (artifacts and pinned versions) is set only on nodes running `microvm`. The node records the levels it ended up running as `isolation_levels`; adding a level to a cluster installs its runtime on the nodes that can run it, at the next spec.
- **Secrets aren't in the spec:** the PAT, gossip key, bucket keys and DNS token. They're sent once at install, or by an action when they change, and written to `0600` files. The spec only names their files and versions, so a spec in a log reveals nothing.
- **Applying** goes in a fixed order, each step idempotent, each comparing what's there with what's wanted, and changing only what differs:
  1. agent version (self-update first, so the rest runs with the code that understands the spec);
  2. AerolVM version;
  3. `sandboxd.env`;
  4. runsc flags;
  5. registry;
  6. firewall;
  7. certificate settings;
  8. cleanup;
  9. heartbeat settings.
- **What needs a restart:**

| Change | Takes effect by |
|---|---|
| A `sandboxd.env` change | `systemctl restart sandboxd`. Docker and gVisor containers keep running across a sandboxd restart; we confirm this in the fork's upgrade test, and for Firecracker VMs too (open question 2). Restarts are spread out within a cluster, at most one node at a time (a lock in the control plane) |
| runsc flags | `systemctl reload docker` (SIGHUP), which doesn't touch running containers. Only sandboxes started afterwards get them, and the agent reports how many running sandboxes don't have them yet |

- **The result is reported:** `spec_applied {version, ok, steps: [{name, changed, error}]}`. The dashboard shows each node's spec version and the last result; a node more than one version behind for 10 minutes raises a `node.spec_failed` notification.
- **The spec replaces every hand-run fix** from legacy: gVisor host runtimes, the overlay flag, the TCP pool rule, build timeouts, push intervals, the cleanup timer.

### Firewall

The same model as legacy (`Cluster::firewallRules()`), applied by the agent with ufw:

| Open to | Ports |
|---|---|
| The internet | SSH on every port `sshd -T` reports (22 if it can't tell); 443 with a domain, or 80 (and 21212, development only) without; the sandbox TCP port pool (22000–23000) |
| The cluster's nodes | TCP 7000, 7001, 7002; UDP 7001; TCP 21212 with a domain |
| Tunnel mode | Nothing inbound from the internet but SSH (and nothing at all if the project chooses). Cluster ports from the nodes' LAN addresses |

- Rules are checked against expected shapes before they become ufw arguments, tagged with comment `lewbox`, and others' rules are never touched.
- New rules are added before old ones are removed. ufw's defaults: deny incoming, allow outgoing.
- **Pushed, not polled:** a node enrolling, failing or being removed changes every other node's spec, so its peers let it in, or shut it out, within seconds.
- `firewall.enabled: false` (the install's `--no-firewall`, changeable later in the dashboard) leaves the host's firewall alone, and the dashboard lists the ports the project must open themselves.
- Still to verify (from legacy): that default-deny doesn't break what sandboxes need from the host over Docker or CNI bridges; that ufw's forward policy doesn't touch Docker's own rules.

### Certificates

[pki-and-dns](pki-and-dns.md#renewal) has the detail. The agent:

- checks `/etc/sandboxd/tls/node.crt` every hour, and renews at `renew_within` (30 days before it expires);
- makes a CSR from the existing `node.key` and sends it over the connection (or `PUT /node/v0/certificate`);
- verifies the certificate chains to the cluster CA, names `node:<id>`, and matches `node.key`, then renames it into place. AerolVM picks it up on its next connection.

Renewals are retried every hour on failure (not once a day), with a `certificate_expiring` notification at 14 days.

### Heartbeats

Every 30 s (`heartbeat.interval`, was 60 s), [health-and-metrics](health-and-metrics.md) has the fields:

- **AerolVM health:** `GET http://127.0.0.1:21212/health`, within 5 s.
- **Host metrics:** CPU % since the last heartbeat; load averages; memory used (`MemTotal − MemAvailable`) and total; disk used and total under `/var/lib`; uptime.
- **Versions:** AerolVM, the agent, runsc and Firecracker; kernel; CPU model.
- **Per-sandbox stats** ([health-and-metrics](health-and-metrics.md#sandbox-stats)), read from cgroups and Docker. Never from the toolbox, as every toolbox request counts as activity ([F5](aerolvm-fork.md#process-and-exec-toolboxd)).
- **Spec version**, and how many running sandboxes predate the current runsc flags.

While disconnected, heartbeats are buffered on disk, up to 24 h at one sample a minute, and sent when the connection is back, so the charts don't have gaps.

### Lifecycle events

AerolVM's events (fork changes F35 and F50, [aerolvm-fork](aerolvm-fork.md#api-clustering-and-lifecycle)) reach the control plane through the agent, so a sandbox's timers, stops and session ends are known in seconds without the control plane holding a stream open to every node:

- The agent subscribes to AerolVM's local event socket (the Unix socket F50 adds; `GET /v1/events` on `127.0.0.1` otherwise), resuming from the last sequence number it acknowledged.
- It forwards each event as an `event` message on its connection: `idle_warning`, `idle`, `lifetime_reached`, `started`, `stopped` (with `reason`, `at`, `backstop`, and the pre-stop script's result), `destroyed`, `error`, `session_ended`, `snapshot_pushed`. The control plane answers `event_ack` once it has recorded it ([sandboxes/reconciler](../sandboxes/reconciler.md#events), [sandboxes/timers-and-activity](../sandboxes/timers-and-activity.md#lewbox-decides-when-to-stop)).
- **While it can't reach the control plane** it keeps unacknowledged events in an append-only file under `/var/lib/lewbox/events/` (up to 10,000 events or 7 days, oldest dropped first, with a counter of dropped ones), and sends them in order on reconnect. Meanwhile AerolVM's backstop still stops sandboxes whose timers ran out, so nothing runs forever.
- Without the WebSocket, it posts batches to `POST /node/v0/events`.
- Events are at-least-once; the control plane deduplicates by `(node, seq)` and applies them with the reconciler's preconditions.

### Snapshot image cleanup

Until the fork removes resume-pulled snapshot images itself ([F16](aerolvm-fork.md#snapshots-resume-and-the-registry)):

- every `cleanup.snapshot_images.interval` (60 s), the agent lists images under `127.0.0.1:5000/cluster/<id>/snapshots/`;
- it removes those no container uses, running or stopped, that were tagged more than `grace` (5 minutes) ago, so a resume or an upload in progress isn't caught;
- AerolVM's own snapshots, base images and template images are never touched.

It's turned off by the spec once every node in the cluster runs a fork release with F16.

The agent also reports the node's Docker disk use (images, containers, build cache), so a full disk can be seen before it fails a create.

### Actions

The control plane can ask for one of a fixed set of actions. Anything else is refused, and there is no "run this shell command".

| Action | What |
|---|---|
| `drain`, `uncordon` | AerolVM drain or uncordon on this node (also callable through the API by the control plane directly) |
| `upgrade_aerolvm` | Upgrade now, outside the window ([below](#upgrading-aerolvm)) |
| `upgrade_agent` | Upgrade the agent now |
| `renew_certificate` | Renew now (`--force`) |
| `rotate_secret` | Write a new PAT, gossip key, bucket keys or DNS token, then restart what uses it |
| `cleanup_images` | Run snapshot cleanup now |
| `collect_diagnostics` | Bundle: the last 500 lines of the sandboxd, docker, caddy, registry and agent journals, `sandboxd.env` with secrets redacted, versions, `docker info`, `ufw status`. Sent to the control plane for the project (and support) to read |
| `uninstall` | Remove the node ([installation](installation.md#removing-a-node)) |

Each action is recorded in the node's activity (who asked, result), shown on the node page.

### Upgrading AerolVM

A cluster has a target AerolVM version ([aerolvm-fork](aerolvm-fork.md#versions-in-lewbox)). When the target changes, by the project or by the instance moving a deprecated release on:

1. **The control plane rolls the cluster, one node at a time:**
   - it waits for an **upgrade window**, if the project set one (e.g. Sundays 02:00–05:00 UTC); otherwise it starts at once;
   - it upgrades non-seed and non-leader nodes first, the Raft leader last;
   - it checks quorum before each node: never take a node down if fewer than a majority of voters would be left.
2. **On each node:**
   1. download and verify the assets;
   2. drain the node (no new placements; running sandboxes stay);
   3. install the new sandboxd and toolboxd (the fork's `install.sh --version`, or replace the binaries);
   4. restart sandboxd;
   5. wait for `/health`, membership, and the node's sandboxes listed again;
   6. uncordon.

   Running sandboxes keep their old toolboxd until they restart, as it's bind-mounted at start. The node reports both versions.
3. **On failure** (health doesn't come back within 2 minutes), the agent puts the previous binaries back, restarts, and reports. The roll stops, and the project is notified.
4. **The cluster's `Features`** move to the new release once every node runs it.

### Upgrading the agent

- The spec's `agent.version` and checksums say what each node should run. The control plane offers its own version by default, so nodes follow the control plane.
- **Self-update:**
  1. download from the control plane;
  2. verify the SHA-256 and signature;
  3. write `/usr/local/bin/lewbox.new`;
  4. run `lewbox.new node self-check`, which parses the config and connects;
  5. rename over `/usr/local/bin/lewbox`;
  6. `systemctl restart lewbox-node`.

  If the new agent doesn't connect within 60 s, a `lewbox-node-rollback` path unit restores `lewbox.old`.
- **Version skew:** the control plane accepts agents one minor version behind, and refuses older ones with a message to upgrade. Agents ignore spec fields they don't know, and report them.

### Node API

`/node/v0` on the control plane, apart from the public API:

| Path | What |
|---|---|
| `GET /node/v0/connect` | The WebSocket |
| `GET /node/v0/spec?after=&wait=` | Long-poll fallback |
| `POST /node/v0/heartbeats` | Fallback; accepts a batch (buffered heartbeats) |
| `POST /node/v0/events` | Fallback; a batch of [lifecycle events](#lifecycle-events), in order |
| `GET /node/v0/actions?wait=`, `POST /node/v0/actions/{id}/result` | Fallback |
| `PUT /node/v0/certificate` | Fallback |
| `POST /node/v0/acme-challenges`, `DELETE …/{id}` | Delegated DNS-01 ([pki-and-dns](pki-and-dns.md#dns-01-through-the-control-plane)) |
| `POST /node/v0/diagnostics` | Upload a diagnostics bundle |

Plus the installation endpoints in [installation](installation.md#node-api).

- **Authentication:** `Authorization: Bearer lbn_…`, looked up by hash. A removed node's token answers `401 invalid_token`; a failed node's `403 node_failed`.
- **Rate limits:** per token, 120 requests a minute. Per address, 1,200 a minute, counting unknown tokens, so tokens can't be guessed quickly. Counting by token means nodes behind one NAT each keep their own limit (legacy `0c244e2`).

## Security

- **A fixed action set.** A compromised control plane can already do anything to a cluster through its PAT, so this isn't a hard boundary. But it keeps a bug from turning into remote command execution, and it makes what the agent does auditable.
- **Signed downloads.** The agent installs only binaries whose checksums are signed by the release key compiled into it, or served by the control plane over TLS with checksums in the spec. Air-gapped instances mirror assets and keep the signatures.
- **Secrets on the node:** the PAT, gossip key, credential key, bucket keys and (only without delegated DNS-01) the DNS token. The project has root on its own nodes, so this is acceptable. Lewbox's own credentials never go to a node.
- **Node token rotation:** `rotate_secret` can replace the node token too, on demand and every 90 days. The old token keeps working for 10 minutes after the new one is sent.
- The agent never logs secrets. Diagnostics bundles are redacted with the same list.

## Observability

- **Node page:**
  - connection (connected since, replica);
  - spec version and last apply result;
  - versions;
  - certificate expiry;
  - firewall state;
  - last heartbeat;
  - actions and their results.
- **Notifications and webhooks:**
  - `node.offline`, `node.online`;
  - `node.spec_failed`;
  - `node.certificate_expiring`;
  - `node.upgrade_failed`;
  - `node.disk_low` (over 90%).
- **Metrics:**
  - `lewbox_node_connected{cluster}`;
  - `lewbox_node_spec_lag`;
  - `lewbox_node_heartbeat_age_seconds`;
  - `lewbox_node_actions_total{kind,result}`.

## Configuration

Settings flow to nodes through the spec, so they can change after install:

| Setting | Default | What |
|---|---|---|
| `LEWBOX_NODE_HEARTBEAT_INTERVAL` | `30s` | |
| `LEWBOX_NODE_OFFLINE_AFTER` | `90s` | No heartbeat for this long marks a node offline |
| `LEWBOX_CERTIFICATE_LIFETIME` | `2160h` (90 days) | See [pki-and-dns](pki-and-dns.md) |
| `LEWBOX_CERTIFICATE_RENEW_WITHIN` | `720h` | |
| `LEWBOX_SNAPSHOT_CLEANUP_INTERVAL`, `…_GRACE` | `60s`, `300s` | |
| `LEWBOX_AEROLVM_BUILD_TIMEOUT` | `20m` | `SB_IMAGE_BUILD_TIMEOUT` |
| `LEWBOX_SNAPSHOT_PUSH_RETRY` | `30s` | `SB_SNAPSHOT_PUSH_RECONCILE_INTERVAL` |
| `LEWBOX_SANDBOX_TCP_PORTS` | `22000-23000` | |

Per cluster:

- upgrade window;
- firewall on or off;
- target AerolVM version.

## Testing

- **Each applier against a fake host:** a temporary root, a fake systemctl and ufw, and the fake AerolVM. Each one is run to be idempotent: twice gives no change the second time.
- **Spec migration:** a legacy node (its timers and files present) upgraded to the agent removes the old scripts and applies the spec.
- **Connection:** reconnects; replica failover; the long-poll fallback; buffered heartbeats replayed in order.
- **Upgrades:** an AerolVM upgrade with a health failure rolls back; the agent's self-update with a bad binary rolls back; skew refusal.
- **Security:** an unknown action is refused; a spec with a malformed firewall rule is refused, and nothing is changed.
- **End to end (nightly):** roll a 3-node cluster to a new fork release with sandboxes running and a terminal attached throughout.

## Build plan

1. **M1:**
   - `lewbox node run` with the WebSocket connection, hello and heartbeats (host metrics);
   - the spec, with appliers for AerolVM env, runsc flags, firewall and certificates;
   - `node_connections` and pushes across replicas.
2. **M1:** snapshot cleanup; long-poll fallback; `lewbox node status`.
3. **M2:** per-sandbox stats; diagnostics; actions.
4. **M3:** AerolVM rolling upgrades; agent self-update; upgrade windows.
5. **M4:** delegated DNS-01 ([pki-and-dns](pki-and-dns.md)); tunnel mode ([connectivity](connectivity.md)).
6. **Beta:** migration of legacy nodes ([migration](../migration.md)).

## Open questions

1. **WebSocket or gRPC** for the agent's connection? WebSocket goes through more proxies and matches the rest of Lewbox; gRPC gives typed streams.
2. **Restarting sandboxd for env changes:** confirm on each runtime that running sandboxes survive it, especially Firecracker VMs.
3. **Upgrade windows:** per cluster only, or also a "do it now" for security releases that skips windows?
4. **Who may trigger `collect_diagnostics`:** project admins only? It can include hostnames and IPs, but no secrets.
5. **Should the agent also run on non-AerolVM hosts** (e.g. a relay host for the tunnel)? See [connectivity](connectivity.md#open-questions).
