-- name: CreateSandboxKey :one
INSERT INTO sandbox_keys (id, org_id, cluster_id, name, upstream_id, last4, created_by, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListSandboxKeys :many
SELECT sqlc.embed(sandbox_keys), users.email AS created_by_email,
       COALESCE(users.instance_admin, 0) AS created_by_admin
FROM sandbox_keys
LEFT JOIN users ON users.id = sandbox_keys.created_by
WHERE sandbox_keys.org_id = ?
ORDER BY (sandbox_keys.revoked_at IS NOT NULL), sandbox_keys.created_at DESC, sandbox_keys.id DESC;

-- name: GetSandboxKeyByID :one
SELECT sqlc.embed(sandbox_keys), users.email AS created_by_email,
       COALESCE(users.instance_admin, 0) AS created_by_admin
FROM sandbox_keys
LEFT JOIN users ON users.id = sandbox_keys.created_by
WHERE sandbox_keys.org_id = ? AND sandbox_keys.id = ?;

-- name: GetLiveSandboxKeyByName :one
SELECT sqlc.embed(sandbox_keys), users.email AS created_by_email,
       COALESCE(users.instance_admin, 0) AS created_by_admin
FROM sandbox_keys
LEFT JOIN users ON users.id = sandbox_keys.created_by
WHERE sandbox_keys.org_id = ? AND sandbox_keys.name = ? AND sandbox_keys.revoked_at IS NULL;

-- name: ListLiveSandboxKeysByCluster :many
SELECT * FROM sandbox_keys WHERE cluster_id = ? AND revoked_at IS NULL;

-- name: ListLiveSandboxKeysByOrg :many
SELECT * FROM sandbox_keys WHERE org_id = ? AND revoked_at IS NULL;

-- name: ListLiveSandboxKeysByCreator :many
SELECT * FROM sandbox_keys
WHERE created_by = sqlc.arg(created_by) AND revoked_at IS NULL
  AND (sqlc.arg(org_id) = '' OR org_id = sqlc.arg(org_id))
ORDER BY cluster_id, id;

-- name: RevokeSandboxKey :execrows
UPDATE sandbox_keys SET revoked_at = ?
WHERE org_id = ? AND id = ? AND revoked_at IS NULL;
