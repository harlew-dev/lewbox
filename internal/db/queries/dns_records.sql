-- name: CreateDNSRecord :one
INSERT INTO dns_records (id, cluster_id, profile_id, zone_id, node, name, type, content,
                         record_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListDNSRecordsByCluster :many
SELECT * FROM dns_records WHERE cluster_id = ? ORDER BY name, type, content;

-- name: DeleteDNSRecord :execrows
DELETE FROM dns_records WHERE id = ?;
