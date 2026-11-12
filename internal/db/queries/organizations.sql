-- name: CreateOrganization :one
INSERT INTO organizations (id, slug, name, status, created_at, team)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetOrganizationByID :one
SELECT * FROM organizations WHERE id = ?;

-- name: GetOrganizationBySlug :one
SELECT * FROM organizations WHERE slug = ?;

-- name: ListOrganizations :many
SELECT * FROM organizations ORDER BY slug;

-- name: ListOrganizationsForUser :many
SELECT sqlc.embed(organizations), memberships.role
FROM organizations
JOIN memberships ON memberships.org_id = organizations.id
WHERE memberships.user_id = ?
ORDER BY organizations.slug;

-- name: CountMembersByOrganization :many
SELECT org_id, COUNT(*) AS members FROM memberships GROUP BY org_id;

-- name: CountOrganizationsByStatus :many
SELECT status, COUNT(*) AS n FROM organizations GROUP BY status;

-- name: SetOrganizationName :one
UPDATE organizations SET name = ? WHERE id = ? RETURNING *;

-- name: SetOrganizationStatus :one
UPDATE organizations SET status = ?, status_reason = ? WHERE id = ? RETURNING *;

-- name: DeleteOrganization :exec
DELETE FROM organizations WHERE id = ?;
