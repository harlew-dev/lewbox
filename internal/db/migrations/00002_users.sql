-- +goose Up
CREATE TABLE users (
    id             TEXT PRIMARY KEY,           -- UUIDv7, canonical lowercase text
    email          TEXT NOT NULL UNIQUE,       -- stored lowercased and trimmed
    password_hash  TEXT NOT NULL,              -- pbkdf2-sha256$<iter>$<salt>$<key>
    instance_admin INTEGER NOT NULL DEFAULT 0,
    disabled       INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL
);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,         -- hex SHA-256 of the bearer token
    created_at   DATETIME NOT NULL,
    expires_at   DATETIME NOT NULL,
    last_used_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
