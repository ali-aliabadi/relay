package obs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// RequestIDHeader is the header used to accept and return request IDs.
const RequestIDHeader = "X-Request-ID"

const maxRequestIDLen = 64

type ctxKey int

const (
	requestIDKey ctxKey = iota
	requestInfoKey
)

// WithRequestID returns a context carrying id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID returns the request ID in ctx, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// NewRequestID returns a random request ID such as "req_9f86d081884c7d659a2feaa0".
func NewRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails on supported platforms
	return "req_" + hex.EncodeToString(b[:])
}

// validRequestID reports whether a caller-supplied ID is safe to log and echo:
// short and limited to letters, digits, '-', '_' and '.'.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}
