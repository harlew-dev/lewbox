-- +goose Up
-- Single-use links that let someone register and start a workspace on a plan
-- in a region (DECISIONS D33). Only the token's hash is stored.
CREATE TABLE setup_links (
    id         TEXT PRIMARY KEY,                    -- UUIDv7
    token_hash TEXT NOT NULL UNIQUE,                -- hex SHA-256 of the token
    plan_id    TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    note       TEXT NOT NULL DEFAULT '',
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    claimed_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    claimed_at DATETIME,
    used_at    DATETIME,
    org_id     TEXT REFERENCES organizations(id) ON DELETE SET NULL
);
CREATE INDEX setup_links_created_at ON setup_links(created_at);

-- +goose Down
DROP INDEX setup_links_created_at;
DROP TABLE setup_links;
