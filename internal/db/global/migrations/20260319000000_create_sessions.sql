-- +goose Up
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    project TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    tokens INTEGER NOT NULL DEFAULT 0,
    cost REAL NOT NULL DEFAULT 0.0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX idx_sessions_project ON sessions(project);
CREATE INDEX idx_sessions_updated_at ON sessions(updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS sessions;
