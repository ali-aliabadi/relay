package obs

import (
	"log/slog"
	"strings"
)

// Redacted is what every redaction helper prints in place of a private value.
const Redacted = "[REDACTED]"

// Secret is a string that never prints its value through fmt, slog or JSON.
// Use it for tokens and keys held in config.
type Secret string

// String implements fmt.Stringer.
func (s Secret) String() string { return redactNonEmpty(len(s)) }

// GoString implements fmt.GoStringer so %#v doesn't leak either.
func (s Secret) GoString() string { return s.String() }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// MarshalText implements encoding.TextMarshaler, which JSON uses.
func (s Secret) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Reveal returns the raw value. Only pass it to the code that needs it.
func (s Secret) Reveal() string { return string(s) }

// SecretBytes is a byte slice (such as an encryption key) that never prints its value.
type SecretBytes []byte

// String implements fmt.Stringer.
func (b SecretBytes) String() string { return redactNonEmpty(len(b)) }

// GoString implements fmt.GoStringer.
func (b SecretBytes) GoString() string { return b.String() }

// LogValue implements slog.LogValuer.
func (b SecretBytes) LogValue() slog.Value { return slog.StringValue(b.String()) }

// MarshalText implements encoding.TextMarshaler.
func (b SecretBytes) MarshalText() ([]byte, error) { return []byte(b.String()), nil }

// Redact hides a user-supplied value while keeping whether it was set.
func Redact(s string) string { return redactNonEmpty(len(s)) }

// RedactAttr returns a log attribute that records only that key had a value.
func RedactAttr(key, value string) slog.Attr { return slog.String(key, Redact(value)) }

func redactNonEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return Redacted
}

// sensitiveKeys are attribute keys that are always redacted by the logger, as a
// safety net for code that logs a private value by mistake. Matching is
// case-insensitive on the last segment of the key.
var sensitiveKeys = map[string]bool{
	"authorization":  true,
	"api_key":        true,
	"apikey":         true,
	"token":          true,
	"bot_token":      true,
	"password":       true,
	"secret":         true,
	"encryption_key": true,
	"chat_id":        true,
	"address":        true,
	"phone":          true,
	"email":          true,
	"title":          true,
	"blocks":         true,
	"text":           true,
	"caption":        true,
	"body":           true,
}

// redactSensitive is a slog ReplaceAttr func that blanks sensitive keys.
func redactSensitive(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		return a
	}
	if sensitiveKeys[strings.ToLower(a.Key)] {
		if a.Value.Kind() == slog.KindString && a.Value.String() == "" {
			return a
		}
		return slog.String(a.Key, Redacted)
	}
	return a
}
