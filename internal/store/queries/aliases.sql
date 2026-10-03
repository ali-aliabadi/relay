-- name: CreateAlias :exec
INSERT INTO aliases (name, recipient_id, created_at) VALUES (?, ?, ?);

-- name: DeleteAlias :execrows
DELETE FROM aliases WHERE name = ?;

-- name: ListAliasesForRecipient :many
SELECT name FROM aliases WHERE recipient_id = ? ORDER BY name;

-- name: GetRecipientsByAliases :many
SELECT a.name AS alias, sqlc.embed(r)
FROM aliases a JOIN recipients r ON r.id = a.recipient_id
WHERE a.name IN (sqlc.slice('names'));
