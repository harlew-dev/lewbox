-- +goose Up
-- The Cloudflare records Lewbox created for a cluster's domain (DECISIONS
-- D34). A row is what lets Lewbox delete only its own records: the cluster
-- check reconciles this table against the cluster's live members.
CREATE TABLE dns_records (
    id         TEXT PRIMARY KEY,                     -- UUIDv7
    cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    profile_id TEXT NOT NULL REFERENCES dns_profiles(id),
    zone_id    TEXT NOT NULL,
    node       TEXT NOT NULL,                        -- the member's node id/name
    name       TEXT NOT NULL,                        -- FQDN: <domain> or *.<domain>
    type       TEXT NOT NULL,                        -- A | AAAA
    content    TEXT NOT NULL,                        -- the IP
    record_id  TEXT NOT NULL,                        -- Cloudflare's id
    created_at DATETIME NOT NULL,
    UNIQUE (cluster_id, name, type, content)
);

-- +goose Down
DROP TABLE dns_records;
