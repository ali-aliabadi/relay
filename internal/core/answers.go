package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

// Answers records recipients' answers to questions.
type Answers struct {
	Store  *store.Store
	Logger *slog.Logger
	// Notify, if set, tells the asking app an answer arrived (see Webhooks).
	Notify func(ctx context.Context, url, messageID string)
}

// Record implements channel.AnswerFunc.
func (a *Answers) Record(ctx context.Context, in channel.Answer) (channel.AnswerResult, error) {
	d, err := a.delivery(ctx, in)
	if errors.Is(err, store.ErrNotFound) {
		return channel.AnswerResult{Outcome: channel.AnswerUnknown}, nil
	}
	if err != nil {
		return channel.AnswerResult{}, err
	}
	m, err := a.Store.MessageByID(ctx, d.MessageID)
	if err != nil {
		return channel.AnswerResult{}, err
	}
	res, text := evaluate(m, in)
	if res.Outcome == channel.AnswerSaved {
		saved, err := a.Store.SaveAnswer(ctx, m.ID, d.RecipientID, text)
		if err != nil {
			return channel.AnswerResult{}, err
		}
		if !saved {
			res.Outcome = channel.AnswerFinal
		}
	}
	a.Logger.InfoContext(ctx, "answer", slog.String("message_id", m.ID), slog.String("delivery_id", d.ID),
		slog.String("channel", in.Channel), slog.String("result", res.Outcome))
	if res.Outcome == channel.AnswerSaved && a.Notify != nil {
		if q, _ := questionOf(m); q.Webhook != "" {
			a.Notify(ctx, q.Webhook, m.ID)
		}
	}
	return res, nil
}

// evaluate checks an answer against the message's question and returns the
// outcome and, when saved, the answer text.
func evaluate(m store.Message, in channel.Answer) (channel.AnswerResult, string) {
	if m.RedactedAt != nil {
		return channel.AnswerResult{Outcome: channel.AnswerExpired}, ""
	}
	q, ok := questionOf(m)
	if !ok {
		return channel.AnswerResult{Outcome: channel.AnswerUnknown}, ""
	}
	res := channel.AnswerResult{Outcome: channel.AnswerSaved, Options: q.Options}
	switch {
	case len(q.Options) > 0 && in.Option < 0:
		res.Outcome = channel.AnswerNeedOption
	case len(q.Options) > 0 && in.Option >= len(q.Options), len(q.Options) == 0 && in.Option >= 0:
		res.Outcome = channel.AnswerUnknown
	case len(q.Options) > 0:
		return res, q.Options[in.Option]
	case strings.TrimSpace(in.Text) == "":
		res.Outcome = channel.AnswerNeedText
	}
	return res, truncateRunes(in.Text, message.MaxTextLen)
}

func questionOf(m store.Message) (message.Block, bool) {
	var blocks []message.Block
	if err := json.Unmarshal(m.Blocks, &blocks); err != nil {
		return message.Block{}, false
	}
	return message.Question(blocks)
}

// delivery finds the delivery an answer belongs to, and only if it was sent
// to the chat that answered: someone else can't answer for a recipient.
func (a *Answers) delivery(ctx context.Context, in channel.Answer) (store.Delivery, error) {
	var candidates []store.Delivery
	if in.DeliveryID != "" {
		d, err := a.Store.Delivery(ctx, in.DeliveryID)
		if err != nil {
			return store.Delivery{}, err
		}
		candidates = []store.Delivery{d}
	} else {
		ds, err := a.Store.DeliveriesByProviderID(ctx, in.Channel, in.ProviderMessageID)
		if err != nil {
			return store.Delivery{}, err
		}
		candidates = ds
	}
	for _, d := range candidates {
		if d.Channel != in.Channel {
			continue
		}
		c, err := a.Store.Contact(ctx, d.RecipientID, d.Channel)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.Delivery{}, err
		}
		if c.Address == in.Address {
			return d, nil
		}
	}
	return store.Delivery{}, store.ErrNotFound
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
