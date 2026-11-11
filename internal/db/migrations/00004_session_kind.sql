-- +goose Up
-- "cli" sessions are bearer tokens; "web" sessions are browser cookies with
-- an idle timeout. Existing rows predate the web console, so "cli" is right.
ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'cli';

-- +goose Down
ALTER TABLE sessions DROP COLUMN kind;
