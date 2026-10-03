-- name: UpsertContact :exec
INSERT INTO contacts (recipient_id, channel, address, verified_at) VALUES (?, ?, ?, ?)
ON CONFLICT (recipient_id, channel) DO UPDATE SET address = excluded.address, verified_at = excluded.verified_at;

-- name: GetContact :one
SELECT * FROM contacts WHERE recipient_id = ? AND channel = ?;

-- name: ListContactsForRecipient :many
SELECT * FROM contacts WHERE recipient_id = ? ORDER BY channel;
