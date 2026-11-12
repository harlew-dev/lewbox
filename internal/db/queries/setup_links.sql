-- name: CreateSetupLink :one
INSERT INTO setup_links (id, token_hash, plan_id, cluster_id, note, created_by, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListSetupLinks :many
SELECT * FROM setup_links ORDER BY created_at DESC, id DESC;

-- name: GetSetupLinkByID :one
SELECT * FROM setup_links WHERE id = ?;

-- name: GetSetupLinkByTokenHash :one
SELECT * FROM setup_links WHERE token_hash = ?;

-- name: ClaimSetupLink :one
-- The first sign-up with the token claims it. No row means it is used,
-- expired or claimed by someone else.
UPDATE setup_links
SET claimed_by = sqlc.arg(user_id), claimed_at = sqlc.arg(claimed_at)
WHERE token_hash = sqlc.arg(token_hash)
  AND claimed_by IS NULL
  AND used_at IS NULL
  AND expires_at > sqlc.arg(now)
RETURNING *;

-- name: ConsumeSetupLink :one
-- The first workspace to use the token wins. No row means it is used,
-- expired or claimed by someone else.
UPDATE setup_links
SET used_at = sqlc.arg(used_at),
    claimed_by = COALESCE(claimed_by, sqlc.arg(user_id)),
    claimed_at = COALESCE(claimed_at, sqlc.arg(claimed_at))
WHERE token_hash = sqlc.arg(token_hash)
  AND used_at IS NULL
  AND expires_at > sqlc.arg(now)
  AND (claimed_by IS NULL OR claimed_by = sqlc.arg(user_id))
RETURNING *;

-- name: SetSetupLinkOrg :exec
UPDATE setup_links SET org_id = ? WHERE id = ?;

-- name: ReleaseSetupLink :exec
UPDATE setup_links SET used_at = NULL, org_id = NULL WHERE id = ?;

-- name: UnclaimSetupLink :exec
UPDATE setup_links SET used_at = NULL, org_id = NULL, claimed_by = NULL, claimed_at = NULL WHERE id = ?;

-- name: DeleteSetupLink :execrows
DELETE FROM setup_links WHERE id = ? AND used_at IS NULL;
