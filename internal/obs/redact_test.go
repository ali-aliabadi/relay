package obs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestSecretFormatting(t *testing.T) {
	const raw = "fake-token-value"
	s := Secret(raw)
	b := SecretBytes(raw)
	j, err := json.Marshal(struct {
		S Secret
		B SecretBytes
	}{s, b})
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{
		s.String(), fmt.Sprintf("%v %s %+v %#v", s, s, s, s),
		b.String(), fmt.Sprintf("%v %s %+v %#v", b, b, b, b),
		string(j),
	}
	for _, out := range outputs {
		if strings.Contains(out, raw) {
			t.Errorf("secret leaked: %q", out)
		}
	}
	if s.Reveal() != raw {
		t.Errorf("Reveal = %q", s.Reveal())
	}
	if Secret("").String() != "" || SecretBytes(nil).String() != "" {
		t.Error("empty secrets should print as empty")
	}
}

func TestRedact(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"ali's chat", Redacted},
	}
	for _, tt := range tests {
		if got := Redact(tt.in); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if a := RedactAttr("chat", "12345"); a.Value.String() != Redacted || a.Key != "chat" {
		t.Errorf("RedactAttr = %v", a)
	}
}

func TestLoggerRedactsSensitiveKeys(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelDebug)
	logger.Debug("oops",
		slog.String("title", "Backup failed on nas"),
		slog.String("Authorization", "Bearer rk_fake"),
		slog.Int64("chat_id", 424242),
		slog.Group("msg", slog.String("text", "private words")),
		slog.String("token", ""),
		slog.String("message_id", "msg_01ABC"),
	)
	out := buf.String()
	for _, leaked := range []string{"Backup failed", "rk_fake", "424242", "private words"} {
		if strings.Contains(out, leaked) {
			t.Errorf("log leaked %q: %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"message_id":"msg_01ABC"`) {
		t.Errorf("IDs must still be logged: %s", out)
	}
	if !strings.Contains(out, `"token":""`) {
		t.Errorf("empty sensitive values stay empty: %s", out)
	}
}

func TestLoggerAddsRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelInfo).With("component", "test").WithGroup("g")
	logger.InfoContext(WithRequestID(t.Context(), "req_abc"), "hello", "k", "v")
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("not JSON: %v: %s", err, buf.String())
	}
	if line["component"] != "test" {
		t.Errorf("missing With attrs: %v", line)
	}
	if !strings.Contains(buf.String(), `"request_id":"req_abc"`) {
		t.Errorf("missing request_id: %s", buf.String())
	}
}

func TestLoggerLevel(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf, slog.LevelInfo).Debug("hidden")
	if buf.Len() != 0 {
		t.Errorf("debug logged at info level: %s", buf.String())
	}
}
