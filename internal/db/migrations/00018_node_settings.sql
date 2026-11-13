-- +goose Up
-- What a node last reported it applied, so a node whose cluster's profiles
-- changed shows as outdated (plans/08-dns-storage-archive.md, "Nodes").
-- The profile columns hold the profile's ID, or its name when the profile is
-- gone; NULL means the node applied none.
ALTER TABLE nodes ADD COLUMN settings_applied_at DATETIME;
ALTER TABLE nodes ADD COLUMN settings_storage_profile_id TEXT;
ALTER TABLE nodes ADD COLUMN settings_storage_version INTEGER;
ALTER TABLE nodes ADD COLUMN settings_dns_profile_id TEXT;
ALTER TABLE nodes ADD COLUMN settings_dns_version INTEGER;

-- +goose Down
ALTER TABLE nodes DROP COLUMN settings_dns_version;
ALTER TABLE nodes DROP COLUMN settings_dns_profile_id;
ALTER TABLE nodes DROP COLUMN settings_storage_version;
ALTER TABLE nodes DROP COLUMN settings_storage_profile_id;
ALTER TABLE nodes DROP COLUMN settings_applied_at;
