-- name: CreateStorageProfile :one
INSERT INTO storage_profiles (id, name, provider, endpoint, region, bucket, prefix, path_style,
                              access_key_id, secret_key_enc, secret_last4, version, tested_at,
                              test_error, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetStorageProfileByID :one
SELECT * FROM storage_profiles WHERE id = ?;

-- name: GetStorageProfileByName :one
SELECT * FROM storage_profiles WHERE name = ?;

-- name: ListStorageProfiles :many
SELECT * FROM storage_profiles ORDER BY name;

-- name: UpdateStorageProfile :one
UPDATE storage_profiles
SET name = ?, endpoint = ?, region = ?, bucket = ?, prefix = ?, path_style = ?,
    access_key_id = ?, secret_key_enc = ?, secret_last4 = ?, version = ?, tested_at = ?,
    test_error = ?, updated_at = ?
WHERE id = ?
RETURNING *;

-- name: RecordStorageProfileTest :exec
UPDATE storage_profiles
SET tested_at = ?, test_error = ?
WHERE id = ?;

-- name: CountStorageProfileClusters :many
SELECT storage_profile_id AS profile_id, COUNT(*) AS clusters
FROM clusters
WHERE storage_profile_id IS NOT NULL
GROUP BY storage_profile_id;

-- name: DeleteStorageProfile :execrows
-- Refuses a profile a cluster uses. The guard is in the statement, so a
-- cluster that starts using the profile is not left pointing at nothing.
DELETE FROM storage_profiles
WHERE storage_profiles.id = sqlc.arg(profile_id)
  AND NOT EXISTS (SELECT 1 FROM clusters WHERE clusters.storage_profile_id = sqlc.arg(profile_id));
