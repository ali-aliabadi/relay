-- name: CreateDelivery :exec
INSERT INTO deliveries (id, message_id, recipient_id, channel, status, attempts, next_attempt_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?);

-- name: ListDeliveriesForMessage :many
SELECT * FROM deliveries WHERE message_id = ? ORDER BY id;
