-- name: CreateMessage :exec
INSERT INTO messages (id, client_id, urgency, title, blocks, source, idempotency_key, request_id, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMessage :one
SELECT * FROM messages WHERE id = ? AND client_id = ?;

-- name: GetMessageByIdempotencyKey :one
SELECT * FROM messages WHERE client_id = ? AND idempotency_key = ?;

-- name: ListMessages :many
-- Newest first. IDs are ULIDs, so id order is creation order and the last id is the cursor.
SELECT * FROM messages
WHERE client_id = sqlc.arg('client_id')
  AND (sqlc.narg('status') IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('since') IS NULL OR created_at >= sqlc.narg('since'))
  AND (sqlc.narg('before_id') IS NULL OR id < sqlc.narg('before_id'))
ORDER BY id DESC
LIMIT sqlc.arg('limit');

-- name: UpdateMessageStatus :exec
UPDATE messages SET status = ? WHERE id = ?;

-- name: GetMessageByID :one
SELECT * FROM messages WHERE id = ?;
