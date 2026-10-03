-- name: CreateDelivery :exec
INSERT INTO deliveries (id, message_id, recipient_id, channel, status, attempts, next_attempt_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?);

-- name: ListDeliveriesForMessage :many
SELECT * FROM deliveries WHERE message_id = ? ORDER BY id;

-- name: ClaimDueDeliveries :many
-- One statement, so a delivery is claimed by exactly one worker pass.
UPDATE deliveries
SET status = 'sending', attempts = attempts + 1, updated_at = sqlc.arg('now')
WHERE id IN (
    SELECT d.id FROM deliveries d
    WHERE d.status = 'queued' AND d.next_attempt_at <= sqlc.arg('now')
    ORDER BY d.next_attempt_at, d.id
    LIMIT sqlc.arg('limit')
)
RETURNING *;

-- name: MarkDeliveryDelivered :exec
UPDATE deliveries SET status = 'delivered', provider_message_id = ?, last_error = NULL, updated_at = ? WHERE id = ?;

-- name: MarkDeliveryRetry :exec
UPDATE deliveries SET status = 'queued', next_attempt_at = ?, last_error = ?, updated_at = ? WHERE id = ?;

-- name: MarkDeliveryFailed :exec
UPDATE deliveries SET status = 'failed', last_error = ?, updated_at = ? WHERE id = ?;

-- name: RequeueSending :execrows
-- On startup nothing can be in flight, so every 'sending' row is stuck from a crash.
UPDATE deliveries SET status = 'queued', next_attempt_at = ?, updated_at = ? WHERE status = 'sending';

-- name: DeliveryStatusCounts :many
SELECT status, count(*) AS n FROM deliveries WHERE message_id = ? GROUP BY status;

-- name: QueueStats :one
SELECT count(*) AS depth, CAST(coalesce(min(next_attempt_at), '') AS TEXT) AS oldest
FROM deliveries WHERE status = 'queued';

-- name: GetDelivery :one
SELECT * FROM deliveries WHERE id = ?;

-- name: ListDeliveriesByProviderID :many
SELECT * FROM deliveries WHERE channel = ? AND provider_message_id = ?;
