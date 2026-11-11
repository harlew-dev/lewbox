-- name: CreateAdminKey :one
INSERT INTO admin_keys (id, org_id, name, prefix, key_hash, last4, created_by, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAdminKeys :many
SELECT sqlc.embed(admin_keys), users.email AS created_by_email
FROM admin_keys
LEFT JOIN users ON users.id = admin_keys.created_by
WHERE admin_keys.org_id = ?
ORDER BY admin_keys.created_at DESC, admin_keys.id DESC;

-- name: GetAdminKeyByID :one
SELECT sqlc.embed(admin_keys), users.email AS created_by_email
FROM admin_keys
LEFT JOIN users ON users.id = admin_keys.created_by
WHERE admin_keys.org_id = ? AND admin_keys.id = ?;

-- name: GetLiveAdminKeyByName :one
SELECT sqlc.embed(admin_keys), users.email AS created_by_email
FROM admin_keys
LEFT JOIN users ON users.id = admin_keys.created_by
WHERE admin_keys.org_id = ? AND admin_keys.name = ? AND admin_keys.revoked_at IS NULL;

-- name: GetAdminKeyByHash :one
SELECT * FROM admin_keys WHERE key_hash = ?;

-- name: RevokeAdminKey :execrows
UPDATE admin_keys SET revoked_at = ?
WHERE org_id = ? AND id = ? AND revoked_at IS NULL;

-- name: TouchAdminKey :exec
UPDATE admin_keys SET last_used_at = ? WHERE id = ?;
