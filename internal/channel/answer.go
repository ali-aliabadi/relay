package channel

import (
	"context"
	"log/slog"
)

// Answer is a recipient's reply to a question, as a channel received it.
// Address and Text are private and never logged.
type Answer struct {
	Channel           string
	Address           string // who replied, e.g. the Telegram chat ID
	DeliveryID        string // set by answer buttons
	ProviderMessageID string // set by typed replies: the message replied to
	Option            int    // the tapped option, or -1 for a typed reply
	Text              string // the typed reply
}

// LogValue keeps the address and text out of logs.
func (a Answer) LogValue() slog.Value {
	return slog.GroupValue(slog.String("channel", a.Channel), slog.String("delivery_id", a.DeliveryID))
}

// Answer outcomes, which a channel turns into feedback for the recipient.
const (
	AnswerSaved      = "saved"       // stored (or replaced)
	AnswerFinal      = "final"       // the app already fetched it; can't change
	AnswerExpired    = "expired"     // content purged by retention
	AnswerUnknown    = "unknown"     // not a question, or not this recipient's
	AnswerNeedOption = "need_option" // typed reply to a question with buttons
	AnswerNeedText   = "need_text"   // empty typed reply
)

// AnswerResult is what Relay did with an Answer. Options are the question's
// options, so a channel can redraw its buttons.
type AnswerResult struct {
	Outcome string
	Options []string
}

// AnswerFunc records an answer. Channels that receive replies call it.
type AnswerFunc func(ctx context.Context, a Answer) (AnswerResult, error)
