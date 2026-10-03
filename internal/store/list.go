package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

// Message and delivery statuses.
const (
	StatusQueued             = "queued"
	StatusSending            = "sending"
	StatusDelivered          = "delivered"
	StatusPartiallyDelivered = "partially_delivered"
	StatusFailed             = "failed"
)

// MaxListLimit caps one page of ListMessages.
const MaxListLimit = 100

// ListFilter selects a page of one client's messages, newest first.
type ListFilter struct {
	ClientID string
	Status   string    // optional
	Since    time.Time // optional; zero means no lower bound
	BeforeID string    // optional cursor: the last ID of the previous page
	Limit    int
}

// ListMessages returns one page of a client's messages.
func (s *Store) ListMessages(ctx context.Context, f ListFilter) ([]Message, error) {
	if f.Limit <= 0 || f.Limit > MaxListLimit {
		f.Limit = MaxListLimit
	}
	p := db.ListMessagesParams{ClientID: f.ClientID, Limit: int64(f.Limit)}
	if f.Status != "" {
		p.Status = f.Status
	}
	if !f.Since.IsZero() {
		p.Since = formatTime(f.Since)
	}
	if f.BeforeID != "" {
		p.BeforeID = f.BeforeID
	}
	rows, err := s.q.ListMessages(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("listing messages: %w", err)
	}
	out := make([]Message, 0, len(rows))
	for _, r := range rows {
		m, err := s.messageFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
