-- name: UpsertSession :exec
INSERT INTO sessions (id, project, title, tokens, cost, prompt_tokens, completion_tokens, station_tokens, model, provider, status, worktree_branch, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'))
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    tokens = excluded.tokens,
    cost = excluded.cost,
    prompt_tokens = excluded.prompt_tokens,
    completion_tokens = excluded.completion_tokens,
    station_tokens = excluded.station_tokens,
    model = excluded.model,
    provider = excluded.provider,
    status = excluded.status,
    worktree_branch = excluded.worktree_branch,
    updated_at = strftime('%s', 'now');

-- name: UpsertSessionTitleAndUsage :exec
INSERT INTO sessions (id, project, title, tokens, cost, prompt_tokens, completion_tokens, station_tokens, model, provider, status, worktree_branch, created_at, updated_at)
VALUES (?, ?, ?, 0, ?, ?, ?, 0, '', '', 'active', '', strftime('%s', 'now'), strftime('%s', 'now'))
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    prompt_tokens = sessions.prompt_tokens + excluded.prompt_tokens,
    completion_tokens = sessions.completion_tokens + excluded.completion_tokens,
    cost = sessions.cost + excluded.cost,
    updated_at = strftime('%s', 'now');

-- name: UpsertSessionUsage :exec
INSERT INTO sessions (id, project, title, tokens, cost, prompt_tokens, completion_tokens, station_tokens, model, provider, status, worktree_branch, created_at, updated_at)
VALUES (?, ?, '', ?, ?, ?, ?, ?, '', '', 'active', '', strftime('%s', 'now'), strftime('%s', 'now'))
ON CONFLICT(id) DO UPDATE SET
    tokens = sessions.tokens + excluded.tokens,
    cost = sessions.cost + excluded.cost,
    prompt_tokens = excluded.prompt_tokens,
    completion_tokens = excluded.completion_tokens,
    station_tokens = sessions.station_tokens + excluded.station_tokens,
    updated_at = strftime('%s', 'now');

-- name: UpdateWorktreeBranch :exec
UPDATE sessions SET worktree_branch = ? WHERE id = ?;

-- name: CompleteSession :exec
UPDATE sessions SET status = 'completed', updated_at = strftime('%s', 'now') WHERE id = ?;

-- name: AbandonOrphanSessions :exec
UPDATE sessions SET status = 'abandoned' WHERE project = ? AND status = 'active';

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: ListSessions :many
SELECT * FROM sessions ORDER BY updated_at DESC;

-- name: ListSessionsByProject :many
SELECT * FROM sessions WHERE project = ? ORDER BY updated_at DESC;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = ?;

-- name: ListSessionIDs :many
SELECT id FROM sessions;
