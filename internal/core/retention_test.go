package core

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

func TestRetention(t *testing.T) {
	h := newHarness(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), 1)
	ancient := h.send(t, message.Request{To: []string{"ali"}, Text: "ancient"})
	h.clk.Advance(150 * 24 * time.Hour)
	old := h.send(t, message.Request{To: []string{"ali"}, Title: "old title", Blocks: []message.Block{
		{Type: "text", Text: "old"}, {Type: "image", Base64: b64(png), ContentType: "image/png"},
	}})
	h.clk.Advance(29 * 24 * time.Hour)
	recent := h.send(t, message.Request{To: []string{"ali"}, Text: "recent"})
	h.clk.Advance(2 * 24 * time.Hour) // ancient is now 181 days old, old 31, recent 2

	logs := &bytes.Buffer{}
	r := &Retention{Store: h.st, Days: 30, Logger: obs.NewLogger(logs, slog.LevelInfo), Clock: h.clk.Now}
	r.Pass(t.Context())

	if _, err := h.st.Message(t.Context(), h.client, ancient.ID); err == nil {
		t.Error("message older than 180 days still exists")
	}
	if ds := h.deliveries(t, ancient.ID); len(ds) != 0 {
		t.Error("deliveries of a deleted message remain")
	}
	got, err := h.st.Message(t.Context(), h.client, old.ID)
	if err != nil || got.Title != "" || got.Blocks != nil || got.RedactedAt == nil {
		t.Errorf("31-day-old message = %+v, %v", got, err)
	}
	if atts, _ := h.st.Attachments(t.Context(), old.ID); len(atts) != 0 {
		t.Error("attachment kept past retention")
	}
	if ds := h.deliveries(t, old.ID); len(ds) != 1 {
		t.Error("redaction removed delivery metadata")
	}
	if got, _ := h.st.Message(t.Context(), h.client, recent.ID); string(got.Blocks) == "" || got.RedactedAt != nil {
		t.Errorf("recent message purged: %+v", got)
	}
	if !strings.Contains(logs.String(), `"messages_redacted":1`) || !strings.Contains(logs.String(), `"messages_deleted":1`) {
		t.Errorf("purge not logged: %s", logs.String())
	}

	logs.Reset()
	r.Pass(t.Context())
	if logs.Len() != 0 {
		t.Errorf("empty pass logged: %s", logs.String())
	}
}

func TestWorkerFailsRedactedMessage(t *testing.T) {
	h := newHarness(t)
	m := h.send(t, message.Request{To: []string{"ali"}, Text: "x"})
	h.clk.Advance(31 * 24 * time.Hour)
	(&Retention{Store: h.st, Days: 30, Logger: obs.NewLogger(&bytes.Buffer{}, slog.LevelInfo), Clock: h.clk.Now}).Pass(t.Context())
	_, _ = h.w.Tick(t.Context())
	if d := h.deliveries(t, m.ID)[0]; d.Status != store.StatusFailed || !strings.Contains(d.LastError, "expired") {
		t.Errorf("delivery = %+v", d)
	}
}
