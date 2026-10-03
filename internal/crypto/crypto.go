// Package crypto encrypts private columns (message content, images, contact
// addresses) with AES-256-GCM before they reach SQLite.
//
// Ciphertext layout: version (1 byte) || nonce (12 bytes) || sealed data.
// The version byte names the key, so keys can be rotated later without
// rewriting old rows first. Callers pass associated data (for example
// "contacts.address:rcp_01J...") so a value can't be moved to another row or
// column without failing authentication.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// CurrentVersion is the key version written by Encrypt.
const CurrentVersion byte = 1

const nonceSize = 12

// ErrDecrypt is returned for any ciphertext that can't be opened: wrong key,
// unknown version, truncated or tampered data. It deliberately carries no detail.
var ErrDecrypt = errors.New("decrypting value: authentication failed")

// Cipher encrypts and decrypts values. It is safe for concurrent use.
type Cipher struct {
	aeads   map[byte]cipher.AEAD
	current byte
}

// New returns a Cipher that encrypts with key as version 1.
func New(key []byte) (*Cipher, error) {
	return NewWithKeys(CurrentVersion, map[byte][]byte{CurrentVersion: key})
}

// NewWithKeys returns a Cipher that can decrypt every version in keys and
// encrypts with current. It exists for key rotation.
func NewWithKeys(current byte, keys map[byte][]byte) (*Cipher, error) {
	c := &Cipher{aeads: make(map[byte]cipher.AEAD, len(keys)), current: current}
	for v, key := range keys {
		if len(key) != KeySize {
			return nil, fmt.Errorf("key version %d: want %d bytes, got %d", v, KeySize, len(key))
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("key version %d: %w", v, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("key version %d: %w", v, err)
		}
		c.aeads[v] = aead
	}
	if _, ok := c.aeads[current]; !ok {
		return nil, fmt.Errorf("no key for current version %d", current)
	}
	return c, nil
}

// Encrypt seals plaintext bound to aad. A nil plaintext still produces a
// ciphertext; callers store SQL NULL themselves when a value is absent.
func (c *Cipher) Encrypt(plaintext, aad []byte) ([]byte, error) {
	out := make([]byte, 1+nonceSize, 1+nonceSize+len(plaintext)+c.aeads[c.current].Overhead())
	out[0] = c.current
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return c.aeads[c.current].Seal(out, out[1:], plaintext, aad), nil
}

// Decrypt opens a value produced by Encrypt with the same aad.
func (c *Cipher) Decrypt(ciphertext, aad []byte) ([]byte, error) {
	if len(ciphertext) < 1+nonceSize {
		return nil, ErrDecrypt
	}
	aead, ok := c.aeads[ciphertext[0]]
	if !ok {
		return nil, ErrDecrypt
	}
	plain, err := aead.Open(nil, ciphertext[1:1+nonceSize], ciphertext[1+nonceSize:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
