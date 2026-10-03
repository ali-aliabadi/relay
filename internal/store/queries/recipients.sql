-- name: CreateRecipient :exec
INSERT INTO recipients (id, username, display_name, timezone, channel_preference, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetRecipientByUsername :one
SELECT * FROM recipients WHERE username = ?;

-- name: GetRecipientsByUsernames :many
SELECT * FROM recipients WHERE username IN (sqlc.slice('usernames')) ORDER BY username;

-- name: ListRecipients :many
SELECT * FROM recipients ORDER BY username;

-- name: UpdateRecipient :execrows
UPDATE recipients SET display_name = ?, timezone = ?, channel_preference = ? WHERE username = ?;

-- name: DeleteRecipient :execrows
DELETE FROM recipients WHERE username = ?;

-- name: GetRecipientByID :one
SELECT * FROM recipients WHERE id = ?;
