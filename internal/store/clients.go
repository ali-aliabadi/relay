package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/id"
	"github.com/ali-aliabadi/relay/internal/store/db"
)

// Client is an app allowed to call the API.
type Client struct {
	ID         string
	Name       string
	APIKeyHash []byte
	CreatedAt  time.Time
	RevokedAt  *time.Time
}

// CreateClient stores a client with the SHA-256 hash of its API key.
func (s *Store) CreateClient(ctx context.Context, name string, keyHash []byte) (Client, error) {
	now := s.clock().UTC().Truncate(time.Millisecond)
	c := Client{ID: id.New(id.Client, now), Name: name, APIKeyHash: keyHash, CreatedAt: now}
	err := s.q.CreateClient(ctx, db.CreateClientParams{
		ID: c.ID, Name: name, ApiKeyHash: keyHash, CreatedAt: formatTime(now),
	})
	if err != nil {
		return Client{}, fmt.Errorf("creating client: %w", mapErr(err))
	}
	return c, nil
}

// ClientByKeyHash returns the active (not revoked) client with this key hash.
func (s *Store) ClientByKeyHash(ctx context.Context, keyHash []byte) (Client, error) {
	row, err := s.q.GetClientByKeyHash(ctx, keyHash)
	if err != nil {
		return Client{}, fmt.Errorf("getting client: %w", mapErr(err))
	}
	return clientFromRow(row)
}

// ListClients returns every client, revoked ones included.
func (s *Store) ListClients(ctx context.Context) ([]Client, error) {
	rows, err := s.q.ListClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing clients: %w", err)
	}
	out := make([]Client, 0, len(rows))
	for _, r := range rows {
		c, err := clientFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// RevokeClient revokes the active client called name.
func (s *Store) RevokeClient(ctx context.Context, name string) error {
	n, err := s.q.RevokeClient(ctx, db.RevokeClientParams{
		RevokedAt: nullString(formatTime(s.clock())), Name: name,
	})
	if err != nil {
		return fmt.Errorf("revoking client: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("revoking client: %w", ErrNotFound)
	}
	return nil
}

func clientFromRow(r db.Client) (Client, error) {
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return Client{}, err
	}
	revoked, err := parseNullTime(r.RevokedAt)
	if err != nil {
		return Client{}, err
	}
	return Client{ID: r.ID, Name: r.Name, APIKeyHash: r.ApiKeyHash, CreatedAt: created, RevokedAt: revoked}, nil
}

// ReissueClient gives a revoked client a new key hash and makes it active
// again, keeping its ID (and so its messages). ErrNotFound when no revoked
// client has this name.
func (s *Store) ReissueClient(ctx context.Context, name string, keyHash []byte) (Client, error) {
	row, err := s.q.ReissueClient(ctx, db.ReissueClientParams{ApiKeyHash: keyHash, Name: name})
	if err != nil {
		return Client{}, fmt.Errorf("reissuing client: %w", mapErr(err))
	}
	return clientFromRow(row)
}
