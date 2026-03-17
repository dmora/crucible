-- +goose Up
-- +goose StatementBegin
-- Drop message_count triggers (column being removed).
DROP TRIGGER IF EXISTS update_session_message_count_on_insert;
DROP TRIGGER IF EXISTS update_session_message_count_on_delete;

-- Drop obsolete columns from sessions table.
-- parent_session_id: child sessions no longer used (ADK manages session hierarchy).
-- message_count: was maintained by trigger on messages table; ADK manages messages.
-- summary_message_id: summarization handled by ContextAwareSessionService.
ALTER TABLE sessions DROP COLUMN parent_session_id;
ALTER TABLE sessions DROP COLUMN message_count;
ALTER TABLE sessions DROP COLUMN summary_message_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT;
ALTER TABLE sessions ADD COLUMN message_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN summary_message_id TEXT;

CREATE TRIGGER IF NOT EXISTS update_session_message_count_on_insert
AFTER INSERT ON messages
BEGIN
UPDATE sessions SET message_count = message_count + 1
WHERE id = new.session_id;
END;

CREATE TRIGGER IF NOT EXISTS update_session_message_count_on_delete
AFTER DELETE ON messages
BEGIN
UPDATE sessions SET message_count = message_count - 1
WHERE id = old.session_id;
END;
-- +goose StatementEnd
