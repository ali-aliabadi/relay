package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

const colInviteHandle = "invites.handle"

// Invite lets whoever owns Handle on Channel link themselves to a recipient.
// Handle (a Telegram username) is a contact detail: private, never logged.
type Invite struct {
	RecipientID string
	Channel     string
	Handle      string
	CreatedAt   time.Time
}

// LogValue keeps the handle out of logs.
func (i Invite) LogValue() slog.Value {
	return slog.GroupValue(slog.String("recipient_id", i.RecipientID), slog.String("channel", i.Channel))
}

// UpsertInvite creates or replaces a recipient's invite on a channel.
func (s *Store) UpsertInvite(ctx context.Context, i Invite) error {
	h, err := s.seal([]byte(i.Handle), colInviteHandle, contactRowID(i.RecipientID, i.Channel))
	if err != nil {
		return err
	}
	err = s.q.UpsertInvite(ctx, db.UpsertInviteParams{
		RecipientID: i.RecipientID, Channel: i.Channel, Handle: h, CreatedAt: formatTime(i.CreatedAt),
	})
	if err != nil {
		return fmt.Errorf("saving invite: %w", mapErr(err))
	}
	return nil
}

// Invites returns a channel's invites created at or after since, decrypted.
func (s *Store) Invites(ctx context.Context, channel string, since time.Time) ([]Invite, error) {
	rows, err := s.q.ListInvites(ctx, db.ListInvitesParams{Channel: channel, Since: formatTime(since)})
	if err != nil {
		return nil, fmt.Errorf("listing invites: %w", err)
	}
	out := make([]Invite, 0, len(rows))
	for _, r := range rows {
		h, err := s.open(r.Handle, colInviteHandle, contactRowID(r.RecipientID, r.Channel))
		if err != nil {
			return nil, err
		}
		created, err := parseTime(r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, Invite{RecipientID: r.RecipientID, Channel: r.Channel, Handle: string(h), CreatedAt: created})
	}
	return out, nil
}

// DeleteInvite removes a recipient's invite on a channel, if any.
func (s *Store) DeleteInvite(ctx context.Context, recipientID, channel string) error {
	if err := s.q.DeleteInvite(ctx, db.DeleteInviteParams{RecipientID: recipientID, Channel: channel}); err != nil {
		return fmt.Errorf("deleting invite: %w", err)
	}
	return nil
}

// DeleteInvitesBefore removes invites created before cutoff (expired).
func (s *Store) DeleteInvitesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := s.q.DeleteInvitesBefore(ctx, formatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("deleting expired invites: %w", err)
	}
	return n, nil
}
