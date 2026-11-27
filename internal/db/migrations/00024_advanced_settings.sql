-- +goose Up
-- Per-cluster AerolVM tuning Lewbox applies on every node. NULL means the
-- setting is off, or AerolVM's own default; docker_timeout always has a value
-- because Lewbox needs more than sandboxd's 180s default for large snapshot
-- commits.
ALTER TABLE clusters ADD COLUMN docker_timeout TEXT NOT NULL DEFAULT '15m';
ALTER TABLE clusters ADD COLUMN max_sandbox_cpu REAL;
ALTER TABLE clusters ADD COLUMN max_sandbox_memory_mb INTEGER;
ALTER TABLE clusters ADD COLUMN max_sandbox_disk_gb INTEGER;
ALTER TABLE clusters ADD COLUMN idle_stop_minutes INTEGER;
ALTER TABLE clusters ADD COLUMN destroy_if_idle_for TEXT;
ALTER TABLE clusters ADD COLUMN max_age TEXT;
ALTER TABLE clusters ADD COLUMN firecracker_vm_slots INTEGER;

-- The advanced settings a node last applied, as JSON. NULL is a node that
-- reported none (one configured before this setting existed): it shows as
-- outdated once its cluster sets a non-default value.
ALTER TABLE nodes ADD COLUMN settings_advanced TEXT;

-- +goose Down
ALTER TABLE nodes DROP COLUMN settings_advanced;
ALTER TABLE clusters DROP COLUMN firecracker_vm_slots;
ALTER TABLE clusters DROP COLUMN max_age;
ALTER TABLE clusters DROP COLUMN destroy_if_idle_for;
ALTER TABLE clusters DROP COLUMN idle_stop_minutes;
ALTER TABLE clusters DROP COLUMN max_sandbox_disk_gb;
ALTER TABLE clusters DROP COLUMN max_sandbox_memory_mb;
ALTER TABLE clusters DROP COLUMN max_sandbox_cpu;
ALTER TABLE clusters DROP COLUMN docker_timeout;
