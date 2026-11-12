-- +goose Up
-- A workspace is in one region (DECISIONS D31). Keep its oldest subscription;
-- the index is the real guard.
DELETE FROM subscriptions
WHERE EXISTS (
    SELECT 1 FROM subscriptions AS older
    WHERE older.org_id = subscriptions.org_id
      AND (older.created_at < subscriptions.created_at
           OR (older.created_at = subscriptions.created_at
               AND older.cluster_id < subscriptions.cluster_id))
);
CREATE UNIQUE INDEX subscriptions_one_per_org ON subscriptions(org_id);

-- New sign-ups get the default plan in this region until it is changed.
INSERT INTO settings (key, value)
SELECT 'default_region',
       CASE
           WHEN json_array_length(value) > 0 THEN json_quote(json_extract(value, '$[0]'))
           ELSE 'null'
       END
FROM settings
WHERE key = 'default_regions';
DELETE FROM settings WHERE key = 'default_regions';

-- +goose Down
INSERT INTO settings (key, value)
SELECT 'default_regions',
       CASE
           WHEN json_type(value) = 'text' THEN json_array(json_extract(value, '$'))
           ELSE '[]'
       END
FROM settings
WHERE key = 'default_region';
DELETE FROM settings WHERE key = 'default_region';
DROP INDEX subscriptions_one_per_org;
