package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

// ClaimDue marks up to limit due queued deliveries as sending (counting the
// attempt) and returns them.
func (s *Store) ClaimDue(ctx context.Context, limit int) ([]Delivery, error) {
	rows, err := s.q.ClaimDueDeliveries(ctx, db.ClaimDueDeliveriesParams{Now: formatTime(s.clock()), Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("claiming deliveries: %w", err)
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

// MarkDelivered records a successful send.
func (s *Store) MarkDelivered(ctx context.Context, deliveryID, providerID string) error {
	err := s.q.MarkDeliveryDelivered(ctx, db.MarkDeliveryDeliveredParams{
		ProviderMessageID: nullString(providerID), UpdatedAt: formatTime(s.clock()), ID: deliveryID,
	})
	if err != nil {
		return fmt.Errorf("marking delivery delivered: %w", err)
	}
	return nil
}

// MarkRetry requeues a delivery for next. reason must hold no private data.
func (s *Store) MarkRetry(ctx context.Context, deliveryID string, next time.Time, reason string) error {
	err := s.q.MarkDeliveryRetry(ctx, db.MarkDeliveryRetryParams{
		NextAttemptAt: formatTime(next), LastError: nullString(reason), UpdatedAt: formatTime(s.clock()), ID: deliveryID,
	})
	if err != nil {
		return fmt.Errorf("marking delivery for retry: %w", err)
	}
	return nil
}

// MarkFailed gives up on a delivery. reason must hold no private data.
func (s *Store) MarkFailed(ctx context.Context, deliveryID, reason string) error {
	err := s.q.MarkDeliveryFailed(ctx, db.MarkDeliveryFailedParams{
		LastError: nullString(reason), UpdatedAt: formatTime(s.clock()), ID: deliveryID,
	})
	if err != nil {
		return fmt.Errorf("marking delivery failed: %w", err)
	}
	return nil
}

// RequeueSending puts every 'sending' delivery back in the queue. Call it
// once at startup, before the worker runs.
func (s *Store) RequeueSending(ctx context.Context) (int64, error) {
	now := formatTime(s.clock())
	n, err := s.q.RequeueSending(ctx, db.RequeueSendingParams{NextAttemptAt: now, UpdatedAt: now})
	if err != nil {
		return 0, fmt.Errorf("requeueing stuck deliveries: %w", err)
	}
	return n, nil
}

// RefreshMessageStatus recomputes a message's status from its deliveries.
func (s *Store) RefreshMessageStatus(ctx context.Context, messageID string) (string, error) {
	rows, err := s.q.DeliveryStatusCounts(ctx, messageID)
	if err != nil {
		return "", fmt.Errorf("counting deliveries: %w", err)
	}
	counts := map[string]int64{}
	for _, r := range rows {
		counts[r.Status] = r.N
	}
	status := DeriveStatus(counts)
	if err := s.q.UpdateMessageStatus(ctx, db.UpdateMessageStatusParams{Status: status, ID: messageID}); err != nil {
		return "", fmt.Errorf("updating message status: %w", err)
	}
	return status, nil
}

// DeriveStatus turns delivery status counts into a message status.
func DeriveStatus(counts map[string]int64) string {
	var total int64
	for _, n := range counts {
		total += n
	}
	delivered, failed := counts[StatusDelivered], counts[StatusFailed]
	pending := counts[StatusQueued] + counts[StatusSending]
	switch {
	case total == 0:
		return StatusQueued
	case delivered == total:
		return StatusDelivered
	case failed == total:
		return StatusFailed
	case pending == 0:
		return StatusPartiallyDelivered
	case counts[StatusSending] > 0 || delivered > 0 || failed > 0:
		return StatusSending
	default:
		return StatusQueued
	}
}

// MessageForDelivery returns a message (any client's) with its attachments,
// decrypted, for the worker to send.
func (s *Store) MessageForDelivery(ctx context.Context, messageID string) (Message, []Attachment, error) {
	row, err := s.q.GetMessageByID(ctx, messageID)
	if err != nil {
		return Message{}, nil, fmt.Errorf("getting message: %w", mapErr(err))
	}
	m, err := s.messageFromRow(row)
	if err != nil {
		return Message{}, nil, err
	}
	atts, err := s.Attachments(ctx, messageID)
	if err != nil {
		return Message{}, nil, err
	}
	return m, atts, nil
}

// QueueStats reports queued deliveries and the oldest due time (zero if none).
func (s *Store) QueueStats(ctx context.Context) (depth int64, oldest time.Time, err error) {
	row, err := s.q.QueueStats(ctx)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("reading queue stats: %w", err)
	}
	if row.Oldest != "" {
		if oldest, err = parseTime(row.Oldest); err != nil {
			return 0, time.Time{}, err
		}
	}
	return row.Depth, oldest, nil
}
