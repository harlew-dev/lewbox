-- name: InsertClusterSample :exec
INSERT INTO cluster_samples (cluster_id, at, status, latency_ms)
VALUES (?, ?, ?, ?)
ON CONFLICT(cluster_id, at) DO UPDATE SET
    status = excluded.status,
    latency_ms = excluded.latency_ms;

-- name: InsertNodeSample :exec
INSERT INTO node_samples (node_id, at, alive, sandboxes, cpu_cores, cpu_reserved,
                          memory_total_mb, memory_free_mb, disk_free_gb, can_admit,
                          cert_expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(node_id, at) DO UPDATE SET
    alive = excluded.alive,
    sandboxes = excluded.sandboxes,
    cpu_cores = excluded.cpu_cores,
    cpu_reserved = excluded.cpu_reserved,
    memory_total_mb = excluded.memory_total_mb,
    memory_free_mb = excluded.memory_free_mb,
    disk_free_gb = excluded.disk_free_gb,
    can_admit = excluded.can_admit,
    cert_expires_at = excluded.cert_expires_at;

-- name: ListClusterSamples :many
SELECT * FROM cluster_samples
WHERE cluster_id = ? AND at >= ? AND at < ?
ORDER BY at;

-- name: ListNodeSamples :many
SELECT node_samples.* FROM node_samples
JOIN nodes ON nodes.id = node_samples.node_id
WHERE nodes.cluster_id = ? AND node_samples.at >= ? AND node_samples.at < ?
ORDER BY node_samples.node_id, node_samples.at;

-- name: ListNodeMetricBuckets :many
SELECT node_samples.node_id,
       CAST(unixepoch(substr(node_samples.at, 1, 19)) / CAST(sqlc.arg(bucket_seconds) AS INTEGER) AS INTEGER) AS bucket,
       AVG(node_samples.sandboxes) AS sandboxes,
       AVG(node_samples.cpu_cores) AS cpu_cores,
       AVG(node_samples.cpu_reserved) AS cpu_reserved,
       AVG(node_samples.memory_total_mb) AS memory_total_mb,
       AVG(node_samples.memory_free_mb) AS memory_free_mb,
       AVG(node_samples.disk_free_gb) AS disk_free_gb,
       AVG(node_samples.alive) AS alive
FROM node_samples
JOIN nodes ON nodes.id = node_samples.node_id
WHERE nodes.cluster_id = sqlc.arg(cluster_id)
  AND node_samples.at >= sqlc.arg(from_at)
  AND node_samples.at < sqlc.arg(to_at)
GROUP BY node_samples.node_id, bucket
ORDER BY node_samples.node_id, bucket;

-- name: LatestNodeSamples :many
SELECT node_samples.* FROM node_samples
JOIN (SELECT node_id, MAX(at) AS at FROM node_samples GROUP BY node_id) latest
ON node_samples.node_id = latest.node_id AND node_samples.at = latest.at;

-- name: DeleteClusterSamplesBefore :exec
DELETE FROM cluster_samples WHERE at < ?;

-- name: DeleteNodeSamplesBefore :exec
DELETE FROM node_samples WHERE at < ?;
