package store

import (
	"encoding/base64"
	"fmt"
)

// SealToken encrypts plain into a URL-safe string bound to purpose, for
// values handed to people rather than stored (link codes).
func (s *Store) SealToken(purpose string, plain []byte) (string, error) {
	ct, err := s.seal(plain, "token", purpose)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ct), nil
}

// OpenToken reverses SealToken. Any tampering or wrong purpose fails.
func (s *Store) OpenToken(purpose, token string) ([]byte, error) {
	ct, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("decoding token: %w", err)
	}
	return s.open(ct, "token", purpose)
}
