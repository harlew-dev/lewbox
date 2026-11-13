-- name: ListArchivableSandboxes :many
-- Every stopped customer sandbox on an archive cluster with a storage profile
-- that has no live archive yet.
SELECT sandboxes.cluster_id, sandboxes.id AS sandbox_id, sandboxes.org_id,
       sandboxes.name, sandboxes.image, sandboxes.runtime, sandboxes.cpu,
       sandboxes.memory_mb, sandboxes.disk_gb
FROM sandboxes
JOIN clusters ON clusters.id = sandboxes.cluster_id
WHERE sandboxes.status = 'stopped'
  AND sandboxes.gone_at IS NULL
  AND sandboxes.org_id IS NOT NULL
  AND clusters.stop_policy = 'archive'
  AND clusters.storage_profile_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM archives
      WHERE archives.cluster_id = sandboxes.cluster_id
        AND archives.sandbox_id = sandboxes.id
        AND archives.status NOT IN ('cancelled', 'deleted')
  );

-- name: InsertArchive :execrows
-- One statement that cannot double-insert: the partial unique index plus
-- INSERT OR IGNORE. The sandbox must still be stopped and orphan-free.
INSERT OR IGNORE INTO archives (id, org_id, cluster_id, sandbox_id, sandbox_name, image,
                                runtime, cpu, memory_mb, disk_gb, tags, snapshot_name,
                                status, created_at)
SELECT sqlc.arg(archive_id), sandboxes.org_id, sandboxes.cluster_id, sandboxes.id, sandboxes.name,
       sandboxes.image, sandboxes.runtime, sandboxes.cpu, sandboxes.memory_mb,
       sandboxes.disk_gb, '{}', 'archive-' || sandboxes.id, 'pending', sqlc.arg(created_at)
FROM sandboxes
JOIN clusters ON clusters.id = sandboxes.cluster_id
WHERE sandboxes.cluster_id = sqlc.arg(cluster_id)
  AND sandboxes.id = sqlc.arg(sandbox_id)
  AND sandboxes.status = 'stopped'
  AND sandboxes.gone_at IS NULL
  AND sandboxes.org_id IS NOT NULL
  AND clusters.stop_policy = 'archive'
  AND clusters.storage_profile_id IS NOT NULL;

-- name: ListDueArchives :many
-- Pending, pushing and failed rows that are due a step.
SELECT * FROM archives
WHERE status IN ('pending', 'pushing', 'failed')
  AND (next_attempt_at IS NULL OR next_attempt_at <= sqlc.arg(now))
ORDER BY created_at, id;

-- name: GetArchiveByID :one
SELECT * FROM archives WHERE id = ?;

-- name: ListArchives :many
-- Every live archive, newest first, for one workspace and region, or every
-- workspace and region when org_id or cluster_id is empty. Cancelled and
-- deleted rows are not archives any more.
SELECT archives.id, archives.org_id, archives.cluster_id, archives.sandbox_id,
       archives.sandbox_name, archives.image, archives.runtime, archives.cpu,
       archives.memory_mb, archives.disk_gb, archives.tags, archives.snapshot_name,
       archives.registry_ref, archives.status, archives.error, archives.attempts,
       archives.next_attempt_at, archives.pushing_since, archives.created_at,
       archives.archived_at, archives.deleted_at, archives.last_restored_sandbox_id,
       archives.last_restored_at, clusters.slug AS region, organizations.slug AS org
FROM archives
JOIN clusters ON clusters.id = archives.cluster_id
JOIN organizations ON organizations.id = archives.org_id
WHERE archives.status IN ('pending', 'pushing', 'archived', 'failed')
  AND (sqlc.arg(org_id) = '' OR archives.org_id = sqlc.arg(org_id))
  AND (sqlc.arg(cluster_id) = '' OR archives.cluster_id = sqlc.arg(cluster_id))
ORDER BY archives.created_at DESC, archives.id DESC;

-- name: CountLiveArchivesByStorageProfile :one
-- How many archives a storage profile's clusters still hold. Their data is
-- in the bucket this profile names, so changing where it points strands it.
SELECT COUNT(*)
FROM archives
JOIN clusters ON clusters.id = archives.cluster_id
WHERE clusters.storage_profile_id = sqlc.arg(profile_id)
  AND archives.status IN ('pending', 'pushing', 'archived', 'failed');

-- name: ListArchivesBySandbox :many
SELECT * FROM archives WHERE cluster_id = ? AND sandbox_id = ? ORDER BY created_at DESC;

-- name: MarkArchivePushing :exec
UPDATE archives
SET sandbox_name = ?, image = ?, runtime = ?, cpu = ?, memory_mb = ?, disk_gb = ?,
    tags = ?, status = 'pushing', pushing_since = ?, next_attempt_at = NULL, error = ''
WHERE id = ?;

-- name: RestartArchivePush :exec
-- A failed row being retried starts a fresh push deadline.
UPDATE archives SET pushing_since = ?, next_attempt_at = NULL WHERE id = ?;

-- name: ArchivePushed :exec
UPDATE archives
SET status = 'archived', registry_ref = ?, archived_at = ?, error = ''
WHERE id = ?;

-- name: CancelArchive :exec
UPDATE archives SET status = 'cancelled', error = ? WHERE id = ?;

-- name: FailArchive :exec
UPDATE archives
SET status = 'failed', attempts = attempts + 1, next_attempt_at = ?,
    pushing_since = NULL, error = ?
WHERE id = ?;

-- name: DeferArchive :exec
-- Leaves the status alone and tries again a little later.
UPDATE archives SET error = ?, next_attempt_at = ? WHERE id = ?;

-- name: RetryArchive :exec
-- The snapshot is gone, so take it again.
UPDATE archives
SET status = 'pending', pushing_since = NULL, next_attempt_at = NULL
WHERE id = ?;

-- name: RecordArchiveRestore :exec
UPDATE archives
SET last_restored_sandbox_id = ?, last_restored_at = ?
WHERE id = ?;

-- name: MarkArchiveDeleted :exec
UPDATE archives SET status = 'deleted', deleted_at = ?, error = '' WHERE id = ?;
