-- name: CreateClient :exec
INSERT INTO clients (id, name, api_key_hash, created_at) VALUES (?, ?, ?, ?);

-- name: GetClientByKeyHash :one
SELECT * FROM clients WHERE api_key_hash = ? AND revoked_at IS NULL;

-- name: ListClients :many
SELECT * FROM clients ORDER BY created_at, id;

-- name: RevokeClient :execrows
UPDATE clients SET revoked_at = ? WHERE name = ? AND revoked_at IS NULL;
