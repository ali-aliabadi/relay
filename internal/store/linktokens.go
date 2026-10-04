package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

// LinkToken is a one-time invite link: whoever opens it is linked to the
// recipient on Channel. Only TokenHash (SHA-256 of the token) is stored.
type LinkToken struct {
	TokenHash   []byte
	RecipientID string
	Channel     string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// ReplaceLinkToken stores t and drops the recipient's earlier tokens on its
// channel, so only the newest invite link works.
func (s *Store) ReplaceLinkToken(ctx context.Context, t LinkToken) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if err := q.DeleteLinkTokensFor(ctx, db.DeleteLinkTokensForParams{RecipientID: t.RecipientID, Channel: t.Channel}); err != nil {
			return fmt.Errorf("deleting old link tokens: %w", err)
		}
		err := q.CreateLinkToken(ctx, db.CreateLinkTokenParams{
			TokenHash: t.TokenHash, RecipientID: t.RecipientID, Channel: t.Channel,
			CreatedAt: formatTime(t.CreatedAt), ExpiresAt: formatTime(t.ExpiresAt),
		})
		if err != nil {
			return fmt.Errorf("saving link token: %w", mapErr(err))
		}
		return nil
	})
}

// ClaimLinkToken deletes the unexpired token with this hash and returns
// whom it links. The delete makes it single-use; a missing, used or expired
// token is ErrNotFound.
func (s *Store) ClaimLinkToken(ctx context.Context, tokenHash []byte, now time.Time) (recipientID, channel string, err error) {
	row, err := s.q.ClaimLinkToken(ctx, db.ClaimLinkTokenParams{TokenHash: tokenHash, Now: formatTime(now)})
	if err != nil {
		return "", "", fmt.Errorf("claiming link token: %w", mapErr(err))
	}
	return row.RecipientID, row.Channel, nil
}

// DeleteLinkTokensBefore removes tokens that expired at or before now.
func (s *Store) DeleteLinkTokensBefore(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.q.DeleteLinkTokensBefore(ctx, formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("deleting expired link tokens: %w", err)
	}
	return n, nil
}
