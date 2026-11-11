-- +goose Up
-- Organizations, their members and their admin keys (MVP §2, §6, §12).
CREATE TABLE organizations (
    id         TEXT PRIMARY KEY,                -- UUIDv7
    slug       TEXT NOT NULL UNIQUE,            -- DNS label, like cluster slugs
    name       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'active',  -- active | suspended
    created_at DATETIME NOT NULL
);

-- An account's role in an organization.
CREATE TABLE memberships (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,                   -- owner | admin | member | viewer
    created_at DATETIME NOT NULL,
    UNIQUE (org_id, user_id)
);
CREATE INDEX memberships_user_id ON memberships(user_id);

-- An organization's keys for the Lewbox API. Only the SHA-256 of the token
-- is stored, with enough of it to recognise (prefix, last4).
CREATE TABLE admin_keys (
    id           TEXT PRIMARY KEY,
    org_id       TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,                 -- lba_ and the first 8 hex characters
    key_hash     TEXT NOT NULL UNIQUE,          -- hex SHA-256 of the token
    last4        TEXT NOT NULL,
    created_by   TEXT REFERENCES users(id) ON DELETE SET NULL,  -- NULL: local mode
    created_at   DATETIME NOT NULL,
    last_used_at DATETIME,
    expires_at   DATETIME,                      -- NULL: never
    revoked_at   DATETIME
);
CREATE INDEX admin_keys_org_id ON admin_keys(org_id);
-- A live key's name is unique in its organization, so it can be revoked by
-- name. Revoked keys keep their names.
CREATE UNIQUE INDEX admin_keys_live_name ON admin_keys(org_id, name) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE admin_keys;
DROP TABLE memberships;
DROP TABLE organizations;
