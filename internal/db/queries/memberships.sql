-- name: CreateMembership :one
INSERT INTO memberships (id, org_id, user_id, role, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: CreateOwnerMembership :one
-- Creates the first owner only while the account owns fewer than max_owned
-- workspaces. No row means the cap is reached; the guard is in the statement,
-- so two concurrent creates cannot both pass it.
INSERT INTO memberships (id, org_id, user_id, role, created_at)
SELECT sqlc.arg(id), sqlc.arg(org_id), sqlc.arg(user_id), 'owner', sqlc.arg(created_at)
WHERE (SELECT COUNT(*) FROM memberships AS owned
       WHERE owned.user_id = sqlc.arg(user_id) AND owned.role = 'owner') < CAST(sqlc.arg(max_owned) AS INTEGER)
RETURNING *;

-- name: GetMembership :one
SELECT * FROM memberships WHERE org_id = ? AND user_id = ?;

-- name: GetMembershipByID :one
SELECT * FROM memberships WHERE org_id = ? AND id = ?;

-- name: ListMembers :many
SELECT sqlc.embed(memberships), users.email, users.instance_admin
FROM memberships
JOIN users ON users.id = memberships.user_id
WHERE memberships.org_id = ?
ORDER BY users.email;

-- name: SetMembershipRole :one
-- Never demotes the organization's last owner. The guard is in the
-- statement, so two concurrent changes can't both pass it.
UPDATE memberships SET role = sqlc.arg(role)
WHERE memberships.id = sqlc.arg(id)
  AND (sqlc.arg(role) = 'owner'
       OR memberships.role != 'owner'
       OR (SELECT COUNT(*) FROM memberships AS o
           WHERE o.org_id = memberships.org_id AND o.role = 'owner') > 1)
RETURNING *;

-- name: DeleteMembership :execrows
-- Never removes the organization's last owner; see SetMembershipRole.
DELETE FROM memberships
WHERE memberships.id = ?
  AND (memberships.role != 'owner'
       OR (SELECT COUNT(*) FROM memberships AS o
           WHERE o.org_id = memberships.org_id AND o.role = 'owner') > 1);
