-- +goose Up
-- A workspace's plan is split between its members (DECISIONS D25–D28). Each
-- member has an AerolVM team of their own, so shares and usage are per member.
CREATE TABLE member_shares (
    membership_id TEXT NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
    cluster_id    TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    max_sandboxes INTEGER,   -- NULL = automatic
    max_vcpus     REAL,
    max_memory_mb INTEGER,
    PRIMARY KEY (membership_id, cluster_id)
);

ALTER TABLE sandbox_keys ADD COLUMN membership_id TEXT REFERENCES memberships(id) ON DELETE CASCADE;

ALTER TABLE sandboxes ADD COLUMN membership_id TEXT;

-- usage_hourly gains a member dimension; its primary key changes, so rebuild.
CREATE TABLE usage_hourly_new (
    org_id            TEXT NOT NULL,
    membership_id     TEXT NOT NULL DEFAULT '',
    cluster_id        TEXT NOT NULL,
    hour              DATETIME NOT NULL,
    sandbox_seconds   INTEGER NOT NULL DEFAULT 0,
    vcpu_seconds      REAL NOT NULL DEFAULT 0,
    memory_mb_seconds INTEGER NOT NULL DEFAULT 0,
    peak_sandboxes    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, membership_id, cluster_id, hour)
);
INSERT INTO usage_hourly_new (org_id, membership_id, cluster_id, hour, sandbox_seconds,
                              vcpu_seconds, memory_mb_seconds, peak_sandboxes)
SELECT org_id, '', cluster_id, hour, sandbox_seconds, vcpu_seconds, memory_mb_seconds, peak_sandboxes
FROM usage_hourly;
DROP TABLE usage_hourly;
ALTER TABLE usage_hourly_new RENAME TO usage_hourly;

-- Quotas are pushed per member team, so quota_state is per membership too.
CREATE TABLE quota_state_new (
    org_id        TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    membership_id TEXT NOT NULL DEFAULT '',
    cluster_id    TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    quota         TEXT NOT NULL,
    pushed_at     DATETIME NOT NULL,
    error         TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, membership_id, cluster_id)
);
INSERT INTO quota_state_new (org_id, membership_id, cluster_id, quota, pushed_at, error)
SELECT org_id, '', cluster_id, quota, pushed_at, error FROM quota_state;
DROP TABLE quota_state;
ALTER TABLE quota_state_new RENAME TO quota_state;

-- The workspace no longer has a team of its own.
DROP INDEX organizations_team;
ALTER TABLE organizations DROP COLUMN team;

-- +goose Down
ALTER TABLE organizations ADD COLUMN team TEXT;
UPDATE organizations SET team = 'lbx-' || replace(id, '-', '');
CREATE UNIQUE INDEX organizations_team ON organizations(team);

CREATE TABLE quota_state_old (
    org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    quota      TEXT NOT NULL,
    pushed_at  DATETIME NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, cluster_id)
);
INSERT INTO quota_state_old (org_id, cluster_id, quota, pushed_at, error)
SELECT org_id, cluster_id, quota, pushed_at, error FROM quota_state GROUP BY org_id, cluster_id;
DROP TABLE quota_state;
ALTER TABLE quota_state_old RENAME TO quota_state;

CREATE TABLE usage_hourly_old (
    org_id            TEXT NOT NULL,
    cluster_id        TEXT NOT NULL,
    hour              DATETIME NOT NULL,
    sandbox_seconds   INTEGER NOT NULL DEFAULT 0,
    vcpu_seconds      REAL NOT NULL DEFAULT 0,
    memory_mb_seconds INTEGER NOT NULL DEFAULT 0,
    peak_sandboxes    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, cluster_id, hour)
);
INSERT INTO usage_hourly_old (org_id, cluster_id, hour, sandbox_seconds, vcpu_seconds,
                              memory_mb_seconds, peak_sandboxes)
SELECT org_id, cluster_id, hour, SUM(sandbox_seconds), SUM(vcpu_seconds),
       SUM(memory_mb_seconds), MAX(peak_sandboxes)
FROM usage_hourly
GROUP BY org_id, cluster_id, hour;
DROP TABLE usage_hourly;
ALTER TABLE usage_hourly_old RENAME TO usage_hourly;

ALTER TABLE sandboxes DROP COLUMN membership_id;
ALTER TABLE sandbox_keys DROP COLUMN membership_id;
DROP TABLE member_shares;
