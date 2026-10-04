package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

// Delivery outcomes, used in logs and metrics.
const (
	ResultDelivered = "delivered"
	ResultRetry     = "retry"
	ResultFailed    = "failed"
)

// sendTimeout bounds one provider call. A send in flight at shutdown is
// allowed to finish rather than be cut off and sent twice after restart.
const sendTimeout = 15 * time.Second

// Worker delivers queued deliveries.
type Worker struct {
	Store     *store.Store
	Channels  []channel.Channel
	Logger    *slog.Logger
	Clock     func() time.Time
	Poll      time.Duration
	Batch     int
	OnOutcome func(channel, result string, took time.Duration) // optional, for metrics
}

// Run requeues deliveries left 'sending' by a crash, then polls until ctx is
// cancelled. Nothing is logged per poll; only delivery outcomes are.
func (w *Worker) Run(ctx context.Context) error {
	n, err := w.Store.RequeueSending(ctx)
	if ctx.Err() != nil {
		return nil // shut down during startup: nothing was claimed, so a clean stop
	}
	if err != nil {
		return err
	}
	if n > 0 {
		w.Logger.Warn("requeued deliveries left sending by a previous run", slog.Int64("count", n))
	}
	ticker := time.NewTicker(w.Poll)
	defer ticker.Stop()
	for {
		for {
			// Drain full batches without waiting for the next tick.
			claimed, err := w.Tick(ctx)
			if err != nil && ctx.Err() == nil {
				w.Logger.Error("worker pass failed", slog.String("error", err.Error()))
			}
			if err != nil || claimed < w.batch() || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (w *Worker) batch() int {
	if w.Batch <= 0 {
		return 10
	}
	return w.Batch
}

// Tick claims and processes one batch, returning how many were claimed.
func (w *Worker) Tick(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, nil // shutting down: claim nothing new
	default:
	}
	due, err := w.Store.ClaimDue(ctx, w.batch())
	if err != nil {
		return 0, err
	}
	for _, d := range due {
		// Finish what was claimed even if shutdown starts mid-batch.
		w.process(context.WithoutCancel(ctx), d)
	}
	return len(due), nil
}

func (w *Worker) process(ctx context.Context, d store.Delivery) {
	start := w.Clock()
	providerID, err := w.send(ctx, d)
	result := w.record(ctx, d, providerID, err)
	took := w.Clock().Sub(start)
	if w.OnOutcome != nil {
		w.OnOutcome(d.Channel, result, took)
	}
	attrs := []slog.Attr{
		slog.String("message_id", d.MessageID), slog.String("delivery_id", d.ID),
		slog.String("channel", d.Channel), slog.String("result", result),
		slog.Int64("attempt", d.Attempts), slog.Duration("duration", took),
	}
	level := slog.LevelInfo
	if err != nil {
		attrs = append(attrs, slog.String("error", reason(err)))
		var ce *channel.Error
		if !errors.As(err, &ce) {
			attrs = append(attrs, slog.String("detail", err.Error())) // internal errors carry IDs, not content
		}
		if result == ResultRetry {
			level = slog.LevelWarn
		}
	}
	w.Logger.LogAttrs(ctx, level, "delivery", attrs...)
	if _, err := w.Store.RefreshMessageStatus(ctx, d.MessageID); err != nil {
		w.Logger.Error("refreshing message status", slog.String("message_id", d.MessageID), slog.String("error", err.Error()))
	}
}

// send loads everything the channel needs and sends. Problems that retrying
// can't fix come back as permanent channel errors.
func (w *Worker) send(ctx context.Context, d store.Delivery) (string, error) {
	var ch channel.Channel
	for _, c := range w.Channels {
		if c.Name() == d.Channel {
			ch = c
		}
	}
	if ch == nil {
		return "", channel.Permanent("channel %s is not configured", d.Channel)
	}
	m, atts, err := w.Store.MessageForDelivery(ctx, d.MessageID)
	if err != nil {
		return "", err
	}
	if m.RedactedAt != nil {
		return "", channel.Permanent("content expired before delivery")
	}
	contact, err := w.Store.Contact(ctx, d.RecipientID, d.Channel)
	if errors.Is(err, store.ErrNotFound) {
		return "", channel.Permanent("recipient has no %s contact", d.Channel)
	}
	if err != nil {
		return "", err
	}
	msg := message.Message{Urgency: m.Urgency, Title: m.Title, Source: m.Source, DeliveryID: d.ID}
	if err := json.Unmarshal(m.Blocks, &msg.Blocks); err != nil {
		return "", channel.Permanent("stored blocks are unreadable")
	}
	for _, a := range atts {
		msg.Attachments = append(msg.Attachments, message.Attachment{ContentType: a.ContentType, Bytes: a.Bytes})
	}
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return ch.Send(sctx, channel.Contact{Address: contact.Address}, msg)
}

// record stores the outcome and returns it.
func (w *Worker) record(ctx context.Context, d store.Delivery, providerID string, sendErr error) string {
	var err error
	result := ResultDelivered
	if sendErr == nil {
		err = w.Store.MarkDelivered(ctx, d.ID, providerID)
	} else {
		var ce *channel.Error
		permanent := errors.As(sendErr, &ce) && ce.Permanent
		var retryAfter time.Duration
		if ce != nil {
			retryAfter = ce.RetryAfter
		}
		next, again := NextAttempt(w.Clock(), int(d.Attempts), retryAfter)
		if permanent || !again {
			result = ResultFailed
			err = w.Store.MarkFailed(ctx, d.ID, reason(sendErr))
		} else {
			result = ResultRetry
			err = w.Store.MarkRetry(ctx, d.ID, next, reason(sendErr))
		}
	}
	if err != nil {
		w.Logger.Error("recording delivery outcome", slog.String("delivery_id", d.ID), slog.String("error", err.Error()))
	}
	return result
}

// reason is the text stored in last_error and logged: a channel error's safe
// reason, or a generic message for internal errors.
func reason(err error) string {
	var ce *channel.Error
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return "internal error"
}
