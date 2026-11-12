-- name: CreateSubscription :one
INSERT INTO subscriptions (org_id, cluster_id, plan_id, max_sandboxes, max_vcpus, max_memory_mb, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateSubscription :one
UPDATE subscriptions
SET plan_id = ?, max_sandboxes = ?, max_vcpus = ?, max_memory_mb = ?
WHERE org_id = ? AND cluster_id = ?
RETURNING *;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE org_id = ? AND cluster_id = ?;

-- name: ListSubscriptions :many
SELECT * FROM subscriptions WHERE org_id = ?;

-- name: ListAllSubscriptions :many
SELECT * FROM subscriptions ORDER BY org_id, cluster_id;

-- name: ListSubscriptionsByPlan :many
SELECT * FROM subscriptions WHERE plan_id = ?;

-- name: DeleteSubscription :execrows
DELETE FROM subscriptions WHERE org_id = ? AND cluster_id = ?;

-- name: CountSubscriptionsByPlan :many
SELECT plan_id, COUNT(*) AS subscriptions FROM subscriptions GROUP BY plan_id;
