-- +goose Up
ALTER TABLE sessions ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN station_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE sessions ADD COLUMN worktree_branch TEXT NOT NULL DEFAULT '';

-- +goose Down
CREATE TABLE sessions_new (
    id TEXT PRIMARY KEY,
    project TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    tokens INTEGER NOT NULL DEFAULT 0,
    cost REAL NOT NULL DEFAULT 0.0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
INSERT INTO sessions_new (id, project, title, tokens, cost, created_at, updated_at)
    SELECT id, project, title, tokens, cost, created_at, updated_at FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;
CREATE INDEX idx_sessions_project ON sessions(project);
CREATE INDEX idx_sessions_updated_at ON sessions(updated_at DESC);
