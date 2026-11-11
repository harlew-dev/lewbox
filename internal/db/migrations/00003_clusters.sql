-- +goose Up
CREATE TABLE clusters (
    id                TEXT PRIMARY KEY,
    slug              TEXT NOT NULL UNIQUE,     -- DNS label, like node names
    name              TEXT NOT NULL,
    admin_url         TEXT NOT NULL,
    endpoint_url      TEXT NOT NULL,
    runtime           TEXT NOT NULL,            -- docker | firecracker
    admin_token_enc   BLOB NOT NULL,            -- secrets.Keyring.Seal(token)
    admin_token_id    TEXT NOT NULL,            -- AerolVM token id, from whoami (principal.id)
    admin_token_last4 TEXT NOT NULL,
    status            TEXT NOT NULL,            -- ready | degraded | unreachable
    status_detail     TEXT NOT NULL DEFAULT '',
    version           TEXT NOT NULL DEFAULT '',
    checks            TEXT NOT NULL DEFAULT '[]', -- JSON []Check from the last check
    checked_at        DATETIME,
    created_at        DATETIME NOT NULL
);

ALTER TABLE nodes ADD COLUMN cluster_id TEXT REFERENCES clusters(id) ON DELETE CASCADE;
ALTER TABLE nodes ADD COLUMN role TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN version TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN alive INTEGER;          -- NULL = unknown (legacy nodes added by URL)
ALTER TABLE nodes ADD COLUMN last_seen_at DATETIME;

-- +goose Down
ALTER TABLE nodes DROP COLUMN last_seen_at;
ALTER TABLE nodes DROP COLUMN alive;
ALTER TABLE nodes DROP COLUMN version;
ALTER TABLE nodes DROP COLUMN role;
ALTER TABLE nodes DROP COLUMN cluster_id;
DROP TABLE clusters;
