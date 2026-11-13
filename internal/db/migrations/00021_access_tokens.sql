-- +goose Up
-- Access tokens (plans/DECISIONS.md D38): long-lived tokens for the Lewbox
-- API that act as one account, with that account's current rights. An
-- instance admin's token can manage clusters and nodes; an admin key cannot.
CREATE TABLE access_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,                 -- lbp_ and the first 8 hex characters
    token_hash   TEXT NOT NULL UNIQUE,          -- hex SHA-256 of the token
    last4        TEXT NOT NULL,
    created_at   DATETIME NOT NULL,
    last_used_at DATETIME,
    expires_at   DATETIME,                      -- NULL: never
    revoked_at   DATETIME
);
CREATE INDEX access_tokens_user_id ON access_tokens(user_id);
-- A live token's name is unique for its account, so it can be revoked by
-- name. Revoked tokens keep their names.
CREATE UNIQUE INDEX access_tokens_live_name ON access_tokens(user_id, name) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE access_tokens;
