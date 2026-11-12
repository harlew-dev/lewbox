-- +goose Up
-- A subscription is a plan in one region (DECISIONS D20). An organization can
-- use a region only while it has one there.
CREATE TABLE subscriptions (
    org_id        TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id    TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    plan_id       TEXT NOT NULL REFERENCES plans(id),
    max_sandboxes INTEGER,          -- per-subscription overrides; NULL = the plan's value
    max_vcpus     INTEGER,
    max_memory_mb INTEGER,
    created_at    DATETIME NOT NULL,
    PRIMARY KEY (org_id, cluster_id)
);
CREATE INDEX subscriptions_plan ON subscriptions(plan_id);

-- Every organization on a plan gets it on every existing cluster.
INSERT INTO subscriptions (org_id, cluster_id, plan_id, max_sandboxes, max_vcpus, max_memory_mb, created_at)
SELECT organizations.id, clusters.id, organizations.plan_id,
       organizations.max_sandboxes, organizations.max_vcpus, organizations.max_memory_mb,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM organizations
CROSS JOIN clusters
WHERE organizations.plan_id IS NOT NULL;

-- New sign-ups get the default plan in these regions until it is changed.
INSERT INTO settings (key, value)
SELECT 'default_regions', COALESCE(json_group_array(slug), '[]')
FROM (SELECT slug FROM clusters ORDER BY slug)
WHERE true
ON CONFLICT(key) DO NOTHING;

ALTER TABLE organizations DROP COLUMN max_memory_mb;
ALTER TABLE organizations DROP COLUMN max_vcpus;
ALTER TABLE organizations DROP COLUMN max_sandboxes;
ALTER TABLE organizations DROP COLUMN plan_id;

-- +goose Down
ALTER TABLE organizations ADD COLUMN plan_id TEXT REFERENCES plans(id);
ALTER TABLE organizations ADD COLUMN max_sandboxes INTEGER;
ALTER TABLE organizations ADD COLUMN max_vcpus INTEGER;
ALTER TABLE organizations ADD COLUMN max_memory_mb INTEGER;
UPDATE organizations
SET plan_id = (SELECT plan_id FROM subscriptions WHERE subscriptions.org_id = organizations.id LIMIT 1),
    max_sandboxes = (SELECT max_sandboxes FROM subscriptions WHERE subscriptions.org_id = organizations.id LIMIT 1),
    max_vcpus = (SELECT max_vcpus FROM subscriptions WHERE subscriptions.org_id = organizations.id LIMIT 1),
    max_memory_mb = (SELECT max_memory_mb FROM subscriptions WHERE subscriptions.org_id = organizations.id LIMIT 1);
DELETE FROM settings WHERE key = 'default_regions';
DROP INDEX subscriptions_plan;
DROP TABLE subscriptions;
