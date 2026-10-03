package core

import (
	"context"
	"fmt"

	"github.com/ali-aliabadi/relay/internal/store"
)

// Channels Relay knows about. Only telegram is implemented in the MVP.
var knownChannels = map[string]bool{"telegram": true}

// Recipients manages the people Relay notifies.
type Recipients struct {
	store *store.Store
}

// NewRecipients returns a Recipients service.
func NewRecipients(s *store.Store) *Recipients { return &Recipients{store: s} }

// Add validates and stores a new recipient. Empty timezone and channel
// preference default to UTC and ["telegram"].
func (r *Recipients) Add(ctx context.Context, rcp store.Recipient) (store.Recipient, error) {
	if rcp.Timezone == "" {
		rcp.Timezone = "UTC"
	}
	if len(rcp.ChannelPreference) == 0 {
		rcp.ChannelPreference = []string{"telegram"}
	}
	if err := validateRecipient(rcp); err != nil {
		return store.Recipient{}, err
	}
	return r.store.CreateRecipient(ctx, rcp)
}

// Get returns one recipient by username.
func (r *Recipients) Get(ctx context.Context, username string) (store.Recipient, error) {
	return r.store.RecipientByUsername(ctx, username)
}

// List returns every recipient.
func (r *Recipients) List(ctx context.Context) ([]store.Recipient, error) {
	return r.store.ListRecipients(ctx)
}

// Update validates and saves a recipient's editable fields.
func (r *Recipients) Update(ctx context.Context, rcp store.Recipient) error {
	if err := validateRecipient(rcp); err != nil {
		return err
	}
	return r.store.UpdateRecipient(ctx, rcp)
}

// Remove deletes a recipient with its contacts and deliveries.
func (r *Recipients) Remove(ctx context.Context, username string) error {
	return r.store.DeleteRecipient(ctx, username)
}

// Channels returns the channels a recipient has a contact on.
func (r *Recipients) Channels(ctx context.Context, recipientID string) ([]string, error) {
	contacts, err := r.store.ContactsForRecipient(ctx, recipientID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, c.Channel)
	}
	return out, nil
}

func validateRecipient(rcp store.Recipient) error {
	if err := validateName(rcp.Username); err != nil {
		return err
	}
	if err := validateDisplayName(rcp.DisplayName); err != nil {
		return err
	}
	if err := validateTimezone(rcp.Timezone); err != nil {
		return err
	}
	if len(rcp.ChannelPreference) == 0 {
		return fmt.Errorf("%w: channel preference must not be empty", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, ch := range rcp.ChannelPreference {
		if !knownChannels[ch] || seen[ch] {
			return fmt.Errorf("%w: channel preference has an unknown or repeated channel", ErrInvalid)
		}
		seen[ch] = true
	}
	return nil
}
