# Phase 3: sandbox mirror, limits across regions, usage

**Goal:** Lewbox knows every sandbox on every cluster and whose it is; each organization's plan limits hold across all regions together; usage is recorded per hour for charts and billing. Customers can see, stop and delete their sandboxes in the portal.

## AerolVM facts (v0.7.0, verified in source)

- `GET /v1/sandboxes` (admin token) is a **bare JSON array**, paged in cluster mode: `?limit=500`, next page in the `X-Cluster-List-Next-Page-Token` response header, passed back as `page_token`; loop until the header is absent. `X-Cluster-List-Partial: true` means some node didn't answer. Each sandbox: `id` (`sb-…`), `name`, `image`, `status` (`creating|started|stopped|destroyed|error`), `status_reason`, `runtime`, `cpu` (float), `memory_mb`, `disk_gb`, `created_at`, `updated_at`, `last_active_at`. **No owner and no node.**
- `GET /v1/cluster/sandbox-index?limit=5000` (admin) → `{"placements":[{"sandbox_id","owner_node_id","owner_ref":"team:<t>","quota_cpu","quota_memory_mb","created_unix","state"}],"next_page_token"}`. Join on `sandbox_id` to get the team and node. `503` with `cluster: not enabled on this node` means a single-node (non-cluster) install: Lewbox can't attribute sandboxes there (see DECISIONS D8).
- `PUT /v1/teams/{team}/quota {"max_sandboxes","max_cpu","max_memory_mb"}`: `0`/omitted = no limit on that axis; all-zero is refused (`400`); replicated by Raft; lowering below usage removes nothing, it only refuses growth (`429 team_quota_exceeded`). `DELETE` removes it (`204`, idempotent). Usage counts every sandbox the team **holds**, running or stopped, at the size it asked for.
- **Blocking quota** (nothing may grow): `{"max_cpu":0.001,"max_memory_mb":1}`.
- `POST /v1/sandboxes/{id}/stop` → `200` (also if already stopped); `DELETE /v1/sandboxes/{id}` → `204`; both work with the admin token on any team's sandbox; `404` if unknown.

## Schema (`00008_sync.sql`)

```sql
CREATE TABLE sandboxes (                     -- a mirror; AerolVM is the source of truth
    cluster_id    TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    id            TEXT NOT NULL,             -- AerolVM sb-…
    org_id        TEXT REFERENCES organizations(id) ON DELETE SET NULL,  -- NULL: not a customer's
    team          TEXT NOT NULL DEFAULT '',
    node          TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    image         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    status_reason TEXT NOT NULL DEFAULT '',
    runtime       TEXT NOT NULL DEFAULT '',
    cpu           REAL NOT NULL DEFAULT 0,
    memory_mb     INTEGER NOT NULL DEFAULT 0,
    disk_gb       INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL,
    last_seen_at  DATETIME NOT NULL,
    gone_at       DATETIME,                   -- set when it disappears from the cluster
    PRIMARY KEY (cluster_id, id)
);
CREATE INDEX sandboxes_org ON sandboxes(org_id, gone_at);

CREATE TABLE usage_hourly (
    org_id            TEXT NOT NULL,          -- '' = not a customer's (the operator's own)
    cluster_id        TEXT NOT NULL,
    hour              DATETIME NOT NULL,      -- UTC, start of the hour
    sandbox_seconds   INTEGER NOT NULL DEFAULT 0,
    vcpu_seconds      REAL NOT NULL DEFAULT 0,
    memory_mb_seconds INTEGER NOT NULL DEFAULT 0,
    peak_sandboxes    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, cluster_id, hour)
);

CREATE TABLE quota_state (                    -- what Lewbox last pushed, to push only changes
    org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    quota      TEXT NOT NULL,                 -- JSON body pushed, or "none" after a DELETE
    pushed_at  DATETIME NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, cluster_id)
);

ALTER TABLE clusters ADD COLUMN synced_at DATETIME;
ALTER TABLE clusters ADD COLUMN sync_error TEXT NOT NULL DEFAULT '';
```

## The sync loop

`lewbox serve` runs `svc.Sync(ctx)` every `--sync-interval` (env `LEWBOX_SYNC_INTERVAL`, default `15s`, `0` disables), as the local principal, like `CheckAllClusters`. One pass:

1. **For each cluster** (any status except one that has never been checked), with a 20 s timeout per cluster:
   1. Read the whole sandbox index and the whole sandbox list (both paged). On error, store `sync_error`, keep the mirror as it is, and skip metering for that cluster this pass.
   2. Upsert every listed sandbox into the mirror. `team` comes from `owner_ref` (`team:` prefix stripped); `org_id` from `organizations.team`; `node` from `owner_node_id`. Sandboxes in the index but not in the list (a node didn't answer) keep their last row and `last_seen_at`.
   3. If the list wasn't partial, set `gone_at = now` on rows of this cluster not seen in this pass (and clear `gone_at` on rows seen again).
   4. **Meter**: `dt = now − cluster.synced_at`, capped at 2 × interval (a long Lewbox outage is not charged; DECISIONS D9). For every sandbox with status `started`, add to the `usage_hourly` row for `(org_id or '', cluster, hour(now))`: `dt` sandbox-seconds, `cpu × dt` vCPU-seconds, `memory_mb × dt` MB-seconds. Set `peak_sandboxes = max(peak, running count for that org on that cluster)`.
   5. Set `synced_at = now`, clear `sync_error`.
2. **Quotas, for each organization** (after all clusters):
   - Effective limits from Phase 1 (`L_sandboxes`, `L_vcpus`, `L_memory_mb`; nil = unlimited).
   - `used_c` per cluster from the mirror: rows not gone and status not `destroyed`; count, Σ cpu, Σ memory_mb (stopped sandboxes count, as AerolVM counts them).
   - If the organization isn't `active`: push the **blocking quota** everywhere.
   - Else, per limited kind: `headroom = max(0, L − Σ_c used_c)`, `quota_c = used_c + headroom` (**shared** mode, DECISIONS D10). If any limited kind's `quota_c` is 0 on a cluster, push the blocking quota there (0 would mean "no limit"). If no kind is limited, `DELETE` the quota.
   - Push only when the body differs from `quota_state`, the last push failed, or it's older than 10 minutes. Record the result in `quota_state`.
   - Clusters that failed to sync this pass keep their last `used_c` (stale but conservative).
3. **Suspended organizations** (`status_reason` `admin` or `credits`): stop every mirrored sandbox of theirs with status `started` (POST stop), so they stop accruing compute. `pending` organizations have none.

Also run step 2 for one organization right after its plan, limits or status change (`UpdateOrganization`, plan edits), best effort, so changes apply in seconds instead of on the next pass.

Every AerolVM call in the loop uses the existing client with the cluster's admin token. Concurrency: one pass at a time (a mutex); clusters may be synced in parallel goroutines (bounded, e.g. 4).

## API

| Route | Who | What |
|---|---|---|
| `GET /v0/sandboxes?region=&status=&org=` | members (their org via header); instance admins (all orgs, `org` filter optional) | `{"data":[Sandbox]}` from the mirror, not gone, newest first, max 500 |
| `POST /v0/sandboxes/{id}/stop` | owner/admin/member of the sandbox's org, org API key, instance admins | Stops it on the cluster, updates the mirror, returns the Sandbox |
| `DELETE /v0/sandboxes/{id}` | same | Deletes it on the cluster, marks it gone, returns the Sandbox |
| `GET /v0/usage?from=&to=&granularity=hour\|day&org=` | members (their org); instance admins (one org, or the whole fleet when no org is given) | Usage series and totals |
| `GET /v0/limits` | members (their org); instance admins | Limits, live use and per-region quotas |

```json
// Sandbox
{"id":"sb-…","region":"eu-west","organization":{"slug":"acme","name":"Acme"} /* instance admins only */,
 "name":"","image":"ubuntu:22.04","status":"started","status_reason":"","runtime":"docker",
 "cpu":1,"memory_mb":1024,"disk_gb":10,"node":"" /* instance admins only */,
 "created_at":"…","last_seen_at":"…"}

// Usage
{"from":"…","to":"…","granularity":"day",
 "totals":{"sandbox_hours":12.5,"vcpu_hours":20.1,"memory_gib_hours":24.0},
 "data":[{"start":"2026-11-01T00:00:00Z","sandbox_hours":1.2,"vcpu_hours":2,"memory_gib_hours":2.4,"peak_sandboxes":3,
          "regions":{"eu-west":{"vcpu_hours":2}}}]}

// Limits
{"limits":{"sandboxes":2,"vcpus":2,"memory_mb":4096},"used":{"sandboxes":1,"vcpus":1,"memory_mb":1024},
 "regions":[{"slug":"eu-west","used":{…},"quota":{"max_sandboxes":2,"max_cpu":2,"max_memory_mb":4096}|null,"blocked":false,"pushed_at":"…","error":""}]}
```

- `{id}` is the AerolVM sandbox ID. If two clusters ever share one, require `?region=`.
- A sandbox belonging to another organization is `404` (never `403`).
- Usage defaults: `to` = now, `from` = 30 days ago, `granularity=day`; buckets with no usage are present with zeros (so charts have a continuous axis). Max range 400 days.
- `vcpu_hours = vcpu_seconds/3600`, `memory_gib_hours = memory_mb_seconds/1024/3600`, rounded to 3 decimals.

## CLI

```
lewbox sandbox list [--region eu-west] [--status started] [--org acme | --all]
lewbox sandbox stop sb-…
lewbox sandbox delete sb-… [--yes]
lewbox usage [--from 2026-11-01] [--to …] [--hourly] [--org acme]
lewbox limits [--org acme]
```

## Done when

- `aerolvmtest` fake gains paged sandbox list (with the next-page header and partial flag), sandbox index, team quotas (PUT/GET/DELETE, with usage and the "0 = unlimited" rule) and stop/delete, plus a way for tests to add sandboxes owned by a team.
- Tests cover: paging both lists; attribution to the right org; unknown teams stay unattributed; partial list doesn't mark sandboxes gone; metering (started only, dt cap, hour bucketing, peak); quota computation (headroom across two clusters, blocking when a quota would be 0, delete when unlimited, non-active orgs blocked, only-changed pushes); suspended orgs' sandboxes stopped; usage API (zero-filled buckets, totals, scoping); limits API; stop/delete scoping (other org → 404, viewer → 403).
- Parity tests for the CLI commands; docs: `docs/customers/sandboxes-and-usage.md`, operator page on limits (`docs/accounts/customers-and-plans.md` gains "How limits are enforced"), references updated.
- The four Go checks pass. Then `e2e.sh` (Phase 7) proves it against real AerolVM.
