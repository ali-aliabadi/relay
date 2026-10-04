package core

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/channel/fake"
	"github.com/ali-aliabadi/relay/internal/crypto"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type harness struct {
	st     *store.Store
	clk    *clock
	ch     *fake.Channel
	w      *Worker
	msgs   *Messages
	client string
	logs   *bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	clk := &clock{t: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)}
	conn, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := store.Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	c, _ := crypto.New(bytes.Repeat([]byte{4}, crypto.KeySize))
	st := store.New(conn, c, clk.Now)
	cl, err := st.CreateClient(t.Context(), "app", bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	r, err := st.CreateRecipient(t.Context(), store.Recipient{Username: "ali", DisplayName: "Ali"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertContact(t.Context(), store.Contact{RecipientID: r.ID, Channel: "telegram", Address: "777"}); err != nil {
		t.Fatal(err)
	}
	ch := fake.New("telegram")
	logs := &bytes.Buffer{}
	chans := []channel.Channel{ch}
	return &harness{
		st: st, clk: clk, ch: ch, client: cl.ID, logs: logs, msgs: NewMessages(st, chans),
		w: &Worker{Store: st, Channels: chans, Logger: obs.NewLogger(logs, slog.LevelInfo), Clock: clk.Now, Poll: time.Millisecond},
	}
}

func (h *harness) send(t *testing.T, req message.Request) store.Message {
	t.Helper()
	m, _, err := h.msgs.Create(t.Context(), h.client, "req_1", req)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (h *harness) deliveries(t *testing.T, id string) []store.Delivery {
	t.Helper()
	ds, err := h.st.Deliveries(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func (h *harness) status(t *testing.T, id string) string {
	t.Helper()
	m, err := h.st.Message(t.Context(), h.client, id)
	if err != nil {
		t.Fatal(err)
	}
	return m.Status
}

func TestWorkerDelivers(t *testing.T) {
	h := newHarness(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), 9, 9)
	m := h.send(t, message.Request{To: []string{"ali"}, Title: "Private title", Blocks: []message.Block{
		{Type: "text", Text: "private body"},
		{Type: "image", Base64: b64(png), ContentType: "image/png"},
	}})
	if n, err := h.w.Tick(t.Context()); n != 1 || err != nil {
		t.Fatalf("Tick = %d, %v", n, err)
	}
	sent := h.ch.Sent()
	if len(sent) != 1 || sent[0].To.Address != "777" || sent[0].Msg.Title != "Private title" ||
		len(sent[0].Msg.Attachments) != 1 || !bytes.Equal(sent[0].Msg.Attachments[0].Bytes, png) {
		t.Fatalf("sent = %+v", sent)
	}
	d := h.deliveries(t, m.ID)[0]
	if d.Status != store.StatusDelivered || d.ProviderMessageID != "fake-1" || d.Attempts != 1 {
		t.Errorf("delivery = %+v", d)
	}
	if s := h.status(t, m.ID); s != store.StatusDelivered {
		t.Errorf("message status = %s", s)
	}
	out := h.logs.String()
	if !strings.Contains(out, `"result":"delivered"`) || !strings.Contains(out, m.ID) {
		t.Errorf("outcome not logged: %s", out)
	}
	for _, leak := range []string{"Private title", "private body", "777"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q", leak)
		}
	}
	if n, _ := h.w.Tick(t.Context()); n != 0 {
		t.Error("delivered message claimed again")
	}
}

func TestWorkerRetriesWithBackoff(t *testing.T) {
	h := newHarness(t)
	m := h.send(t, message.Request{To: []string{"ali"}, Text: "x"})
	h.ch.FailNext(channel.Transient("telegram: 502 Bad Gateway"), &channel.Error{Reason: "telegram: rate limited", RetryAfter: time.Minute})

	_, _ = h.w.Tick(t.Context())
	d := h.deliveries(t, m.ID)[0]
	if d.Status != store.StatusQueued || d.Attempts != 1 || d.LastError != "telegram: 502 Bad Gateway" ||
		!d.NextAttemptAt.Equal(h.clk.Now().Add(10*time.Second)) {
		t.Fatalf("after 1st failure = %+v", d)
	}
	if h.status(t, m.ID) != store.StatusQueued {
		t.Errorf("status = %s", h.status(t, m.ID))
	}
	if n, _ := h.w.Tick(t.Context()); n != 0 {
		t.Error("claimed before next_attempt_at")
	}

	h.clk.Advance(10 * time.Second)
	_, _ = h.w.Tick(t.Context())
	d = h.deliveries(t, m.ID)[0]
	if d.Attempts != 2 || !d.NextAttemptAt.Equal(h.clk.Now().Add(time.Minute)) {
		t.Fatalf("retry_after not honoured: %+v", d)
	}
	if !strings.Contains(h.logs.String(), `"level":"WARN"`) {
		t.Error("retry not logged at warn")
	}

	h.clk.Advance(time.Minute)
	_, _ = h.w.Tick(t.Context())
	if d = h.deliveries(t, m.ID)[0]; d.Status != store.StatusDelivered || d.Attempts != 3 || d.LastError != "" {
		t.Errorf("after recovery = %+v", d)
	}
}

func TestWorkerGivesUp(t *testing.T) {
	h := newHarness(t)
	m := h.send(t, message.Request{To: []string{"ali"}, Text: "x"})
	for range MaxAttempts {
		h.ch.FailNext(channel.Transient("timeout"))
	}
	for i := range MaxAttempts {
		_, _ = h.w.Tick(t.Context())
		h.clk.Advance(time.Hour)
		if i < MaxAttempts-1 && h.deliveries(t, m.ID)[0].Status != store.StatusQueued {
			t.Fatalf("gave up after %d attempts", i+1)
		}
	}
	d := h.deliveries(t, m.ID)[0]
	if d.Status != store.StatusFailed || d.Attempts != MaxAttempts || h.status(t, m.ID) != store.StatusFailed {
		t.Errorf("after max attempts = %+v, message %s", d, h.status(t, m.ID))
	}
}

func TestWorkerPermanentFailure(t *testing.T) {
	h := newHarness(t)
	m := h.send(t, message.Request{To: []string{"ali"}, Text: "x"})
	h.ch.FailNext(channel.Permanent("telegram: 403 Forbidden: bot was blocked by the user"))
	_, _ = h.w.Tick(t.Context())
	if d := h.deliveries(t, m.ID)[0]; d.Status != store.StatusFailed || d.Attempts != 1 {
		t.Errorf("delivery = %+v", d)
	}
}

func TestWorkerUnconfiguredChannelFails(t *testing.T) {
	h := newHarness(t)
	m := h.send(t, message.Request{To: []string{"ali"}, Text: "x"})
	h.w.Channels = nil
	_, _ = h.w.Tick(t.Context())
	if d := h.deliveries(t, m.ID)[0]; d.Status != store.StatusFailed || !strings.Contains(d.LastError, "not configured") {
		t.Errorf("delivery = %+v", d)
	}
}

func TestWorkerRequeuesStuckOnStart(t *testing.T) {
	h := newHarness(t)
	m := h.send(t, message.Request{To: []string{"ali"}, Text: "x"})
	if _, err := h.st.ClaimDue(t.Context(), 10); err != nil { // simulate a crash mid-send
		t.Fatal(err)
	}
	if d := h.deliveries(t, m.ID)[0]; d.Status != store.StatusSending {
		t.Fatalf("setup: %+v", d)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- h.w.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(h.ch.Sent()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if d := h.deliveries(t, m.ID)[0]; d.Status != store.StatusDelivered || d.Attempts != 2 {
		t.Errorf("stuck delivery after restart = %+v", d)
	}
}

func TestNextAttempt(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	want := []time.Duration{10 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	for i, w := range want {
		next, ok := NextAttempt(now, i+1, 0)
		if !ok || next.Sub(now) != w {
			t.Errorf("attempt %d: %v, %v", i+1, next.Sub(now), ok)
		}
	}
	if _, ok := NextAttempt(now, MaxAttempts, 0); ok {
		t.Error("retry after the last attempt")
	}
	if next, _ := NextAttempt(now, 1, time.Hour); next.Sub(now) != time.Hour {
		t.Error("retryAfter ignored")
	}
}

func TestDeriveStatus(t *testing.T) {
	tests := []struct {
		counts map[string]int64
		want   string
	}{
		{map[string]int64{}, "queued"},
		{map[string]int64{"queued": 2}, "queued"},
		{map[string]int64{"sending": 1, "queued": 1}, "sending"},
		{map[string]int64{"delivered": 1, "queued": 1}, "sending"},
		{map[string]int64{"delivered": 2}, "delivered"},
		{map[string]int64{"failed": 2}, "failed"},
		{map[string]int64{"delivered": 1, "failed": 1}, "partially_delivered"},
	}
	for _, tt := range tests {
		if got := store.DeriveStatus(tt.counts); got != tt.want {
			t.Errorf("DeriveStatus(%v) = %s, want %s", tt.counts, got, tt.want)
		}
	}
}

// TestWorkerStopsCleanlyDuringStartup: a shutdown that lands before or during
// the startup requeue is a clean stop, not an error, and logs nothing at error.
func TestWorkerStopsCleanlyDuringStartup(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := h.w.Run(ctx); err != nil {
		t.Fatalf("Run with a cancelled context = %v, want nil", err)
	}
	if strings.Contains(h.logs.String(), `"level":"ERROR"`) {
		t.Errorf("error logged on shutdown: %s", h.logs.String())
	}
}
