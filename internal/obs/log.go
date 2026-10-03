// Package obs holds logging, redaction, request IDs and (later) metrics.
package obs

import (
	"context"
	"io"
	"log/slog"
)

// NewLogger returns a JSON logger at level that adds the request ID from the
// context to every record and redacts known-sensitive attribute keys.
func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactSensitive,
	})
	return slog.New(contextHandler{Handler: h})
}

// contextHandler copies request-scoped values from the context into records.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}
