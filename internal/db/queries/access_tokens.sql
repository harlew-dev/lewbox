-- name: CreateAccessToken :one
INSERT INTO access_tokens (id, user_id, name, prefix, token_hash, last4, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAccessTokens :many
SELECT * FROM access_tokens
WHERE user_id = ?
ORDER BY created_at DESC, id DESC;

-- name: GetAccessTokenByID :one
SELECT * FROM access_tokens WHERE user_id = ? AND id = ?;

-- name: GetLiveAccessTokenByName :one
SELECT * FROM access_tokens WHERE user_id = ? AND name = ? AND revoked_at IS NULL;

-- name: GetAccessTokenByHash :one
SELECT * FROM access_tokens WHERE token_hash = ?;

-- name: RevokeAccessToken :execrows
UPDATE access_tokens SET revoked_at = ?
WHERE user_id = ? AND id = ? AND revoked_at IS NULL;

-- name: RevokeAccessTokensByUser :exec
UPDATE access_tokens SET revoked_at = ?
WHERE user_id = ? AND revoked_at IS NULL;

-- name: TouchAccessToken :exec
UPDATE access_tokens SET last_used_at = ? WHERE id = ?;
