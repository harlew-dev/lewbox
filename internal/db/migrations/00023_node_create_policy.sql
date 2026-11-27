-- +goose Up
-- The runtime a node's AerolVM create policy allowed when it last applied its
-- node settings. NULL is a node that reported none (one configured before
-- Lewbox wrote the policy): it shows as outdated until `lewbox node
-- configure` runs there.
ALTER TABLE nodes ADD COLUMN settings_policy_runtime TEXT;

-- +goose Down
ALTER TABLE nodes DROP COLUMN settings_policy_runtime;
