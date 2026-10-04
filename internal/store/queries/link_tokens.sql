-- name: CreateLinkToken :exec
INSERT INTO link_tokens (token_hash, recipient_id, channel, created_at, expires_at) VALUES (?, ?, ?, ?, ?);

-- name: DeleteLinkTokensFor :exec
DELETE FROM link_tokens WHERE recipient_id = ? AND channel = ?;

-- name: ClaimLinkToken :one
DELETE FROM link_tokens WHERE token_hash = sqlc.arg('token_hash') AND expires_at > sqlc.arg('now')
RETURNING recipient_id, channel;

-- name: DeleteLinkTokensBefore :execrows
DELETE FROM link_tokens WHERE expires_at <= sqlc.arg('now');
