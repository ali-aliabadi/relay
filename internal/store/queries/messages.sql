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

-- name: RedactMessagesBefore :execrows
-- Retention: drop content, keep metadata.
UPDATE messages SET title = NULL, blocks = NULL, redacted_at = sqlc.arg('now')
WHERE created_at < sqlc.arg('cutoff') AND redacted_at IS NULL;

-- name: DeleteAttachmentsBefore :execrows
DELETE FROM attachments WHERE message_id IN (SELECT m.id FROM messages m WHERE m.created_at < sqlc.arg('cutoff'));

-- name: DeleteMessagesBefore :execrows
-- Deliveries and attachments go with them (ON DELETE CASCADE).
DELETE FROM messages WHERE created_at < sqlc.arg('cutoff');
