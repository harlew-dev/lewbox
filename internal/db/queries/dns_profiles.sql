-- name: CreateDNSProfile :one
INSERT INTO dns_profiles (id, name, provider, zone, zone_id, api_token_enc, token_last4,
                          acme_email, version, tested_at, test_error, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDNSProfileByID :one
SELECT * FROM dns_profiles WHERE id = ?;

-- name: GetDNSProfileByName :one
SELECT * FROM dns_profiles WHERE name = ?;

-- name: ListDNSProfiles :many
SELECT * FROM dns_profiles ORDER BY name;

-- name: UpdateDNSProfile :one
UPDATE dns_profiles
SET name = ?, zone = ?, zone_id = ?, api_token_enc = ?, token_last4 = ?, acme_email = ?,
    version = ?, tested_at = ?, test_error = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: RecordDNSProfileTest :exec
UPDATE dns_profiles
SET tested_at = ?, test_error = ?, zone_id = ?
WHERE id = ?;

-- name: CountDNSProfileClusters :many
SELECT dns_profile_id AS profile_id, COUNT(*) AS clusters
FROM clusters
WHERE dns_profile_id IS NOT NULL
GROUP BY dns_profile_id;

-- name: DeleteDNSProfile :execrows
-- Refuses a profile a cluster uses. The guard is in the statement, so a
-- cluster that starts using the profile is not left pointing at nothing.
DELETE FROM dns_profiles
WHERE dns_profiles.id = sqlc.arg(profile_id)
  AND NOT EXISTS (SELECT 1 FROM clusters WHERE clusters.dns_profile_id = sqlc.arg(profile_id));
