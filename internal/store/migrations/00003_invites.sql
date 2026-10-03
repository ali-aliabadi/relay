-- +goose Up
-- A recipient waiting to link themselves: when the Telegram account with this
-- @username sends /start to the bot, its chat becomes the recipient's contact.
CREATE TABLE invites (
    recipient_id TEXT NOT NULL REFERENCES recipients (id) ON DELETE CASCADE,
    channel      TEXT NOT NULL,
    handle       BLOB NOT NULL, -- encrypted; lowercase username without "@"
    created_at   TEXT NOT NULL,
    PRIMARY KEY (recipient_id, channel)
) STRICT;

-- +goose Down
DROP TABLE invites;
