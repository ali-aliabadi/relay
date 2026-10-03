package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

// Messages accepts and reads messages.
type Messages struct {
	store      *store.Store
	configured map[string]bool
}

// NewMessages returns a Messages service that routes to the configured channels.
func NewMessages(s *store.Store, configured []string) *Messages {
	m := &Messages{store: s, configured: map[string]bool{}}
	for _, ch := range configured {
		m.configured[ch] = true
	}
	return m
}

// Create validates req, resolves recipients and stores a queued message with
// its deliveries. A repeated idempotency key returns the original message and
// created=false. Invalid requests return a *message.ValidationError.
func (m *Messages) Create(ctx context.Context, clientID, requestID string, req message.Request) (store.Message, bool, error) {
	req, images, err := message.Normalize(req)
	if err != nil {
		return store.Message{}, false, err
	}
	if err := m.checkChannels(req.Channels); err != nil {
		return store.Message{}, false, err
	}
	if req.IdempotencyKey != "" {
		existing, err := m.store.MessageByIdempotencyKey(ctx, clientID, req.IdempotencyKey)
		if err == nil {
			return existing, false, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.Message{}, false, err
		}
	}
	plan, err := m.plan(ctx, req)
	if err != nil {
		return store.Message{}, false, err
	}
	blocks, err := json.Marshal(req.Blocks)
	if err != nil {
		return store.Message{}, false, fmt.Errorf("encoding blocks: %w", err)
	}
	nm := store.NewMessage{
		Message: store.Message{
			ClientID: clientID, Urgency: req.Urgency, Title: req.Title, Blocks: blocks,
			Source: req.Source, IdempotencyKey: req.IdempotencyKey, RequestID: requestID,
		},
		Deliveries: plan,
	}
	for _, img := range images {
		nm.Attachments = append(nm.Attachments, store.Attachment{ContentType: img.ContentType, Bytes: img.Bytes})
	}
	created, err := m.store.CreateMessage(ctx, nm)
	if errors.Is(err, store.ErrConflict) && req.IdempotencyKey != "" {
		// Lost a race with a concurrent request carrying the same key.
		existing, getErr := m.store.MessageByIdempotencyKey(ctx, clientID, req.IdempotencyKey)
		return existing, false, getErr
	}
	if err != nil {
		return store.Message{}, false, err
	}
	return created, true, nil
}

func (m *Messages) checkChannels(channels []string) error {
	var problems []string
	for i, ch := range channels {
		if !m.configured[ch] {
			problems = append(problems, fmt.Sprintf("channels[%d]: not a configured channel", i))
		}
	}
	if problems != nil {
		return &message.ValidationError{Problems: problems}
	}
	return nil
}

// plan resolves every recipient and routes it. Unknown or unreachable
// recipients are reported by position, never by name.
func (m *Messages) plan(ctx context.Context, req message.Request) ([]store.PlannedDelivery, error) {
	found, err := m.store.RecipientsByUsernames(ctx, req.To)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]store.Recipient, len(found))
	for _, r := range found {
		byName[r.Username] = r
	}
	var plan []store.PlannedDelivery
	var problems []string
	for i, username := range req.To {
		r, ok := byName[username]
		if !ok {
			problems = append(problems, fmt.Sprintf("to[%d]: unknown recipient", i))
			continue
		}
		contacts, err := m.store.ContactsForRecipient(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		linked := make(map[string]bool, len(contacts))
		for _, c := range contacts {
			linked[c.Channel] = true
		}
		channels := Plan(PlanInput{
			Urgency: req.Urgency, Preference: r.ChannelPreference, Override: req.Channels,
			Linked: linked, Configured: m.configured,
		})
		if len(channels) == 0 {
			problems = append(problems, fmt.Sprintf("to[%d]: recipient has no linked channel to send on", i))
		}
		for _, ch := range channels {
			plan = append(plan, store.PlannedDelivery{RecipientID: r.ID, Channel: ch})
		}
	}
	if problems != nil {
		return nil, &message.ValidationError{Problems: problems}
	}
	return plan, nil
}

// Get returns one of clientID's messages with its deliveries.
func (m *Messages) Get(ctx context.Context, clientID, messageID string) (store.Message, []store.Delivery, error) {
	msg, err := m.store.Message(ctx, clientID, messageID)
	if err != nil {
		return store.Message{}, nil, err
	}
	ds, err := m.store.Deliveries(ctx, msg.ID)
	if err != nil {
		return store.Message{}, nil, err
	}
	return msg, ds, nil
}

// List returns one page of clientID's messages, newest first.
func (m *Messages) List(ctx context.Context, f store.ListFilter) ([]store.Message, error) {
	return m.store.ListMessages(ctx, f)
}
