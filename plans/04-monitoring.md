# Phase 4: 30-day health, uptime and fleet metrics

**Goal:** the operator sees, for every cluster and server, a 30-day uptime bar (status-page style), live and historical resource use (memory, disk, CPU reserved vs cores, sandboxes), and a fleet overview, without installing anything new on the nodes.

## Where the data comes from (no new node software; DECISIONS D11)

The existing check loop (`CheckAllClusters`, every 60 s) already calls each cluster's `/health`, `whoami` and `GET /v1/cluster/members`. `members` carries a capacity heartbeat per node (refreshed about every 5 s):

`alive`, `version`, and `capacity.{host_cpu_cores, host_memory_total_mb, live_memory_free_mb, host_disk_free_gb, reserved_cpu, reserved_memory_mb, sandboxes_active, can_admit}` (`host_disk_total_gb` only if the operator set `SB_HOST_DISK_GB`). See `internal/aerolvm/testdata/members.json`.

Not available from AerolVM: CPU utilisation, load, uptime, network/disk I/O. Lewbox shows **allocation** (reserved vCPU vs cores) instead of utilisation.

## Schema (`00009_monitoring.sql`)

```sql
CREATE TABLE cluster_samples (
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    at         DATETIME NOT NULL,
    status     TEXT NOT NULL,        -- ready | degraded | unreachable (the check's result)
    latency_ms INTEGER NOT NULL,     -- time for GET /health; -1 if it failed
    PRIMARY KEY (cluster_id, at)
);

CREATE TABLE node_samples (
    node_id          TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    at               DATETIME NOT NULL,
    alive            INTEGER NOT NULL,
    sandboxes        INTEGER NOT NULL DEFAULT 0,
    cpu_cores        INTEGER NOT NULL DEFAULT 0,
    cpu_reserved     REAL NOT NULL DEFAULT 0,
    memory_total_mb  INTEGER NOT NULL DEFAULT 0,
    memory_free_mb   INTEGER NOT NULL DEFAULT 0,
    disk_free_gb     INTEGER NOT NULL DEFAULT 0,
    can_admit        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, at)
);
```

Every check writes one `cluster_samples` row and one `node_samples` row per member (an unreachable cluster writes its row with `status = unreachable` and its known nodes with `alive = 0`). Rows older than 31 days are deleted once a day (in the check loop). At 60 s that's ~44k rows per node per month: fine for SQLite with the primary key as the index.

The aerolvm client's `Member` type gains the `capacity` fields above (keep the struct small: only what is stored).

## Uptime

For each UTC day in the range: `uptime = ok samples / samples` where a cluster sample is ok when `status` is `ready` or `degraded`, and a node sample is ok when `alive`. `null` when there are no samples that day (Lewbox wasn't running). `down_minutes ≈ (samples − ok) × check interval`. The 30-day figure is the same ratio over all samples. (Gaps are "no data", not "down": a stopped orchestrator says nothing about the cluster. DECISIONS D11.)

## API (instance admins only)

| Route | What |
|---|---|
| `GET /v0/clusters/{ref}/health?days=30` | Uptime per day for the cluster and each node, plus the 30-day figures |
| `GET /v0/clusters/{ref}/metrics?range=24h\|7d\|30d` | Per-node series, averaged into buckets (24h → 15 min, 7d → 1 h, 30d → 6 h) |
| `GET /v0/overview` | The console home: counts, capacity, per-cluster uptime, running sandboxes over 7 days, top customers this month |

```json
// health
{"cluster":{"uptime":0.9995,"days":[{"date":"2026-11-01","uptime":1,"samples":1440,"down_minutes":0}]},
 "nodes":[{"name":"node-1","uptime":0.999,"days":[…]}]}

// metrics
{"range":"24h","bucket_seconds":900,
 "nodes":[{"name":"node-1","data":[{"start":"…","sandboxes":3,"cpu_cores":4,"cpu_reserved":3,"memory_total_mb":15364,"memory_used_mb":6812,"disk_free_gb":13,"alive":1}]}]}

// overview
{"customers":{"total":12,"pending":1,"suspended":0},
 "sandboxes":{"running":7,"stopped":2},
 "capacity":{"nodes_alive":2,"nodes":2,"cpu_cores":8,"cpu_reserved":7,"memory_total_mb":30728,"memory_used_mb":12000},
 "clusters":[{"slug":"eu-west","name":"EU West","status":"ready","uptime":0.9995,"days":[…30…]}],
 "running":[{"start":"…","sandboxes":3.4}],              // hourly average over 7 days, from usage_hourly
 "top_customers":[{"slug":"acme","name":"Acme","vcpu_hours":120.5}]}  // this calendar month, max 5
```

`memory_used_mb = memory_total_mb − memory_free_mb`. `running[].sandboxes` = Σ `sandbox_seconds` / 3600 for that hour, all orgs (including the operator's own, `org_id = ''`).

## CLI

```
lewbox cluster health eu-west [--days 30]
lewbox cluster metrics eu-west [--range 24h]
lewbox overview
```

Text rendering: health prints one line per day as a compact bar (`█` ok, `▒` partial, `·` no data) and the percentages.

## Done when

- Tests: samples written on check (reachable and unreachable), pruning, uptime math (no-data days null, partial days), metrics bucketing, overview numbers from seeded data, admin-only (customers get 403).
- Parity tests for the three commands; `docs/clusters/health-and-checks.md` gains "History and uptime"; references updated.
- The four Go checks pass.
