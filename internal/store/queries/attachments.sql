-- name: CreateAttachment :exec
INSERT INTO attachments (id, message_id, content_type, bytes, size) VALUES (?, ?, ?, ?, ?);

-- name: ListAttachmentsForMessage :many
SELECT * FROM attachments WHERE message_id = ? ORDER BY id;
