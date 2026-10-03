-- name: UpsertInvite :exec
INSERT INTO invites (recipient_id, channel, handle, created_at) VALUES (?, ?, ?, ?)
ON CONFLICT (recipient_id, channel) DO UPDATE SET handle = excluded.handle, created_at = excluded.created_at;

-- name: ListInvites :many
SELECT * FROM invites WHERE channel = sqlc.arg('channel') AND created_at >= sqlc.arg('since') ORDER BY created_at;

-- name: DeleteInvite :exec
DELETE FROM invites WHERE recipient_id = ? AND channel = ?;

-- name: DeleteInvitesBefore :execrows
DELETE FROM invites WHERE created_at < sqlc.arg('cutoff');
