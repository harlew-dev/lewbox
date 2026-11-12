-- name: UpsertSandbox :exec
INSERT INTO sandboxes (cluster_id, id, org_id, team, node, name, image, status,
                       status_reason, runtime, cpu, memory_mb, disk_gb, created_at,
                       last_seen_at, gone_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
ON CONFLICT(cluster_id, id) DO UPDATE SET
    org_id = COALESCE(excluded.org_id, sandboxes.org_id),
    team = CASE WHEN excluded.team = '' THEN sandboxes.team ELSE excluded.team END,
    node = CASE WHEN excluded.node = '' THEN sandboxes.node ELSE excluded.node END,
    name = excluded.name,
    image = excluded.image,
    status = excluded.status,
    status_reason = excluded.status_reason,
    runtime = excluded.runtime,
    cpu = excluded.cpu,
    memory_mb = excluded.memory_mb,
    disk_gb = excluded.disk_gb,
    created_at = excluded.created_at,
    last_seen_at = excluded.last_seen_at,
    gone_at = NULL;

-- name: MarkSandboxesGone :exec
UPDATE sandboxes SET gone_at = ?
WHERE cluster_id = ? AND gone_at IS NULL AND last_seen_at < ?;

-- name: ListSandboxes :many
SELECT sqlc.embed(sandboxes),
       clusters.slug AS cluster_slug,
       organizations.slug AS org_slug,
       organizations.name AS org_name
FROM sandboxes
JOIN clusters ON clusters.id = sandboxes.cluster_id
LEFT JOIN organizations ON organizations.id = sandboxes.org_id
WHERE sandboxes.gone_at IS NULL
  AND (sqlc.arg(org_id) = '' OR sandboxes.org_id = sqlc.arg(org_id))
  AND (sqlc.arg(cluster_id) = '' OR sandboxes.cluster_id = sqlc.arg(cluster_id))
  AND (sqlc.arg(status) = '' OR sandboxes.status = sqlc.arg(status))
ORDER BY sandboxes.created_at DESC, sandboxes.id DESC
LIMIT 500;

-- name: ListSandboxesByID :many
SELECT sqlc.embed(sandboxes),
       clusters.slug AS cluster_slug,
       organizations.slug AS org_slug,
       organizations.name AS org_name
FROM sandboxes
JOIN clusters ON clusters.id = sandboxes.cluster_id
LEFT JOIN organizations ON organizations.id = sandboxes.org_id
WHERE sandboxes.id = ? AND sandboxes.gone_at IS NULL;

-- name: ListStartedSandboxesByOrg :many
SELECT sqlc.embed(sandboxes), clusters.slug AS cluster_slug
FROM sandboxes
JOIN clusters ON clusters.id = sandboxes.cluster_id
WHERE sandboxes.org_id = ? AND sandboxes.gone_at IS NULL AND sandboxes.status = 'started';

-- name: UpdateSandboxStatus :exec
UPDATE sandboxes SET status = ?, status_reason = ?
WHERE cluster_id = ? AND id = ?;

-- name: MarkSandboxGone :exec
UPDATE sandboxes SET gone_at = ?, status = 'destroyed'
WHERE cluster_id = ? AND id = ?;

-- name: SumSandboxesByOrg :many
SELECT cluster_id,
       COUNT(*) AS sandboxes,
       CAST(COALESCE(SUM(cpu), 0) AS REAL) AS vcpus,
       CAST(COALESCE(SUM(memory_mb), 0) AS INTEGER) AS memory_mb
FROM sandboxes
WHERE org_id = ? AND gone_at IS NULL AND status != 'destroyed'
GROUP BY cluster_id;

-- name: UpsertUsageHourly :exec
INSERT INTO usage_hourly (org_id, cluster_id, hour, sandbox_seconds, vcpu_seconds,
                          memory_mb_seconds, peak_sandboxes)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(org_id, cluster_id, hour) DO UPDATE SET
    sandbox_seconds = sandbox_seconds + excluded.sandbox_seconds,
    vcpu_seconds = vcpu_seconds + excluded.vcpu_seconds,
    memory_mb_seconds = memory_mb_seconds + excluded.memory_mb_seconds,
    peak_sandboxes = max(peak_sandboxes, excluded.peak_sandboxes);

-- name: ListUsageHourly :many
SELECT * FROM usage_hourly
WHERE (sqlc.arg(org_id) = '' OR org_id = sqlc.arg(org_id))
  AND hour >= sqlc.arg(from_hour) AND hour <= sqlc.arg(to_hour)
ORDER BY hour;

-- name: SumVcpuSecondsByOrg :many
SELECT org_id, CAST(COALESCE(SUM(vcpu_seconds), 0) AS REAL) AS vcpu_seconds
FROM usage_hourly
WHERE hour >= ?
GROUP BY org_id;

-- name: GetQuotaState :one
SELECT * FROM quota_state WHERE org_id = ? AND cluster_id = ?;

-- name: UpsertQuotaState :exec
INSERT INTO quota_state (org_id, cluster_id, quota, pushed_at, error)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(org_id, cluster_id) DO UPDATE SET
    quota = excluded.quota,
    pushed_at = excluded.pushed_at,
    error = excluded.error;

-- name: ListQuotaStates :many
SELECT * FROM quota_state WHERE org_id = ?;
