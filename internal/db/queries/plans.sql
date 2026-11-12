-- name: CreatePlan :one
INSERT INTO plans (id, slug, name, max_sandboxes, max_vcpus, max_memory_mb, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPlanByID :one
SELECT * FROM plans WHERE id = ?;

-- name: GetPlanBySlug :one
SELECT * FROM plans WHERE slug = ?;

-- name: ListPlans :many
SELECT * FROM plans ORDER BY slug;

-- name: CountOrganizationsByPlan :many
SELECT plan_id, COUNT(*) AS organizations FROM organizations
WHERE plan_id IS NOT NULL GROUP BY plan_id;

-- name: UpdatePlan :one
UPDATE plans
SET slug = ?, name = ?, max_sandboxes = ?, max_vcpus = ?, max_memory_mb = ?
WHERE id = ?
RETURNING *;

-- name: DeletePlan :execrows
-- Refuses a plan an organization uses or that is the default. The guards are
-- in the statement, so a plan that becomes used is not removed.
DELETE FROM plans
WHERE plans.id = sqlc.arg(plan_id)
  AND NOT EXISTS (SELECT 1 FROM organizations WHERE organizations.plan_id = sqlc.arg(plan_id))
  AND NOT EXISTS (
    SELECT 1 FROM settings
    WHERE settings.key = 'default_plan' AND json_extract(settings.value, '$') = sqlc.arg(plan_id)
  );
