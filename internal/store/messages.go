package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/id"
	"github.com/ali-aliabadi/relay/internal/store/db"
)

const (
	colMessageTitle    = "messages.title"
	colMessageBlocks   = "messages.blocks"
	colAttachmentBytes = "attachments.bytes"
)

// Message is a stored notification. Title and Blocks are private and are
// empty once the retention job has purged them (RedactedAt is then set).
type Message struct {
	ID             string
	ClientID       string
	Urgency        string
	Title          string
	Blocks         []byte // JSON array of content blocks
	Source         string
	IdempotencyKey string
	RequestID      string
	Status         string
	CreatedAt      time.Time
	RedactedAt     *time.Time
}

// LogValue keeps title and blocks out of logs.
func (m Message) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("message_id", m.ID),
		slog.String("client_id", m.ClientID),
		slog.String("urgency", m.Urgency),
		slog.String("status", m.Status),
	)
}

// Attachment is an inline image. Bytes are private.
type Attachment struct {
	ID          string
	MessageID   string
	ContentType string
	Bytes       []byte
	Size        int64
}

// LogValue keeps the image bytes out of logs.
func (a Attachment) LogValue() slog.Value {
	return slog.GroupValue(slog.String("attachment_id", a.ID), slog.Int64("size", a.Size))
}

// NewMessage is everything CreateMessage writes in one transaction.
type NewMessage struct {
	Message     Message // ID, Status and CreatedAt are set by the store
	Attachments []Attachment
	Deliveries  []PlannedDelivery
}

// PlannedDelivery is one (recipient, channel) the router chose.
type PlannedDelivery struct {
	RecipientID string
	Channel     string
}

// CreateMessage stores a queued message, its attachments and one queued
// delivery per planned (recipient, channel), atomically.
func (s *Store) CreateMessage(ctx context.Context, nm NewMessage) (Message, error) {
	now := s.clock().UTC().Truncate(time.Millisecond)
	m := nm.Message
	m.ID, m.Status, m.CreatedAt = id.New(id.Message, now), StatusQueued, now

	var title []byte
	if m.Title != "" {
		title = []byte(m.Title)
	}
	sealedTitle, err := s.seal(title, colMessageTitle, m.ID)
	if err != nil {
		return Message{}, err
	}
	sealedBlocks, err := s.seal(m.Blocks, colMessageBlocks, m.ID)
	if err != nil {
		return Message{}, err
	}

	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := q.CreateMessage(ctx, db.CreateMessageParams{
			ID: m.ID, ClientID: m.ClientID, Urgency: m.Urgency, Title: sealedTitle, Blocks: sealedBlocks,
			Source: nullString(m.Source), IdempotencyKey: nullString(m.IdempotencyKey),
			RequestID: nullString(m.RequestID), Status: m.Status, CreatedAt: formatTime(now),
		}); err != nil {
			return fmt.Errorf("creating message: %w", mapErr(err))
		}
		for _, a := range nm.Attachments {
			if err := s.createAttachment(ctx, q, m.ID, a, now); err != nil {
				return err
			}
		}
		for _, d := range nm.Deliveries {
			if err := q.CreateDelivery(ctx, db.CreateDeliveryParams{
				ID: id.New(id.Delivery, now), MessageID: m.ID, RecipientID: d.RecipientID, Channel: d.Channel,
				Status: StatusQueued, NextAttemptAt: formatTime(now), CreatedAt: formatTime(now), UpdatedAt: formatTime(now),
			}); err != nil {
				return fmt.Errorf("creating delivery: %w", mapErr(err))
			}
		}
		return nil
	})
	if err != nil {
		return Message{}, err
	}
	return m, nil
}

func (s *Store) createAttachment(ctx context.Context, q *db.Queries, messageID string, a Attachment, now time.Time) error {
	attID := id.New(id.Attachment, now)
	sealed, err := s.seal(a.Bytes, colAttachmentBytes, attID)
	if err != nil {
		return err
	}
	if err := q.CreateAttachment(ctx, db.CreateAttachmentParams{
		ID: attID, MessageID: messageID, ContentType: a.ContentType, Bytes: sealed, Size: int64(len(a.Bytes)),
	}); err != nil {
		return fmt.Errorf("creating attachment: %w", mapErr(err))
	}
	return nil
}

// Message returns a message created by clientID. Another client's message is ErrNotFound.
func (s *Store) Message(ctx context.Context, clientID, messageID string) (Message, error) {
	row, err := s.q.GetMessage(ctx, db.GetMessageParams{ID: messageID, ClientID: clientID})
	if err != nil {
		return Message{}, fmt.Errorf("getting message: %w", mapErr(err))
	}
	return s.messageFromRow(row)
}

// MessageByIdempotencyKey returns clientID's message with this key.
func (s *Store) MessageByIdempotencyKey(ctx context.Context, clientID, key string) (Message, error) {
	row, err := s.q.GetMessageByIdempotencyKey(ctx, db.GetMessageByIdempotencyKeyParams{
		ClientID: clientID, IdempotencyKey: nullString(key),
	})
	if err != nil {
		return Message{}, fmt.Errorf("getting message by idempotency key: %w", mapErr(err))
	}
	return s.messageFromRow(row)
}

// Attachments returns a message's attachments, decrypted.
func (s *Store) Attachments(ctx context.Context, messageID string) ([]Attachment, error) {
	rows, err := s.q.ListAttachmentsForMessage(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("listing attachments: %w", err)
	}
	out := make([]Attachment, 0, len(rows))
	for _, r := range rows {
		b, err := s.open(r.Bytes, colAttachmentBytes, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Attachment{ID: r.ID, MessageID: r.MessageID, ContentType: r.ContentType, Bytes: b, Size: r.Size})
	}
	return out, nil
}

func (s *Store) messageFromRow(r db.Message) (Message, error) {
	title, err := s.open(r.Title, colMessageTitle, r.ID)
	if err != nil {
		return Message{}, err
	}
	blocks, err := s.open(r.Blocks, colMessageBlocks, r.ID)
	if err != nil {
		return Message{}, err
	}
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	redacted, err := parseNullTime(r.RedactedAt)
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID: r.ID, ClientID: r.ClientID, Urgency: r.Urgency, Title: string(title), Blocks: blocks,
		Source: r.Source.String, IdempotencyKey: r.IdempotencyKey.String, RequestID: r.RequestID.String,
		Status: r.Status, CreatedAt: created, RedactedAt: redacted,
	}, nil
}
