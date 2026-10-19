-- +goose Up
-- A question can ask for a voice note. Its audio is encrypted like the answer
-- text (which is empty for a voice answer) and goes with the row at retention.
ALTER TABLE answers ADD COLUMN audio BLOB;      -- encrypted; NULL for text answers
ALTER TABLE answers ADD COLUMN audio_type TEXT; -- e.g. audio/ogg; NULL for text answers

-- +goose Down
ALTER TABLE answers DROP COLUMN audio_type;
ALTER TABLE answers DROP COLUMN audio;
