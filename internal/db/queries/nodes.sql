-- name: CreateNode :one
INSERT INTO nodes (id, name, url, created_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetNodeByID :one
SELECT * FROM nodes WHERE id = ?;

-- name: GetNodeByName :one
SELECT * FROM nodes WHERE name = ?;

-- name: ListNodes :many
SELECT * FROM nodes ORDER BY name;

-- name: ListNodesByCluster :many
SELECT * FROM nodes WHERE cluster_id = ? ORDER BY name;

-- name: UpsertClusterNode :one
INSERT INTO nodes (id, name, url, cluster_id, role, version, alive, last_seen_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    url = excluded.url,
    cluster_id = excluded.cluster_id,
    role = excluded.role,
    version = excluded.version,
    alive = excluded.alive,
    last_seen_at = excluded.last_seen_at
RETURNING *;

-- name: ReportNodeSettings :one
UPDATE nodes
SET settings_applied_at = ?,
    settings_storage_profile_id = ?,
    settings_storage_version = ?,
    settings_dns_profile_id = ?,
    settings_dns_version = ?
WHERE id = ?
RETURNING *;

-- name: DeleteNode :execrows
DELETE FROM nodes WHERE id = ?;

-- name: MarkClusterNodesDown :exec
UPDATE nodes SET alive = 0 WHERE cluster_id = ?;
