package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/id"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store/db"
)

// Recipient is a person Relay can notify.
type Recipient struct {
	ID                string
	Username          string
	DisplayName       string
	Timezone          string
	ChannelPreference []string
	CreatedAt         time.Time
}

// Contact is how to reach a recipient on one channel. Address is private.
type Contact struct {
	RecipientID string
	Channel     string
	Address     string
	VerifiedAt  *time.Time
}

// LogValue keeps the address out of logs.
func (c Contact) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("recipient_id", c.RecipientID),
		slog.String("channel", c.Channel),
		obs.RedactAttr("address", c.Address),
	)
}

// CreateRecipient stores r, assigning its ID and creation time.
func (s *Store) CreateRecipient(ctx context.Context, r Recipient) (Recipient, error) {
	now := s.clock().UTC().Truncate(time.Millisecond)
	r.ID, r.CreatedAt = id.New(id.Recipient, now), now
	if r.Timezone == "" {
		r.Timezone = "UTC"
	}
	if r.ChannelPreference == nil {
		r.ChannelPreference = []string{"telegram"}
	}
	pref, err := json.Marshal(r.ChannelPreference)
	if err != nil {
		return Recipient{}, fmt.Errorf("encoding channel preference: %w", err)
	}
	err = s.q.CreateRecipient(ctx, db.CreateRecipientParams{
		ID: r.ID, Username: r.Username, DisplayName: r.DisplayName, Timezone: r.Timezone,
		ChannelPreference: string(pref), CreatedAt: formatTime(now),
	})
	if err != nil {
		return Recipient{}, fmt.Errorf("creating recipient: %w", mapErr(err))
	}
	return r, nil
}

// RecipientByUsername returns one recipient.
func (s *Store) RecipientByUsername(ctx context.Context, username string) (Recipient, error) {
	row, err := s.q.GetRecipientByUsername(ctx, username)
	if err != nil {
		return Recipient{}, fmt.Errorf("getting recipient: %w", mapErr(err))
	}
	return recipientFromRow(row)
}

// RecipientsByUsernames returns the recipients that exist among usernames.
func (s *Store) RecipientsByUsernames(ctx context.Context, usernames []string) ([]Recipient, error) {
	rows, err := s.q.GetRecipientsByUsernames(ctx, usernames)
	if err != nil {
		return nil, fmt.Errorf("getting recipients: %w", err)
	}
	return recipientsFromRows(rows)
}

// ListRecipients returns every recipient by username.
func (s *Store) ListRecipients(ctx context.Context) ([]Recipient, error) {
	rows, err := s.q.ListRecipients(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing recipients: %w", err)
	}
	return recipientsFromRows(rows)
}

// UpdateRecipient saves display name, timezone and channel preference.
func (s *Store) UpdateRecipient(ctx context.Context, r Recipient) error {
	pref, err := json.Marshal(r.ChannelPreference)
	if err != nil {
		return fmt.Errorf("encoding channel preference: %w", err)
	}
	n, err := s.q.UpdateRecipient(ctx, db.UpdateRecipientParams{
		DisplayName: r.DisplayName, Timezone: r.Timezone, ChannelPreference: string(pref), Username: r.Username,
	})
	if err != nil {
		return fmt.Errorf("updating recipient: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("updating recipient: %w", ErrNotFound)
	}
	return nil
}

// DeleteRecipient removes a recipient with its contacts and deliveries.
func (s *Store) DeleteRecipient(ctx context.Context, username string) error {
	n, err := s.q.DeleteRecipient(ctx, username)
	if err != nil {
		return fmt.Errorf("deleting recipient: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("deleting recipient: %w", ErrNotFound)
	}
	return nil
}

func recipientsFromRows(rows []db.Recipient) ([]Recipient, error) {
	out := make([]Recipient, 0, len(rows))
	for _, row := range rows {
		r, err := recipientFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func recipientFromRow(row db.Recipient) (Recipient, error) {
	created, err := parseTime(row.CreatedAt)
	if err != nil {
		return Recipient{}, err
	}
	var pref []string
	if err := json.Unmarshal([]byte(row.ChannelPreference), &pref); err != nil {
		return Recipient{}, fmt.Errorf("decoding channel preference of %s: %w", row.ID, err)
	}
	return Recipient{
		ID: row.ID, Username: row.Username, DisplayName: row.DisplayName, Timezone: row.Timezone,
		ChannelPreference: pref, CreatedAt: created,
	}, nil
}
