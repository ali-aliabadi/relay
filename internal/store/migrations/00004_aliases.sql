-- +goose Up
-- Extra names for a recipient (e.g. "admin" for ali). Aliases and usernames
-- share one namespace; core checks both sides, since SQLite can't.
CREATE TABLE aliases (
    name         TEXT PRIMARY KEY,
    recipient_id TEXT NOT NULL REFERENCES recipients (id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL
) STRICT;

CREATE INDEX aliases_recipient ON aliases (recipient_id);

-- +goose Down
DROP TABLE aliases;
