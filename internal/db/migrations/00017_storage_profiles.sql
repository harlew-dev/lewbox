-- +goose Up
-- Saved S3-compatible bucket credentials, chosen per cluster (DECISIONS D35).
-- The secret access key is sealed with the keyring and only its last four
-- characters are shown after saving.
CREATE TABLE storage_profiles (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL UNIQUE,
    provider          TEXT NOT NULL,          -- 's3'
    endpoint          TEXT NOT NULL DEFAULT '',
    region            TEXT NOT NULL,
    bucket            TEXT NOT NULL,
    prefix            TEXT NOT NULL DEFAULT '',
    path_style        INTEGER NOT NULL DEFAULT 0,
    access_key_id     TEXT NOT NULL,
    secret_key_enc    BLOB NOT NULL,
    secret_last4      TEXT NOT NULL,
    version           INTEGER NOT NULL DEFAULT 1,  -- +1 when endpoint, region, bucket, prefix, path_style, key id or secret changes
    tested_at         DATETIME,
    test_error        TEXT NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL
);

-- Cluster settings that use a profile. Only the delete guard and the profile
-- list read these for now; archiving stopped sandboxes comes later.
ALTER TABLE clusters ADD COLUMN storage_profile_id TEXT REFERENCES storage_profiles(id);
ALTER TABLE clusters ADD COLUMN stop_policy TEXT NOT NULL DEFAULT 'keep';   -- keep | archive

-- +goose Down
ALTER TABLE clusters DROP COLUMN stop_policy;
ALTER TABLE clusters DROP COLUMN storage_profile_id;
DROP TABLE storage_profiles;
