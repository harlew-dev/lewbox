# Placement and capacity

> Status: Draft · Milestone: Alpha (M1) · Updated: 2026-10-29

Placement is where a sandbox runs; capacity is whether it fits. AerolVM's scheduler places sandboxes on a cluster's nodes, and Lewbox lets it. What Lewbox owns:

- which node it sends a request to;
- what happens when a cluster is full;
- how much a node may be overcommitted;
- the cluster's status;
- showing the project how full each cluster is.

## Today

- **AerolVM places, Lewbox calls any node.** Any node accepts cluster-wide calls (create, list, build) and places the work, forwarding writes to the Raft leader on 21212. Calls about one sandbox are forwarded to its owner (`clusterForwardWrap`). The gateway prefers the node the app records, found with `GET /v1/cluster/sandbox-index` (`lewbox-gateway-legacy/docs/aerolvm.md` § *Clusters and nodes*).
- **Capacity is refused, not queued,** with words, not a code ([F34](aerolvm-fork.md#api-clustering-and-lifecycle)): `503 host capacity exceeded: [live memory floor breached (…)]`, `cluster: target capacity exceeded after pending reservations`, `cluster: no worker placement target available`. The gateway matches those words anywhere in the message (`e1573fa`), retries for `GATEWAY_CAPACITY_WAIT` (2 min), then fails the operation with `cluster_capacity_exceeded`. Consumers see `503 cluster_capacity_exceeded` ("after 2 minutes of trying. A smaller size, or a later try, may fit").
- **Resources per sandbox** (`cpu`, `memory_mb`, `disk_gb`) are set at create, and enforced by the node.
- **Disk placement is off** (`SB_DISK_RESERVATION_RATIO=0`), so placement counts CPU and memory only, and disk only by declared sizes when on (`docs/clusters.md` § *Install script settings*).
- **Image builds are placed like a default-sized sandbox,** only on nodes listing `docker` (F22).
- **Firecracker templates** (later, with `microvm`): placement prefers nodes that have them cached.
- **Node roles:** `mixed` (the default) is supported up to 10 live nodes; larger clusters need dedicated `server`, `worker` and `ingress` nodes. Voters should be odd (3, 5, 7).
- **Cluster status:** `active` or `suspended`. A suspended cluster refuses creates and starts (`409 cluster_suspended`), and is never skipped for another.
- **Lewagent's asks** (`lewagent-legacy/docs/todo.md`): "Memory overprovision factor 1 per cluster", "limits per cluster", and later "moving a persistent sandbox to another cluster".

## Goals

- Let AerolVM place, but make Lewbox's handling of a full cluster fair (first come, first served) and fast (woken when capacity frees, not polling).
- Refuse impossible requests at once: bigger than any node.
- Per-cluster overcommit settings for CPU, memory and disk, applied to every node.
- A clear view of each cluster's capacity: total, reserved, used, and per node.
- Node roles for clusters past 10 nodes.

### Non-goals

- Our own scheduler. AerolVM's knows each node's live state; ours would only race it.
- Affinity rules (sandboxes together or apart). Later, if AerolVM grows them.
- Moving running sandboxes between nodes live. A move is a stop and a start ([sandboxes/persistence](../sandboxes/persistence.md)).

## Keep, change, drop

| | What |
|---|---|
| **Keep** | AerolVM places, by runtime among the nodes that run it; capacity retried for 2 minutes, then `cluster_capacity_exceeded`; `cluster_suspended` refusing creates and starts, never skipped |
| **Change** | Retrying each create on its own every few seconds → a **per-cluster capacity queue**, woken by stops and deletes, retried in order. Capacity recognised by words → by the fork's `code` (F34, P0); no phrase matching. Disk placement → on, from each node's disk |
| **Add** | Overcommit settings per cluster. An "impossible size" check. Capacity on the cluster page and API. Node roles at install. Entry-node choice that avoids unusable nodes |
| **Drop** | `sandbox-index` lookups: the fork names a sandbox's node (F37, P0). Phrase matching on capacity errors (F34) |

## Design

### Choosing the node to call

- **Cluster-wide calls** (create, build, list, members): any *usable* node ([health-and-metrics](health-and-metrics.md#node-statuses)), chosen round-robin. For a create or start, only nodes that run the sandbox's [isolation level](runtimes.md#what-a-cluster-offers) are candidates, and builds go to nodes with Docker. A node that fails is skipped for 30 s, and the next one tried, so one bad node can't stall creates.
- **Calls about one sandbox:** the node recorded on the sandbox (`sandboxes.node_id`, from create and resume). If that node isn't usable, the call fails with `503 cluster_unavailable` rather than being forwarded through another node. AerolVM would forward it to the owner anyway, and the owner is the one that's down. When the fork names a sandbox's node (F37), the recorded node is checked against AerolVM's answer on each create and resume.
- **Tunnel nodes** (beta) are reached through their tunnel ([connectivity](connectivity.md)); this is the client's `Dialer`, invisible here.

### When a cluster is full

A create or start that AerolVM refuses for capacity:

1. **The operation enters the cluster's capacity queue:**
   - `capacity_waits(operation_id, cluster_id, cpu, memory_mb, disk_gb, gpu, enqueued_at, deadline)`;
   - its step becomes `waiting_for_capacity`, which consumers see on the operation;
   - its `deadline` is `enqueued_at + LEWBOX_CAPACITY_WAIT` (2 min).
2. **Waiting operations are retried in `enqueued_at` order,** at most `LEWBOX_CAPACITY_PARALLEL` (2) at a time per cluster, when:
   - a sandbox on the cluster stops or is deleted (an event from the [reconciler](../sandboxes/reconciler.md), or the fork's event stream, F35);
   - a node becomes usable;
   - or every 10 s otherwise.

   A smaller request behind a larger one may go first if the larger one was just refused again. This avoids head-of-line blocking without starving the larger.
3. **At the deadline,** the operation fails with `error_code: cluster_capacity_exceeded`, and the sandbox becomes `failed` (create), or stays `stopped` (start).
4. **A create still waiting for its 60 s answer** returns `202` with the operation in `waiting_for_capacity`, and consumers follow it as usual.

**Recognising capacity:**

- the fork's `code: capacity_exceeded` with its `reason` (`memory`, `cpu`, `disk`, `gpu`, `no_placement_target`) (F34, in the minimum fork release). Lewbox never reads an error's wording ([D15](../README.md#decisions)).

`no_placement_target` with no node usable isn't capacity: it's `cluster_unavailable`.

### Impossible requests

Before reserving, Lewbox checks the request fits on at least one member **that runs the sandbox's isolation level**, using its last-known capacity from membership ([health-and-metrics](health-and-metrics.md#membership)):

- `cpu ≤ node.cpu × cpu_overcommit`;
- `memory_mb ≤ node.memory × memory_overcommit`;
- `disk_gb ≤ node.disk × disk_ratio`;
- a GPU on a node that has one.

If no member could ever hold it, the create is refused at once with `422 validation_failed` on the field, "larger than any node in cluster eu-main (largest: 16 vCPUs, 64 GB)". This replaces two minutes of waiting for something that can never fit.

### Overcommit

Per cluster, applied to every node through the spec ([node-agent](node-agent.md#desired-state)):

| Setting | Default | AerolVM setting | Means |
|---|---|---|---|
| `cpu_overcommit` | `4.0` | CPU reservation ratio (confirm the setting, F44) | vCPUs placed per host core. CPU is shared fairly by cgroups, so overcommit is safe |
| `memory_overcommit` | `1.0` | memory reservation ratio (F44) | Memory placed per host GB. `1.0` = never more than the host has; what Lewagent asked for |
| `disk_ratio` | `1.0` | `SB_DISK_RESERVATION_RATIO`, with `SB_HOST_DISK_GB` from the node's `/var/lib` | Declared disk placed per host GB |
| `reserved_memory_mb` | `2048` | AerolVM's memory floor | Kept back for the host, Docker, the registry and the agent |

- **`microvm` (Firecracker, later)** needs `memory_overcommit` ≤ 1.0, as a VM's memory isn't shared back. On a cluster offering several levels, the cap applies to the nodes that run `microvm` ([runtimes](runtimes.md#firecracker-microvm-later)); the node spec sets it per node.
- **Changing a setting** applies to new placements only; running sandboxes are unaffected.

### Disk

Disk placement is turned on: each node's `SB_HOST_DISK_GB` comes from its heartbeat (the filesystem holding `/var/lib`, less the space for images and the build cache, `LEWBOX_NODE_DISK_RESERVE_GB`, 50 GB). It counts declared `disk_gb`, not use, so a node's disk can still fill:

- the node agent's `node.disk_low` notification ([health-and-metrics](health-and-metrics.md#notifications));
- snapshot image cleanup;
- GC ([storage-and-registry](storage-and-registry.md#garbage-collection)).

### Node roles

For clusters past 10 nodes, AerolVM needs dedicated roles:

- `server`: Raft voters, the API;
- `worker`: sandboxes;
- `ingress`: Caddy, the edge for port URLs.

- **Chosen per installation** (`role`: `mixed` by default; `server`, `worker`, `ingress`), passed to AerolVM at install, and recorded on the node.
- **The dashboard guides it:**
  - warns at 8 mixed nodes;
  - suggests 3 or 5 servers;
  - refuses an 11th `mixed` node.
- **Voters:**
  - the cluster page shows the voter count;
  - warns when it's even;
  - warns when it's below 3 with more than 2 nodes.
- **Ingress nodes in direct mode** change the DNS shape (port URLs go to ingress nodes, not the sandbox's node), which AerolVM's shared-domain topologies cover (open question 1).

### Cluster status

`clusters.status`, chosen by the project:

| Status | Creates and starts | Running sandboxes | Builds |
|---|---|---|---|
| `active` | Accepted | Run | Run |
| `suspended` | `409 cluster_suspended` (carries `cluster`), whether named or the profile's default; never passed over for another cluster | Keep running; can be stopped and deleted | Run |
| `draining` (new) | As suspended | Persisted ones are stopped (offloaded) and not restarted; others deleted, with notice ([installation](installation.md#removing-a-node), for a whole cluster) | Paused |

`draining` is for retiring a cluster: once it has no sandboxes, it can be deleted.

### Capacity view

The control plane keeps, per node and per cluster:

| Figure | Source |
|---|---|
| `total` (vCPUs, memory, disk, GPUs) | Membership `capacity`, or the heartbeat |
| `placeable` | `total × overcommit − reserved` |
| `reserved` | The sum of `cpu`, `memory_mb` and `disk_gb` of sandboxes on the node that are `creating`, `running`, `starting` or `stopping` (Lewbox's own records) |
| `used` | The heartbeat: host CPU %, memory used, disk used |
| `waiting` | Requests in the capacity queue |

- **Shown on the cluster page** (bars per node) and returned by the API.
- **Not used to refuse creates.** AerolVM is the authority, and Lewbox's figures lag. Only the impossible-size check uses them.

### Limits

Profile limits (running sandboxes, total vCPUs and memory, per-sandbox sizes) are in [limits](../limits.md). Lewagent asked for **limits per cluster**: limits may name a cluster, and a create is checked against the limits for its cluster as well as the profile-wide ones.

### Moving between clusters

Later. A persisted sandbox's snapshot would be copied from one cluster's registry to another's (the same architecture, and the target offering the sandbox's isolation level), and the sandbox resumed there with a new `cluster`. Its id and its level stay.

## API

Admin API (paths per [api](../api.md)):

| Method and path | What |
|---|---|
| `PATCH /v0/projects/{prj}/clusters/{clu}` | `status` (`active`, `suspended`, `draining`); `placement: {cpu_overcommit, memory_overcommit, disk_ratio, reserved_memory_mb}` |
| `GET …/clusters/{clu}/capacity` | Totals and per node: `total`, `placeable`, `reserved`, `used`, `waiting` |
| `POST …/installations` | Takes `role` |

Consumers:

- see `waiting_for_capacity` as an operation step;
- see `503 cluster_capacity_exceeded` (or a failed operation with that `error_code`);
- see `422 validation_failed` for impossible sizes;
- see `409 cluster_suspended`.

The consumer's `GET /v0/profile` may show each cluster's largest allowed sandbox (open question 2).

## Security

- **Overcommit is the project's choice.** Memory overcommit above 1.0 risks out-of-memory kills across sandboxes, so the dashboard explains it.
- **Capacity figures are the project's,** never shown to consumers beyond refusals.

## Observability

- **Metrics:**
  - `lewbox_capacity_waits{cluster}` (queue length);
  - `lewbox_capacity_wait_seconds`;
  - `lewbox_capacity_exceeded_total{cluster,reason}`;
  - `lewbox_cluster_reserved_ratio{cluster,resource}`.
- **Notifications:**
  - `cluster.capacity_low` when placeable memory is over 90% reserved for 10 minutes;
  - `cluster.capacity_exceeded` when creates fail for capacity.

## Configuration

| Setting | Default | What |
|---|---|---|
| `LEWBOX_CAPACITY_WAIT` | `2m` | How long a create or start waits for room |
| `LEWBOX_CAPACITY_PARALLEL` | `2` | Waiting operations retried at once per cluster |
| `LEWBOX_NODE_DISK_RESERVE_GB` | `50` | Disk kept back from placement per node |
| Defaults for new clusters' `placement` | as above | |

## Testing

- **Capacity queue:**
  - order;
  - wake-ups on stop and delete;
  - deadline failures;
  - a smaller request behind a larger one;
  - concurrency per cluster;
  - many replicas sharing the queue (`SKIP LOCKED`).
- **Recognition:** each code and reason; `no_placement_target` with no usable node as `cluster_unavailable`.
- **Impossible sizes:** against fixture membership.
- **The fake AerolVM** with capacity limits per node, filled and drained by tests.

## Build plan

1. **M1:**
   - entry-node choice and skipping;
   - capacity recognition;
   - the capacity queue with wake-ups;
   - impossible-size refusals;
   - cluster status (`active`, `suspended`).
2. **M1:** overcommit and disk settings in the node spec (with F44 confirmed).
3. **M2:** capacity view and API; notifications.
4. **M3:** node roles at install, with dashboard guidance; `draining` clusters.
5. **Later:** moving persisted sandboxes between clusters.

## Open questions

1. **Ingress nodes:** with dedicated ingress nodes, port URLs can't name the sandbox's node. Do we adopt AerolVM's shared-domain topology (a TLS pass-through load balancer in front of ingress nodes) for large clusters?
2. **Show consumers a cluster's largest sandbox size** on their profile, so apps can size requests? It leaks a little about the project's hardware.
3. **Default CPU overcommit:** 4× is common for dev workloads; should Lewagent's hosted clusters use less, as agents build and test heavily?
4. **Should the capacity wait be longer for starts** (a user resuming their computer) than for creates?
