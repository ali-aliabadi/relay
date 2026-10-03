package store

import (
	"database/sql"
	"fmt"
	"time"
)

// timeLayout is fixed-width so stored times sort as text.
const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing stored time: %w", err)
	}
	return t, nil
}

func formatNullTime(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*t), Valid: true}
}

func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil //nolint:nilnil // a NULL time is a nil pointer, not an error
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

// aad binds a ciphertext to its column and row so it can't be swapped elsewhere.
func aad(column, rowID string) []byte { return []byte(column + ":" + rowID) }

// seal encrypts a private value. nil stays nil (SQL NULL).
func (s *Store) seal(plain []byte, column, rowID string) ([]byte, error) {
	if plain == nil {
		return nil, nil
	}
	ct, err := s.cipher.Encrypt(plain, aad(column, rowID))
	if err != nil {
		return nil, fmt.Errorf("encrypting %s: %w", column, err)
	}
	return ct, nil
}

// open decrypts a private value. NULL stays nil.
func (s *Store) open(ct []byte, column, rowID string) ([]byte, error) {
	if ct == nil {
		return nil, nil
	}
	plain, err := s.cipher.Decrypt(ct, aad(column, rowID))
	if err != nil {
		return nil, fmt.Errorf("%s of %s: %w", column, rowID, err)
	}
	if plain == nil {
		plain = []byte{}
	}
	return plain, nil
}
