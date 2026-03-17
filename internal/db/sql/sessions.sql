-- name: CreateSession :one
INSERT INTO sessions (
    id,
    title,
    updated_at,
    created_at
) VALUES (
    ?,
    ?,
    strftime('%s', 'now'),
    strftime('%s', 'now')
) RETURNING *;

-- name: GetSessionByID :one
SELECT *
FROM sessions
WHERE id = ? LIMIT 1;

-- name: ListSessions :many
SELECT *
FROM sessions
ORDER BY updated_at DESC;

-- name: UpdateSession :one
UPDATE sessions
SET
    title = ?,
    prompt_tokens = ?,
    completion_tokens = ?,
    total_tokens = ?,
    station_tokens = ?,
    cost = ?,
    todos = ?
WHERE id = ?
RETURNING *;

-- name: UpdateSessionTitleAndUsage :exec
UPDATE sessions
SET
    title = ?,
    prompt_tokens = prompt_tokens + ?,
    completion_tokens = completion_tokens + ?,
    cost = cost + ?
WHERE id = ?;


-- name: UpdateSessionTodos :exec
UPDATE sessions
SET
    todos = ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;

-- name: UpdateSessionUsage :exec
UPDATE sessions
SET
    prompt_tokens = ?,
    completion_tokens = ?,
    total_tokens = total_tokens + ?,
    station_tokens = station_tokens + ?,
    cost = cost + ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE id = ?;
