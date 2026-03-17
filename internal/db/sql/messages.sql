-- name: DeleteSessionMessages :exec
DELETE FROM messages
WHERE session_id = ?;
