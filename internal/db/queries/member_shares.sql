-- name: ListMemberShares :many
SELECT member_shares.* FROM member_shares
JOIN memberships ON memberships.id = member_shares.membership_id
WHERE memberships.org_id = ?;

-- name: UpsertMemberShare :exec
INSERT INTO member_shares (membership_id, cluster_id, max_sandboxes, max_vcpus, max_memory_mb)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(membership_id, cluster_id) DO UPDATE SET
    max_sandboxes = excluded.max_sandboxes,
    max_vcpus = excluded.max_vcpus,
    max_memory_mb = excluded.max_memory_mb;
