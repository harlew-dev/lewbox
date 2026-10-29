# Health and metrics

> Status: Draft · Milestone: Alpha (M1); sandbox stats Beta (with F39) · Updated: 2026-10-29

Lewbox knows each node's health from three sources:

- **heartbeats** from its node agent;
- **AerolVM's membership,** read by the control plane;
- **whether calls to the node succeed.**

From these come each node's status, each cluster's health, what happens to sandboxes on a node that disappears, and the metrics the dashboard charts. The node agent also reports each sandbox's own CPU, memory and disk use, which Lewagent asked for and legacy never shipped.

## Today

- **Heartbeats** (`lewbox-legacy/docs/clusters.md` § *8. Health and metrics*):
  - Every minute (±10 s), `lewbox-metrics` checks AerolVM's `/health`, reads `/proc` and `df`, and sends `POST /api/node/v1/node/metrics`.
  - The fields are `aerolvm_healthy`, `cpu_percent`, `cpu_load_{1,5,15}min`, `memory_used_mb`/`memory_total_mb`, `disk_used_mb`/`disk_total_mb` (the filesystem holding `/var/lib`) and `uptime_seconds`.
  - A metric it can't read is sent as `null`. Failed heartbeats are dropped.
- **Statuses:**
  - A heartbeat sets an installed node `online` when AerolVM is healthy, or `offline` when it isn't.
  - `nodes:mark-offline` (every minute) marks nodes `offline` after 3 minutes without a heartbeat. Its overlap lock was still the default 24 hours (the bug that froze sign-ins when another task kept the same lock, `d63dea0`).
- **Samples:** `node_metrics`, one per heartbeat (~1,440 a day per node), pruned after 14 days. The node page charts 24 h, 2, 7 or 14 days, plus uptime bars per 4 h over 14 days; the cluster page charts usage (`a23ff98`, `6fe4af8`).
- **Cluster health** (`Cluster::health()`, computed, not stored) comes from members that finished installing:

| Health | When |
|---|---|
| `pending` | No member yet |
| `healthy` | All online |
| `degraded` | Some offline, a majority online |
| `error` | Some online, no majority: no Raft quorum |
| `offline` | None online |

  Health doesn't refuse anything: a create on an `error` cluster is reserved and fails later.
- **Sandboxes on an offline node** stay `running`, but show as Unreachable:
  - billing stops from the node's last heartbeat (`EndOfflineSandboxRuns`), and `apply_setup` skips them;
  - when the node is back, billing resumes and an `apply_setup` is queued (`ResumeNodeSandboxes`);
  - the reconciler reports any that didn't survive (`e6a5926`).
- **Not built:**
  - membership sync with AerolVM (`SyncClusterMembers`, planned);
  - charts beyond the basics;
  - notifying tenants when a node goes offline;
  - per-sandbox stats (`docs/todo.md` l.240–246).
- **"Online" at install** meant `/health` answered, which a node with a wrong gossip key also does.

## Goals

- A node's status reflects what matters: can the control plane reach it, and is it in its cluster?
- Detect problems within a minute and a half, and tell the project.
- Refuse work early on a cluster that can't do it (no quorum), with a clear error.
- Keep metrics cheap to store and fast to chart, for weeks.
- Per-sandbox resource use for consumers, without waking sandboxes.

### Non-goals

- A general monitoring system. Lewbox exposes Prometheus metrics for that.
- Alerting rules beyond Lewbox's own notifications.

## Keep, change, drop

| | What |
|---|---|
| **Keep** | Heartbeat fields and their care (nulls, `MemAvailable`, `/var/lib`); online/offline from AerolVM health; cluster health levels; sandboxes on offline nodes stay `running` but marked unreachable, with setup re-applied when back; 14-day retention |
| **Change** | Every 60 s → every 30 s over the agent's connection, buffered when offline. Offline after 3 min → 90 s. A per-minute scheduled command → derived from the agent's connection plus a leader job. Raw samples only → raw for 2 days, then 5-minute and hourly rollups. Health → also used to refuse creates and starts on a cluster with no quorum |
| **Add** | Membership sync. `unreachable` (agent up, but the control plane can't reach AerolVM). Draining and removal statuses. Per-sandbox stats. Notifications and webhooks for status changes. Prometheus metrics |
| **Drop** | Metering changes on offline nodes in the alpha (no credits until Beta). The rules carry over to [credits/metering](../credits/metering.md) |

## Design

### Node statuses

`nodes.status`:

| Status | Means | Set when |
|---|---|---|
| `installing` | Installation running | Enrolled |
| `online` | Connected, AerolVM healthy, an alive member of its cluster | Heartbeat healthy **and** membership alive |
| `degraded` | Connected, but AerolVM unhealthy, or not an alive member (e.g. gossip broken) | Heartbeat says unhealthy, or membership says not alive |
| `unreachable` | The agent is connected, but the control plane's AerolVM calls fail (DNS, TLS, firewall, tunnel stream refused) | Repeated call failures to the node |
| `offline` | No heartbeat for `LEWBOX_NODE_OFFLINE_AFTER` (90 s) | Leader job |
| `draining` | Being emptied for removal or upgrade | [installation](installation.md#removing-a-node), [node-agent](node-agent.md#upgrading-aerolvm) |
| `removing`, `removed` | Leaving the cluster; gone (soft-deleted) | |
| `failed` | Its installation failed | |

- **A node is *usable*** (sandboxes can be created on it, and calls to it are tried) in `online` and `degraded`. A degraded node may still run sandboxes; AerolVM decides placement ([placement-and-capacity](placement-and-capacity.md)).
- **Every status change** goes to `node_events` (the node's history on its page), and fires the webhook `node.status_changed` with `previous_status`, `status` and `reason`.

### Heartbeats

Sent every 30 s over the agent's connection ([node-agent](node-agent.md#heartbeats)):

```jsonc
{
  "sent_at": "2026-10-29T18:00:00Z",
  "aerolvm": { "healthy": true, "version": "v0.6.8-lewbox.2", "toolbox_version": "…" },
  "agent_version": "1.0.3",
  "host": {
    "cpu_percent": 23.4, "load": [1.52, 1.31, 1.08],
    "memory_used_mb": 18234, "memory_total_mb": 64298,
    "disk_used_mb": 211520, "disk_total_mb": 953869,      // the filesystem holding /var/lib
    "docker_disk_mb": { "images": 41230, "containers": 8120, "build_cache": 2200 },
    "uptime_seconds": 864210,
    "kernel": "6.8.0-45-generic", "cpu_model": "AMD EPYC 9454P"
  },
  "registry": { "healthy": true },
  "spec_version": 42,
  "sandboxes": [ { "aerolvm_id": "…", "cpu_percent": 12.0, "memory_used_mb": 1830, "disk_used_mb": 4120, "net_rx_bytes": 1234, "net_tx_bytes": 5678 } ],
  "stale_runtime_flags": 0
}
```

- **On the control plane,** a heartbeat:
  - updates `nodes.last_seen_at`, versions and health;
  - writes a sample row;
  - updates sandbox stats;
  - may change the node's status.

  Heartbeats arrive on whichever replica holds the connection, and writes are batched every few seconds.
- **Buffered heartbeats** (sent after a reconnect) are written as samples with their own `sent_at`, but don't change status. Only live ones do.
- **Validation:** a used value without its total, or more than its total, is dropped to `null`, as legacy did.

### Membership

A leader job, `cluster_members_sync`, runs every 30 s per cluster with a usable node. It calls AerolVM `GET /v1/cluster/members` on one usable node, and:

- marks a node not alive in membership as `degraded` (reason `not_a_member`), even while its own heartbeats are healthy;
- records each member's `role`, `capacity` (CPU, memory, disk, `supported_runtimes`, `gpu_count`, `gpu_vendor`) and `drained`, for [placement](placement-and-capacity.md);
- flags members Lewbox doesn't know (`unknown_member`, e.g. a node joined by hand), and nodes missing from membership. Both are shown on the cluster page, never acted on automatically;
- confirms installations: an installation that reported `joined` becomes `online` once its node is an alive member ([installation](installation.md#statuses)).

### Detecting failures from calls

The control plane's AerolVM client counts failures per node: connection refused, TLS errors, timeouts, and for tunnel nodes a missing tunnel. After 3 failures in 60 s with no success, the node becomes `unreachable`. One success, or a healthy membership check through another path, clears it.

This catches what heartbeats can't: a node whose agent is fine, but whose 443 is firewalled from the control plane.

### Cluster health

Computed from members: nodes that have finished installing and aren't `removed`. Stored on `clusters.health`, updated on every node status change, so lists and checks don't recompute it.

| Health | When | Effect |
|---|---|---|
| `pending` | No members | Creates refused: `503 cluster_unavailable` ("this cluster has no nodes yet") |
| `healthy` | All members usable | |
| `degraded` | Some not usable, a majority usable | Creates proceed |
| `no_quorum` | Some usable, not a majority | Creates and starts refused: `503 cluster_unavailable`, `reason: no_quorum`. Raft can't commit, so they'd fail anyway |
| `offline` | None usable | As above, `reason: offline` |

- **Was `error`; renamed `no_quorum`** to say what it means.
- **Refusing early is new.** It turns a confusing operation failure minutes later into an immediate answer.
- **Stops and deletes are still accepted,** and their operations retry until the cluster is back, as legacy's gateway did.

### Sandboxes on a node that's gone

When a node becomes `offline` or `unreachable`:

- **Its `running` sandboxes stay `running`,** with `reachable: false` on the sandbox ([sandboxes/lifecycle](../sandboxes/lifecycle.md)). Calls into them answer `503 cluster_unavailable` naming the node. A node that's unreachable isn't proof its sandboxes are gone.
- **The dashboard shows them as Unreachable.**
- **Webhooks:** `sandbox.unreachable`, and `sandbox.reachable` when the node is back.
- **When the node is usable again:**
  - the reconciler checks each of its sandboxes against AerolVM, and reports any that stopped or were lost ([sandboxes/reconciler](../sandboxes/reconciler.md));
  - the survivors get an `apply_setup`, so a profile change made meanwhile (an SSH key removed, a credential rotated) reaches them.
- **Metering** (Beta, the `credits` plugin, [credits/metering](../credits/metering.md)): metering pauses from `last_seen_at`, never before the run started, and resumes when the node is back.
- **After a long absence** (`LEWBOX_NODE_LOST_AFTER`, 24 h), the project is offered *Mark lost*:
  - **persisted sandboxes with a snapshot become `stopped`** with `status_reason: node_lost` (decided 2026-10-29), and start again from their last snapshot on another node. What they wrote since that snapshot is lost, and the API says so: the sandbox carries `status_reason: node_lost` and `last_snapshot_at`, and its `sandbox.stopped` webhook and activity event say the node was lost;
  - sandboxes that don't persist, or persist but have no snapshot yet, become `failed` with `status_reason: node_lost`, and can only be deleted;
  - never automatic, as the node may come back.

### Metrics storage

`node_metrics`, partitioned by day (Postgres native partitioning; [foundation/database](../foundation/database.md)):

| Column | Type |
|---|---|
| `node_id` | bigint |
| `at` | timestamptz |
| `aerolvm_healthy` | bool |
| `cpu_percent`, `load_1`, `load_5`, `load_15` | real, nullable |
| `memory_used_mb`, `memory_total_mb`, `disk_used_mb`, `disk_total_mb` | integer, nullable |
| `uptime_seconds` | bigint, nullable |

- **Raw samples** (every 30 s, ~2,880 a day per node) are kept for 2 days; dropping a day's partition is instant.
- **`node_metrics_5m`** (avg, max) is kept for 14 days; **`node_metrics_1h`** for 400 days. Each is computed by a leader job as each period closes.
- **Uptime** is derived from samples: a node is up in a bucket when it was usable for most of its samples. Gaps count as down.
- **Charts pick a table by range:** raw for ≤ 6 h, 5-minute for ≤ 14 days, hourly beyond. They answer at most ~500 points (a constant in the API, not a setting).

### Sandbox stats

**Beta, with the fork's F39.** Per sandbox, read from **outside** it, so reading never counts as activity ([F5](aerolvm-fork.md#process-and-exec-toolboxd)): the node agent calls the fork's `GET /v1/sandboxes/{id}/stats` (CPU, memory, writable-layer size, network) once a heartbeat ([F39](aerolvm-fork.md#api-clustering-and-lifecycle)). The agent doesn't read cgroups or `docker system df` itself: that was legacy's planned workaround for a gap in AerolVM, and the fork fills the gap instead ([D15](../README.md#decisions)). Until F39 is in the minimum release, sandboxes have no stats.

- **Mapping:** the agent reports by AerolVM id; the control plane maps it to `sbx_` ids.
- **Stored:**
  - the latest per sandbox in `sandbox_stats` (one row each, overwritten);
  - 5-minute rollups for 7 days in `sandbox_stats_5m`, for a sandbox page chart.
- **Served** on `GET /v0/sandboxes/{sbx}/stats` ([sandboxes/lifecycle](../sandboxes/lifecycle.md)):
  - `cpu_percent` is of the sandbox's own `cpu`, not the host's;
  - also `memory_used_mb`, `disk_used_mb`, `net_rx_bytes`, `net_tx_bytes`, `sampled_at`;
  - a stopped sandbox's are all `null`.

  Every key that can read the sandbox can read its stats.

### Notifications

Project notifications (dashboard, email to project admins, and webhooks: [activity-and-webhooks/webhooks](../activity-and-webhooks/webhooks.md)):

| Event | When |
|---|---|
| `node.offline`, `node.online` | Status changes, debounced: offline only after 90 s; online when back |
| `node.degraded` | Not a member, or AerolVM unhealthy, for 2 minutes |
| `node.unreachable` | |
| `cluster.health_changed` | Any change of cluster health |
| `node.disk_low` | Over 90% of `/var/lib` for 10 minutes |
| `node.certificate_expiring` | [pki-and-dns](pki-and-dns.md#renewal) |
| `node.spec_failed`, `node.upgrade_failed` | [node-agent](node-agent.md) |

One open notification per node per kind, until it resolves or is acknowledged.

### Prometheus

`GET /metrics` on the control plane (with a token, [foundation/observability](../foundation/observability.md)):

- `lewbox_node_status{project,cluster,node,status}`;
- `lewbox_cluster_health{project,cluster,health}`;
- `lewbox_node_cpu_percent`, `_memory_used_bytes`, `_disk_used_bytes` (latest);
- `lewbox_node_heartbeat_age_seconds`.

Instance operators can scrape fleet health without the dashboard. Per-project labels can be turned off for large instances (`LEWBOX_METRICS_PROJECT_LABELS=false`).

## API

Admin API (paths per [api](../api.md)):

| Method and path | What |
|---|---|
| `GET /v0/projects/{prj}/clusters/{clu}` | Includes `health`, member counts, and capacity totals |
| `GET …/clusters/{clu}/nodes`, `GET /v0/projects/{prj}/nodes/{node}` | Status, reason, `last_seen_at`, latest metrics, versions, membership (role, capacity, drained), certificate, connection |
| `GET …/nodes/{node}/metrics?from=&to=&resolution=` | Series for charts: `resolution` `raw`, `5m`, `1h`, or `auto` |
| `GET …/nodes/{node}/uptime?days=14&bucket=4h` | Uptime buckets |
| `GET …/clusters/{clu}/metrics?…` | Summed across the cluster's members |
| `GET …/nodes/{node}/events` | The node's status history and actions |
| `POST …/nodes/{node}/lost` | Mark a long-offline node lost (see above) |

Consumer API: `GET /v0/sandboxes/{sbx}/stats`.

## Security

- **Heartbeats are trusted only for their own node** (the node token).
- **Metrics reveal hardware and load,** so they're visible to the project, never to consumers, except a sandbox's own stats.
- **`/metrics` needs a token** (`LEWBOX_METRICS_TOKEN`) or a separate listen address on a private network.

## Observability

This plan is the observability for nodes. Its own health:

- heartbeat write latency;
- membership sync errors per cluster;
- rollup job lag.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_NODE_HEARTBEAT_INTERVAL` | `30s` | |
| `LEWBOX_NODE_OFFLINE_AFTER` | `90s` | |
| `LEWBOX_NODE_LOST_AFTER` | `24h` | When *Mark lost* is offered |
| `LEWBOX_MEMBERSHIP_SYNC_INTERVAL` | `30s` | |
| `LEWBOX_NODE_METRICS_RAW_RETENTION` | `48h` | |
| `LEWBOX_NODE_METRICS_5M_RETENTION` | `336h` | |
| `LEWBOX_NODE_METRICS_1H_RETENTION` | `9600h` | |

## Testing

- **Status machine:**
  - each transition from heartbeats, membership, call failures and timeouts;
  - debouncing;
  - buffered heartbeats not changing status.
- **Cluster health:** each level, and creates and starts refused on `no_quorum` and `offline` while stops are still accepted.
- **Offline sandboxes:**
  - `reachable: false`, and calls refused;
  - when the node is back: the reconciler run, then `apply_setup`.
- **Rollups and partitions:** dropping a day; charts choosing the right table.
- **Sandbox stats (beta):** the F39 response mapped to `sandbox_stats`; mapping AerolVM ids.
- **End to end:**
  - stop the agent → offline within 90 s, then back;
  - block 443 from the control plane → unreachable;
  - break a node's gossip → degraded (`not_a_member`).

## Build plan

1. **M1:**
   - heartbeats over the connection;
   - statuses;
   - the offline job;
   - cluster health with early refusals;
   - membership sync;
   - node events and notifications.
2. **M1:** `node_metrics` partitions, rollups, charts API; uptime.
3. **M2:** `unreachable` from call failures.
4. **M3:** *Mark lost*; Prometheus metrics.
5. **Beta:** metering pauses on offline nodes ([credits/metering](../credits/metering.md)); sandbox stats from the fork (F39: `sandbox_stats`, `GET /v0/sandboxes/{sbx}/stats`).

## Open questions

1. ~~**Thresholds.**~~ Decided (2026-10-29): 30 s heartbeats and 90 s offline, instance-wide settings for the alpha; per-cluster thresholds Later, if homelab nodes on flaky links need them.
2. ~~**Refusing creates on `no_quorum`.**~~ Decided (2026-10-29): refuse at once with `503 cluster_unavailable` (`reason: no_quorum`); the SDKs' retries cover a brief loss.
3. **Sandbox stats retention.** Deferred (Beta): with sandbox stats (F39); 7 days of 5-minute history is the starting point.
4. **gVisor stats.** Deferred (Beta): with gVisor.
