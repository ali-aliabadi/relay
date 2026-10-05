// Package core holds Relay's business logic between the API/CLI and the store.
package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/ali-aliabadi/relay/internal/store"
)

// apiKeyPrefix marks Relay keys so they are recognisable in config files and
// by secret scanners.
const apiKeyPrefix = "rk_"

// ErrUnauthorized means the API key is missing, malformed, unknown or revoked.
var ErrUnauthorized = errors.New("unauthorized")

// Clients manages API clients and authenticates their keys.
type Clients struct {
	store *store.Store
}

// NewClients returns a Clients service.
func NewClients(s *store.Store) *Clients { return &Clients{store: s} }

// Create registers a client and returns its API key. The key is not stored
// (only its SHA-256 hash), so this is the only time it can be shown. A
// revoked client of the same name gets the new key and is active again; an
// active one is store.ErrConflict.
func (c *Clients) Create(ctx context.Context, name string) (store.Client, string, error) {
	if err := validateName(name); err != nil {
		return store.Client{}, "", err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return store.Client{}, "", fmt.Errorf("generating api key: %w", err)
	}
	key := apiKeyPrefix + base64.RawURLEncoding.EncodeToString(raw[:])
	client, err := c.store.CreateClient(ctx, name, hashKey(key))
	if errors.Is(err, store.ErrConflict) {
		var rerr error
		if client, rerr = c.store.ReissueClient(ctx, name, hashKey(key)); !errors.Is(rerr, store.ErrNotFound) {
			err = rerr // reissued (nil) or failed; ErrNotFound means the name is active
		}
	}
	if err != nil {
		return store.Client{}, "", err
	}
	return client, key, nil
}

// List returns every client, revoked ones included.
func (c *Clients) List(ctx context.Context) ([]store.Client, error) {
	return c.store.ListClients(ctx)
}

// Revoke disables a client's key immediately.
func (c *Clients) Revoke(ctx context.Context, name string) error {
	return c.store.RevokeClient(ctx, name)
}

// Authenticate returns the active client owning key, or ErrUnauthorized.
func (c *Clients) Authenticate(ctx context.Context, key string) (store.Client, error) {
	if !strings.HasPrefix(key, apiKeyPrefix) || len(key) > 128 {
		return store.Client{}, ErrUnauthorized
	}
	h := hashKey(key)
	client, err := c.store.ClientByKeyHash(ctx, h)
	if errors.Is(err, store.ErrNotFound) {
		return store.Client{}, ErrUnauthorized
	}
	if err != nil {
		return store.Client{}, fmt.Errorf("authenticating: %w", err)
	}
	// The lookup is by hash already; compare again in constant time so the
	// decision never depends on a variable-time comparison.
	if subtle.ConstantTimeCompare(client.APIKeyHash, h) != 1 {
		return store.Client{}, ErrUnauthorized
	}
	return client, nil
}

func hashKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}
