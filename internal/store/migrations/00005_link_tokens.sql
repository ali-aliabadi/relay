-- +goose Up
-- One-time invite links: the bot's t.me/<bot>?start=<token> link links whoever
-- opens it to the recipient. Only the token's SHA-256 is stored.
CREATE TABLE link_tokens (
    token_hash   BLOB PRIMARY KEY,
    recipient_id TEXT NOT NULL REFERENCES recipients (id) ON DELETE CASCADE,
    channel      TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL
) STRICT;

CREATE INDEX link_tokens_recipient ON link_tokens (recipient_id, channel);

-- +goose Down
DROP TABLE link_tokens;
