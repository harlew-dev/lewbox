-- +goose Up
-- Instance settings, one row per key. Values are JSON.
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE plans (
    id            TEXT PRIMARY KEY,           -- UUIDv7
    slug          TEXT NOT NULL UNIQUE,       -- DNS label, never a UUID
    name          TEXT NOT NULL,
    max_sandboxes INTEGER,                    -- NULL = unlimited
    max_vcpus     INTEGER,                    -- NULL = unlimited (whole vCPUs)
    max_memory_mb INTEGER,                    -- NULL = unlimited
    created_at    DATETIME NOT NULL
);

-- An organization is a customer: a plan, optional per-organization limits
-- that override it, and the AerolVM team its sandboxes will live in.
ALTER TABLE organizations ADD COLUMN team TEXT;
ALTER TABLE organizations ADD COLUMN plan_id TEXT REFERENCES plans(id);
ALTER TABLE organizations ADD COLUMN max_sandboxes INTEGER;
ALTER TABLE organizations ADD COLUMN max_vcpus INTEGER;
ALTER TABLE organizations ADD COLUMN max_memory_mb INTEGER;
ALTER TABLE organizations ADD COLUMN status_reason TEXT NOT NULL DEFAULT '';
UPDATE organizations SET team = 'lbx-' || replace(id, '-', '');
CREATE UNIQUE INDEX organizations_team ON organizations(team);

-- The plan every new sign-up gets until the operator changes it.
INSERT INTO plans (id, slug, name, max_sandboxes, max_vcpus, max_memory_mb, created_at)
VALUES ('01970000-0000-7000-8000-000000000001', 'free', 'Free', 2, 2, 4096, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
UPDATE organizations SET plan_id = '01970000-0000-7000-8000-000000000001';

INSERT INTO settings (key, value) VALUES
    ('default_plan', '"free"'),
    ('signup', '"closed"'),
    ('instance_name', '"Lewbox"');

-- +goose Down
DROP INDEX organizations_team;
ALTER TABLE organizations DROP COLUMN status_reason;
ALTER TABLE organizations DROP COLUMN max_memory_mb;
ALTER TABLE organizations DROP COLUMN max_vcpus;
ALTER TABLE organizations DROP COLUMN max_sandboxes;
ALTER TABLE organizations DROP COLUMN plan_id;
ALTER TABLE organizations DROP COLUMN team;
DROP TABLE plans;
DROP TABLE settings;
