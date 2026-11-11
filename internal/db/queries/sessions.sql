-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at, last_used_at, kind)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: TouchSession :exec
UPDATE sessions SET last_used_at = ? WHERE id = ?;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = ?;

-- name: DeleteSessionsByUser :exec
DELETE FROM sessions WHERE user_id = ?;

-- name: DeleteOtherSessionsByUser :exec
DELETE FROM sessions WHERE user_id = ? AND id != ?;
