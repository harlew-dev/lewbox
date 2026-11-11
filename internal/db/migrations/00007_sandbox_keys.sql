-- +goose Up
-- A customer's runner token on one cluster. The token itself is never stored.
CREATE TABLE sandbox_keys (
    id           TEXT PRIMARY KEY,                    -- UUIDv7
    org_id       TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cluster_id   TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    upstream_id  TEXT NOT NULL,                       -- AerolVM tok_…
    last4        TEXT NOT NULL,
    created_by   TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at   DATETIME NOT NULL,
    expires_at   DATETIME,                            -- NULL = never
    revoked_at   DATETIME
);
CREATE INDEX sandbox_keys_org ON sandbox_keys(org_id);
CREATE UNIQUE INDEX sandbox_keys_live_name ON sandbox_keys(org_id, name) WHERE revoked_at IS NULL;

-- +goose Down
DROP INDEX sandbox_keys_live_name;
DROP INDEX sandbox_keys_org;
DROP TABLE sandbox_keys;
