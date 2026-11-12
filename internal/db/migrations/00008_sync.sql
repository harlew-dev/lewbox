-- +goose Up
-- The sandbox mirror, hourly usage and what Lewbox last pushed as a quota.
CREATE TABLE sandboxes (
    cluster_id    TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    id            TEXT NOT NULL,
    org_id        TEXT REFERENCES organizations(id) ON DELETE SET NULL,
    team          TEXT NOT NULL DEFAULT '',
    node          TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    image         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    status_reason TEXT NOT NULL DEFAULT '',
    runtime       TEXT NOT NULL DEFAULT '',
    cpu           REAL NOT NULL DEFAULT 0,
    memory_mb     INTEGER NOT NULL DEFAULT 0,
    disk_gb       INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL,
    last_seen_at  DATETIME NOT NULL,
    gone_at       DATETIME,
    PRIMARY KEY (cluster_id, id)
);
CREATE INDEX sandboxes_org ON sandboxes(org_id, gone_at);

CREATE TABLE usage_hourly (
    org_id            TEXT NOT NULL,
    cluster_id        TEXT NOT NULL,
    hour              DATETIME NOT NULL,
    sandbox_seconds   INTEGER NOT NULL DEFAULT 0,
    vcpu_seconds      REAL NOT NULL DEFAULT 0,
    memory_mb_seconds INTEGER NOT NULL DEFAULT 0,
    peak_sandboxes    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, cluster_id, hour)
);

CREATE TABLE quota_state (
    org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    quota      TEXT NOT NULL,
    pushed_at  DATETIME NOT NULL,
    error      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (org_id, cluster_id)
);

ALTER TABLE clusters ADD COLUMN synced_at DATETIME;
ALTER TABLE clusters ADD COLUMN sync_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE clusters DROP COLUMN sync_error;
ALTER TABLE clusters DROP COLUMN synced_at;
DROP TABLE quota_state;
DROP TABLE usage_hourly;
DROP TABLE sandboxes;
