// Package channel defines the delivery channel interface. Each provider is
// one package implementing Channel; nothing else changes when one is added.
package channel

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/obs"
)

// Channel sends rendered messages through one provider.
type Channel interface {
	// Name is the channel's identifier in requests and the database, e.g. "telegram".
	Name() string
	// Send delivers msg to one contact and returns the provider's message ID.
	// Failures are *Error so the worker can tell retryable from permanent.
	Send(ctx context.Context, to Contact, msg message.Message) (providerID string, err error)
	// Preview returns what Send would send, without network access.
	Preview(msg message.Message) (Preview, error)
}

// HealthChecker is implemented by channels that can check their provider.
type HealthChecker interface {
	// Health returns a short, safe reason when the channel can't send.
	Health(ctx context.Context) error
}

// Contact is where to send. Address is private (a Telegram chat ID, later a
// phone number) and never logged.
type Contact struct {
	Address string
}

// LogValue keeps the address out of logs.
func (c Contact) LogValue() slog.Value { return slog.StringValue(obs.Redact(c.Address)) }

// Preview is the ordered list of provider calls Send would make.
type Preview struct {
	Parts []Part `json:"parts"`
}

// Part is one provider call: a text message or a photo.
type Part struct {
	Kind                string   `json:"kind"`              // "text" or "photo"
	Text                string   `json:"text,omitempty"`    // rendered markup (the caption for photos)
	Photo               string   `json:"photo,omitempty"`   // https URL, or "inline" for uploaded bytes
	Buttons             []Button `json:"buttons,omitempty"` // buttons under this part
	DisableNotification bool     `json:"disable_notification,omitempty"`
}

// Button is a link button (URL) or an answer button (Data, sent back by the provider).
type Button struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
	Data string `json:"data,omitempty"`
}

// Error is a failed send. Reason is short, safe to store and show (no
// message content, no addresses, no tokens).
type Error struct {
	Permanent  bool
	RetryAfter time.Duration
	Reason     string
}

func (e *Error) Error() string {
	kind := "transient"
	if e.Permanent {
		kind = "permanent"
	}
	return fmt.Sprintf("%s: %s", kind, e.Reason)
}

// Transient returns a retryable error.
func Transient(format string, args ...any) *Error {
	return &Error{Reason: fmt.Sprintf(format, args...)}
}

// Permanent returns an error that should not be retried.
func Permanent(format string, args ...any) *Error {
	return &Error{Permanent: true, Reason: fmt.Sprintf(format, args...)}
}
