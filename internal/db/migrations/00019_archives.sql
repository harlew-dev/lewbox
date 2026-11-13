-- +goose Up
-- Stopped sandboxes on an archive cluster, snapshotted to the bucket
-- (DECISIONS D36, docs/design/snapshot-archive.md).
CREATE TABLE archives (
    id                       TEXT PRIMARY KEY,                                   -- UUIDv7
    org_id                   TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id               TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    sandbox_id               TEXT NOT NULL,
    sandbox_name             TEXT NOT NULL DEFAULT '',
    image                    TEXT NOT NULL DEFAULT '',
    runtime                  TEXT NOT NULL DEFAULT '',
    cpu                      REAL NOT NULL DEFAULT 0,
    memory_mb                INTEGER NOT NULL DEFAULT 0,
    disk_gb                  INTEGER NOT NULL DEFAULT 0,
    tags                     TEXT NOT NULL DEFAULT '{}',                         -- JSON object
    snapshot_name            TEXT NOT NULL,                                      -- archive-<sandbox id>
    registry_ref             TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL,   -- pending | pushing | archived | failed | cancelled | deleted
    error                    TEXT NOT NULL DEFAULT '',
    attempts                 INTEGER NOT NULL DEFAULT 0,
    next_attempt_at          DATETIME,
    pushing_since            DATETIME,
    created_at               DATETIME NOT NULL,
    archived_at              DATETIME,
    deleted_at               DATETIME,
    last_restored_sandbox_id TEXT NOT NULL DEFAULT '',
    last_restored_at         DATETIME
);
CREATE UNIQUE INDEX archives_live ON archives(cluster_id, sandbox_id) WHERE status NOT IN ('cancelled', 'deleted');
CREATE INDEX archives_org ON archives(org_id, created_at);
CREATE INDEX archives_due ON archives(status, next_attempt_at);

-- +goose Down
DROP TABLE archives;
