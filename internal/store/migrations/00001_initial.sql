-- +goose Up
-- Times are UTC text in a fixed-width format (2006-01-02T15:04:05.000Z) so they sort correctly.
-- Columns marked "encrypted" hold internal/crypto ciphertext, never plaintext.

CREATE TABLE clients (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    api_key_hash BLOB NOT NULL UNIQUE, -- sha256 of the key
    created_at   TEXT NOT NULL,
    revoked_at   TEXT
) STRICT;

CREATE TABLE recipients (
    id                 TEXT PRIMARY KEY,
    username           TEXT NOT NULL UNIQUE,
    display_name       TEXT NOT NULL,
    timezone           TEXT NOT NULL DEFAULT 'UTC',
    channel_preference TEXT NOT NULL DEFAULT '["telegram"]', -- JSON array of channel names
    created_at         TEXT NOT NULL
) STRICT;

CREATE TABLE contacts (
    recipient_id TEXT NOT NULL REFERENCES recipients (id) ON DELETE CASCADE,
    channel      TEXT NOT NULL,
    address      BLOB NOT NULL, -- encrypted
    verified_at  TEXT,
    PRIMARY KEY (recipient_id, channel)
) STRICT;

CREATE TABLE messages (
    id              TEXT PRIMARY KEY,
    client_id       TEXT NOT NULL REFERENCES clients (id),
    urgency         TEXT NOT NULL CHECK (urgency IN ('low', 'normal', 'high', 'critical')),
    title           BLOB, -- encrypted; NULL when absent or purged
    blocks          BLOB, -- encrypted JSON; NULL once purged by retention
    source          TEXT,
    idempotency_key TEXT,
    request_id      TEXT,
    status          TEXT NOT NULL CHECK (status IN ('queued', 'sending', 'delivered', 'partially_delivered', 'failed')),
    created_at      TEXT NOT NULL,
    redacted_at     TEXT,
    UNIQUE (client_id, idempotency_key)
) STRICT;

CREATE INDEX messages_client_created ON messages (client_id, created_at, id);
CREATE INDEX messages_created ON messages (created_at);

CREATE TABLE attachments (
    id           TEXT PRIMARY KEY,
    message_id   TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,
    bytes        BLOB NOT NULL, -- encrypted
    size         INTEGER NOT NULL
) STRICT;

CREATE INDEX attachments_message ON attachments (message_id);

CREATE TABLE deliveries (
    id                  TEXT PRIMARY KEY,
    message_id          TEXT NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    recipient_id        TEXT NOT NULL REFERENCES recipients (id) ON DELETE CASCADE,
    channel             TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('queued', 'sending', 'delivered', 'failed')),
    attempts            INTEGER NOT NULL DEFAULT 0,
    next_attempt_at     TEXT NOT NULL,
    provider_message_id TEXT,
    last_error          TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
) STRICT;

CREATE INDEX deliveries_due ON deliveries (status, next_attempt_at);
CREATE INDEX deliveries_message ON deliveries (message_id);
CREATE INDEX deliveries_recipient ON deliveries (recipient_id);

-- +goose Down
DROP TABLE deliveries;
DROP TABLE attachments;
DROP TABLE messages;
DROP TABLE contacts;
DROP TABLE recipients;
DROP TABLE clients;
