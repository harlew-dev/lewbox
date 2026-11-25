-- +goose Up
-- The audit trail: one row for every state change made through the service,
-- written in the same transaction as the change. Actor and target are public
-- IDs; actor_label keeps a human name after the account or key is deleted.
-- Passwords, hashes, tokens and other secrets are never stored here.
CREATE TABLE events (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,              -- <resource>.<past-tense fact>
    actor_kind      TEXT NOT NULL,              -- user, access_token, admin_key, local, system or anonymous
    actor_id        TEXT,                       -- account or key ID; NULL once unknown
    actor_label     TEXT NOT NULL DEFAULT '',   -- email or key name at the time
    target_type     TEXT NOT NULL,              -- user, workspace, plan, ...
    target_id       TEXT NOT NULL DEFAULT '',   -- public ID or name; empty when the target is unknown
    target_name     TEXT NOT NULL DEFAULT '',
    organization_id TEXT REFERENCES organizations(id) ON DELETE SET NULL,
    changes         TEXT,                       -- JSON {field: {from, to}}; NULL when nothing changed
    ip              TEXT,                       -- the only place a client address is kept
    request_id      TEXT,
    created_at      DATETIME NOT NULL,
    -- The code checks the same shape. GLOB keeps a bad name out of the trail
    -- even through a path that skips that check.
    CHECK (
        length(name) BETWEEN 3 AND 80
        AND name GLOB '[a-z]*'
        AND name NOT GLOB '*[^a-z_.]*'
        AND name NOT GLOB '*..*'
        AND name NOT GLOB '.*'
        AND name NOT GLOB '*.'
        AND name NOT GLOB '*.*.*.*'
    )
);
CREATE INDEX events_created ON events (created_at DESC, id DESC);
CREATE INDEX events_actor ON events (actor_id, created_at DESC, id DESC);
CREATE INDEX events_organization ON events (organization_id, created_at DESC, id DESC);
CREATE INDEX events_name ON events (name, created_at DESC, id DESC);

-- +goose Down
DROP TABLE events;
