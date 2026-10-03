package store

import (
	"context"
	"fmt"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

const colContactAddress = "contacts.address"

// contactRowID scopes the address ciphertext to one recipient and channel.
func contactRowID(recipientID, channel string) string { return recipientID + "/" + channel }

// UpsertContact creates or replaces a recipient's address on a channel.
func (s *Store) UpsertContact(ctx context.Context, c Contact) error {
	addr, err := s.seal([]byte(c.Address), colContactAddress, contactRowID(c.RecipientID, c.Channel))
	if err != nil {
		return err
	}
	err = s.q.UpsertContact(ctx, db.UpsertContactParams{
		RecipientID: c.RecipientID, Channel: c.Channel, Address: addr, VerifiedAt: formatNullTime(c.VerifiedAt),
	})
	if err != nil {
		return fmt.Errorf("saving contact: %w", mapErr(err))
	}
	return nil
}

// Contact returns a recipient's contact on one channel, decrypted.
func (s *Store) Contact(ctx context.Context, recipientID, channel string) (Contact, error) {
	row, err := s.q.GetContact(ctx, db.GetContactParams{RecipientID: recipientID, Channel: channel})
	if err != nil {
		return Contact{}, fmt.Errorf("getting contact: %w", mapErr(err))
	}
	return s.contactFromRow(row)
}

// ContactsForRecipient returns every contact of a recipient, decrypted.
func (s *Store) ContactsForRecipient(ctx context.Context, recipientID string) ([]Contact, error) {
	rows, err := s.q.ListContactsForRecipient(ctx, recipientID)
	if err != nil {
		return nil, fmt.Errorf("listing contacts: %w", err)
	}
	out := make([]Contact, 0, len(rows))
	for _, row := range rows {
		c, err := s.contactFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) contactFromRow(row db.Contact) (Contact, error) {
	addr, err := s.open(row.Address, colContactAddress, contactRowID(row.RecipientID, row.Channel))
	if err != nil {
		return Contact{}, err
	}
	verified, err := parseNullTime(row.VerifiedAt)
	if err != nil {
		return Contact{}, err
	}
	return Contact{RecipientID: row.RecipientID, Channel: row.Channel, Address: string(addr), VerifiedAt: verified}, nil
}
