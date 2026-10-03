-- name: SaveAnswer :execrows
-- Inserts or replaces an answer the app hasn't fetched yet; 0 rows means it is final.
INSERT INTO answers (message_id, recipient_id, answer, answered_at) VALUES (?, ?, ?, ?)
ON CONFLICT (message_id, recipient_id) DO UPDATE SET answer = excluded.answer, answered_at = excluded.answered_at
WHERE answers.fetched_at IS NULL;

-- name: ListAnswersForMessage :many
SELECT a.message_id, a.recipient_id, r.username, a.answer, a.answered_at
FROM answers a JOIN recipients r ON r.id = a.recipient_id
WHERE a.message_id = ? ORDER BY a.answered_at, r.username;

-- name: MarkAnswersFetched :exec
UPDATE answers SET fetched_at = ? WHERE message_id = ? AND fetched_at IS NULL;

-- name: DeleteAnswersBefore :execrows
DELETE FROM answers WHERE message_id IN (SELECT m.id FROM messages m WHERE m.created_at < sqlc.arg('cutoff'));
