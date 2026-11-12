-- +goose Up
-- The default plan is stored by ID, so renaming a plan's slug keeps it the
-- default. The API still shows and accepts the slug.
UPDATE settings
SET value = COALESCE((SELECT json_quote(id) FROM plans WHERE slug = json_extract(settings.value, '$')), settings.value)
WHERE key = 'default_plan';

-- +goose Down
UPDATE settings
SET value = COALESCE((SELECT json_quote(slug) FROM plans WHERE id = json_extract(settings.value, '$')), settings.value)
WHERE key = 'default_plan';
