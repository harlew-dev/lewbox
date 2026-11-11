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

-- name: DeleteNode :execrows
DELETE FROM nodes WHERE id = ?;

-- name: MarkClusterNodesDown :exec
UPDATE nodes SET alive = 0 WHERE cluster_id = ?;
