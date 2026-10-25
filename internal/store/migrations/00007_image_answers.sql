-- +goose Up
-- A question can ask for a picture. Like a voice note's audio, its bytes are
-- encrypted and go with the row at retention.
ALTER TABLE answers ADD COLUMN image BLOB;      -- encrypted; NULL unless the answer is a picture
ALTER TABLE answers ADD COLUMN image_type TEXT; -- image/png, image/jpeg or image/webp; NULL otherwise

-- +goose Down
ALTER TABLE answers DROP COLUMN image_type;
ALTER TABLE answers DROP COLUMN image;
