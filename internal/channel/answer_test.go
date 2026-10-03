package channel

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestLogValuesHidePrivateData(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("x", slog.Any("answer", Answer{Channel: "telegram", Address: "SECRET-chat", DeliveryID: "dlv_1", Text: "SECRET-text"}),
		slog.Any("contact", Contact{Address: "SECRET-address"}))
	if out := buf.String(); strings.Contains(out, "SECRET") || !strings.Contains(out, "dlv_1") {
		t.Errorf("log = %s", out)
	}
}
