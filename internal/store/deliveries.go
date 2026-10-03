package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

// Delivery is one attempt plan for a (message, recipient, channel).
type Delivery struct {
	ID                string
	MessageID         string
	RecipientID       string
	Channel           string
	Status            string
	Attempts          int64
	NextAttemptAt     time.Time
	ProviderMessageID string
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Deliveries returns a message's deliveries in creation order.
func (s *Store) Deliveries(ctx context.Context, messageID string) ([]Delivery, error) {
	rows, err := s.q.ListDeliveriesForMessage(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("listing deliveries: %w", err)
	}
	out := make([]Delivery, 0, len(rows))
	for _, r := range rows {
		d, err := deliveryFromRow(r)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func deliveryFromRow(r db.Delivery) (Delivery, error) {
	var times [3]time.Time
	for i, s := range []string{r.NextAttemptAt, r.CreatedAt, r.UpdatedAt} {
		t, err := parseTime(s)
		if err != nil {
			return Delivery{}, err
		}
		times[i] = t
	}
	return Delivery{
		ID: r.ID, MessageID: r.MessageID, RecipientID: r.RecipientID, Channel: r.Channel, Status: r.Status,
		Attempts: r.Attempts, NextAttemptAt: times[0], ProviderMessageID: r.ProviderMessageID.String,
		LastError: r.LastError.String, CreatedAt: times[1], UpdatedAt: times[2],
	}, nil
}
