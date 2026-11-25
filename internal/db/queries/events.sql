-- name: InsertEvent :exec
INSERT INTO events (
    id, name, actor_kind, actor_id, actor_label, target_type, target_id,
    target_name, organization_id, changes, ip, request_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetEventByID :one
SELECT * FROM events WHERE id = ?;

-- name: ListEvents :many
-- Newest first, with the cursor compared as a (created_at, id) row so rows
-- sharing a timestamp never repeat or go missing. Empty strings and NULL
-- times mean "no filter".
SELECT * FROM events
WHERE (sqlc.arg(actor_id) = '' OR actor_id = sqlc.arg(actor_id))
  AND (sqlc.arg(organization_id) = '' OR organization_id = sqlc.arg(organization_id))
  AND (sqlc.arg(name) = '' OR name = sqlc.arg(name))
  AND (sqlc.narg(from_time) IS NULL OR created_at >= sqlc.narg(from_time))
  AND (sqlc.narg(to_time) IS NULL OR created_at < sqlc.narg(to_time))
  AND (
      sqlc.arg(before_id) = ''
      OR (created_at, id) < (sqlc.arg(before_time), sqlc.arg(before_id))
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: DeleteEventsBefore :execrows
-- One bounded batch; the prune repeats it until it deletes fewer than a batch.
DELETE FROM events WHERE id IN (
    SELECT e2.id FROM events AS e2 WHERE e2.created_at < ? ORDER BY e2.created_at LIMIT ?
);
