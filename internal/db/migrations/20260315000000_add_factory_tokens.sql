-- +goose Up
ALTER TABLE sessions ADD COLUMN total_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN station_tokens INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN station_tokens;
ALTER TABLE sessions DROP COLUMN total_tokens;
