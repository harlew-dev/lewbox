-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, instance_admin, disabled, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY email;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CountEnabledInstanceAdmins :one
SELECT COUNT(*) FROM users WHERE instance_admin = 1 AND disabled = 0;

-- name: SetUserDisabled :one
-- Never disables the last enabled instance admin. The guard is in the
-- statement itself, so two concurrent calls can't both pass it.
UPDATE users SET disabled = sqlc.arg(disabled)
WHERE users.id = sqlc.arg(id)
  AND (sqlc.arg(disabled) = 0
       OR NOT (users.instance_admin = 1 AND users.disabled = 0
               AND (SELECT COUNT(*) FROM users AS a WHERE a.instance_admin = 1 AND a.disabled = 0) <= 1))
RETURNING *;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = ? WHERE id = ?;

-- name: DeleteUser :execrows
-- Never deletes the last enabled instance admin; see SetUserDisabled.
DELETE FROM users
WHERE users.id = ?
  AND NOT (users.instance_admin = 1 AND users.disabled = 0
           AND (SELECT COUNT(*) FROM users AS a WHERE a.instance_admin = 1 AND a.disabled = 0) <= 1);
