-- +goose Up
-- Saved Cloudflare credentials, chosen per cluster (DECISIONS D34). The API
-- token is sealed with the keyring and only its last four characters are
-- shown after saving.
CREATE TABLE dns_profiles (
    id            TEXT PRIMARY KEY,                -- UUIDv7
    name          TEXT NOT NULL UNIQUE,            -- label style, never a UUID
    provider      TEXT NOT NULL,                   -- 'cloudflare'
    zone          TEXT NOT NULL,                   -- e.g. example.com
    zone_id       TEXT NOT NULL,                   -- Cloudflare zone id, from the last good test
    api_token_enc BLOB NOT NULL,                   -- keyring.Seal(token)
    token_last4   TEXT NOT NULL,
    acme_email    TEXT NOT NULL DEFAULT '',
    version       INTEGER NOT NULL DEFAULT 1,      -- +1 whenever zone or token changes
    tested_at     DATETIME,
    test_error    TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

-- Cluster settings that use a profile. Only the delete guard and the profile
-- list read these for now; domain-mode installs and DNS records come later.
ALTER TABLE clusters ADD COLUMN domain TEXT NOT NULL DEFAULT '';
ALTER TABLE clusters ADD COLUMN dns_profile_id TEXT REFERENCES dns_profiles(id);
ALTER TABLE clusters ADD COLUMN dns_records TEXT NOT NULL DEFAULT 'auto'; -- auto | off

-- +goose Down
ALTER TABLE clusters DROP COLUMN dns_records;
ALTER TABLE clusters DROP COLUMN dns_profile_id;
ALTER TABLE clusters DROP COLUMN domain;
DROP TABLE dns_profiles;
