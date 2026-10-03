package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

// CreateAlias adds name as another name for a recipient. An existing alias
// is ErrConflict.
func (s *Store) CreateAlias(ctx context.Context, name, recipientID string, now time.Time) error {
	if err := s.q.CreateAlias(ctx, db.CreateAliasParams{Name: name, RecipientID: recipientID, CreatedAt: formatTime(now)}); err != nil {
		return fmt.Errorf("creating alias: %w", mapErr(err))
	}
	return nil
}

// DeleteAlias removes an alias; a missing one is ErrNotFound.
func (s *Store) DeleteAlias(ctx context.Context, name string) error {
	n, err := s.q.DeleteAlias(ctx, name)
	if err != nil {
		return fmt.Errorf("deleting alias: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("deleting alias: %w", ErrNotFound)
	}
	return nil
}

// Aliases returns a recipient's aliases, sorted.
func (s *Store) Aliases(ctx context.Context, recipientID string) ([]string, error) {
	names, err := s.q.ListAliasesForRecipient(ctx, recipientID)
	if err != nil {
		return nil, fmt.Errorf("listing aliases: %w", err)
	}
	return names, nil
}

// RecipientsByNames resolves usernames and aliases to recipients, keyed by
// the name given. Names that match nothing are left out.
func (s *Store) RecipientsByNames(ctx context.Context, names []string) (map[string]Recipient, error) {
	out := make(map[string]Recipient, len(names))
	byUsername, err := s.RecipientsByUsernames(ctx, names)
	if err != nil {
		return nil, err
	}
	for _, r := range byUsername {
		out[r.Username] = r
	}
	rows, err := s.q.GetRecipientsByAliases(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("resolving aliases: %w", err)
	}
	for _, row := range rows {
		r, err := recipientFromRow(row.Recipient)
		if err != nil {
			return nil, err
		}
		out[row.Alias] = r
	}
	return out, nil
}
