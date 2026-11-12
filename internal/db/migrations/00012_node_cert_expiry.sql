-- +goose Up
ALTER TABLE node_samples ADD COLUMN cert_expires_at DATETIME;

-- +goose Down
ALTER TABLE node_samples DROP COLUMN cert_expires_at;
