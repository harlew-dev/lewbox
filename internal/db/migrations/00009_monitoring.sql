-- +goose Up
-- One row per check for a cluster's health, and one per member's capacity.
CREATE TABLE cluster_samples (
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    at         DATETIME NOT NULL,
    status     TEXT NOT NULL,
    latency_ms INTEGER NOT NULL,
    PRIMARY KEY (cluster_id, at)
);

CREATE TABLE node_samples (
    node_id         TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    at              DATETIME NOT NULL,
    alive           INTEGER NOT NULL,
    sandboxes       INTEGER NOT NULL DEFAULT 0,
    cpu_cores       INTEGER NOT NULL DEFAULT 0,
    cpu_reserved    REAL NOT NULL DEFAULT 0,
    memory_total_mb INTEGER NOT NULL DEFAULT 0,
    memory_free_mb  INTEGER NOT NULL DEFAULT 0,
    disk_free_gb    INTEGER NOT NULL DEFAULT 0,
    can_admit       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (node_id, at)
);

-- +goose Down
DROP TABLE node_samples;
DROP TABLE cluster_samples;
