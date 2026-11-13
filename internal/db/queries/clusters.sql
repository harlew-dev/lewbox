-- name: CreateCluster :one
INSERT INTO clusters (id, slug, name, admin_url, endpoint_url, runtime, admin_token_enc,
                      admin_token_id, admin_token_last4, status, status_detail, version,
                      domain, dns_profile_id, dns_records, storage_profile_id, stop_policy,
                      archive_local_retention, archive_snapshot_name,
                      checks, checked_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetClusterByID :one
SELECT * FROM clusters WHERE id = ?;

-- name: GetClusterBySlug :one
SELECT * FROM clusters WHERE slug = ?;

-- name: ListClusters :many
SELECT * FROM clusters ORDER BY slug;

-- name: UpdateClusterSettings :one
UPDATE clusters
SET domain = ?, dns_profile_id = ?, dns_records = ?, storage_profile_id = ?, stop_policy = ?,
    archive_local_retention = ?, archive_snapshot_name = ?
WHERE id = ?
RETURNING *;

-- name: UpdateClusterCheck :exec
UPDATE clusters
SET status = ?, status_detail = ?, version = ?, checks = ?, checked_at = ?
WHERE id = ?;

-- name: DeleteCluster :execrows
DELETE FROM clusters WHERE id = ?;

-- name: SetClusterSynced :exec
UPDATE clusters SET synced_at = ?, sync_error = '' WHERE id = ?;

-- name: SetClusterSyncError :exec
UPDATE clusters SET sync_error = ? WHERE id = ?;

-- name: CountNodesByCluster :many
SELECT cluster_id, COUNT(*) AS n FROM nodes WHERE cluster_id IS NOT NULL GROUP BY cluster_id;
