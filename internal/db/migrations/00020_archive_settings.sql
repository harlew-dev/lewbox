-- +goose Up
-- Two archive settings per cluster (plans/08-dns-storage-archive.md).
--
-- archive_local_retention: how long a cluster's nodes keep their local copies
-- of archived sandboxes before the cleanup timer removes them:
-- instant | 1h | 1d | 7d | never. The bucket always holds the archive; a
-- local copy only makes a restore on that node faster.
--
-- archive_snapshot_name: how the archiver names its snapshots: id
-- (archive-<sandbox id>) or name (archive-<sandbox name>-<sandbox id>).
ALTER TABLE clusters ADD COLUMN archive_local_retention TEXT NOT NULL DEFAULT 'instant';
ALTER TABLE clusters ADD COLUMN archive_snapshot_name TEXT NOT NULL DEFAULT 'id';

-- The retention a node last applied with its storage settings. NULL is a node
-- that reported none (one configured before this setting existed: instant).
ALTER TABLE nodes ADD COLUMN settings_archive_retention TEXT;

-- +goose Down
ALTER TABLE nodes DROP COLUMN settings_archive_retention;
ALTER TABLE clusters DROP COLUMN archive_snapshot_name;
ALTER TABLE clusters DROP COLUMN archive_local_retention;
