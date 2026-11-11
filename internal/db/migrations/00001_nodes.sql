-- +goose Up
CREATE TABLE nodes (
    id         TEXT PRIMARY KEY,           -- UUIDv7, canonical lowercase text
    name       TEXT NOT NULL UNIQUE,       -- DNS-label style, e.g. eu-west-1
    url        TEXT NOT NULL,              -- the node's AerolVM API URL
    created_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE nodes;
