-- +goose Up
-- One answer per recipient to a message's question block. Replaced while
-- fetched_at is NULL; once the app has read it, it is final.
CREATE TABLE answers (
    message_id   TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    recipient_id TEXT NOT NULL REFERENCES recipients (id) ON DELETE CASCADE,
    answer       BLOB NOT NULL, -- encrypted
    answered_at  TEXT NOT NULL,
    fetched_at   TEXT,
    PRIMARY KEY (message_id, recipient_id)
) STRICT;

-- Typed replies point at the provider's message ID.
CREATE INDEX deliveries_provider ON deliveries (channel, provider_message_id);

-- +goose Down
DROP INDEX deliveries_provider;
DROP TABLE answers;
