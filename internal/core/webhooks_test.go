package core

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/obs"
)

type hookServer struct {
	srv    *httptest.Server
	hits   atomic.Int32
	bodies chan map[string]any
}

// newHookServer answers with statuses in order, then 200.
func newHookServer(t *testing.T, statuses ...int) *hookServer {
	t.Helper()
	h := &hookServer{bodies: make(chan map[string]any, 10)}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(h.hits.Add(1))
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		h.bodies <- body
		if n <= len(statuses) {
			w.WriteHeader(statuses[n-1])
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// testWebhooks talks to loopback test servers, which NewWebhooks refuses.
func testWebhooks(logs io.Writer, client *http.Client) *Webhooks {
	return &Webhooks{Client: client, Logger: obs.NewLogger(logs, slog.LevelInfo), Delays: []time.Duration{time.Millisecond, time.Millisecond}}
}

func TestWebhookPostsOnlyTheMessageID(t *testing.T) {
	h := newHookServer(t)
	w := testWebhooks(io.Discard, h.srv.Client())
	w.Notify(t.Context(), h.srv.URL+"/hook", "msg_1")
	w.Wait()
	body := <-h.bodies
	if len(body) != 2 || body["message_id"] != "msg_1" || body["event"] != "answer" {
		t.Errorf("body = %v", body)
	}
}

func TestWebhookRetriesThenSucceeds(t *testing.T) {
	h := newHookServer(t, http.StatusInternalServerError, http.StatusServiceUnavailable)
	var logs syncBuffer
	w := testWebhooks(&logs, h.srv.Client())
	w.Notify(t.Context(), h.srv.URL, "msg_1")
	w.Wait()
	if h.hits.Load() != 3 || logs.String() != "" {
		t.Errorf("hits = %d, logs = %s", h.hits.Load(), logs.String())
	}
}

func TestWebhookGivesUpAndLogsWithoutURL(t *testing.T) {
	h := newHookServer(t, 500, 500, 500, 500)
	var logs syncBuffer
	w := testWebhooks(&logs, h.srv.Client())
	w.Notify(t.Context(), h.srv.URL+"/hook?token=SECRET-token", "msg_1")
	w.Wait()
	if h.hits.Load() != 3 {
		t.Errorf("hits = %d, want 1 + %d retries", h.hits.Load(), len(w.Delays))
	}
	out := logs.String()
	if !strings.Contains(out, "answer webhook failed") || !strings.Contains(out, "msg_1") || !strings.Contains(out, "status 500") {
		t.Errorf("logs = %s", out)
	}
	if strings.Contains(out, "SECRET") || strings.Contains(out, "127.0.0.1") {
		t.Errorf("logs leak the URL: %s", out)
	}
}

func TestWebhookStopsOnShutdown(t *testing.T) {
	h := newHookServer(t, 500, 500, 500)
	w := testWebhooks(io.Discard, h.srv.Client())
	w.Delays = []time.Duration{time.Hour}
	ctx, cancel := context.WithCancel(t.Context())
	w.Notify(ctx, h.srv.URL, "msg_1")
	<-h.bodies
	cancel()
	done := make(chan struct{})
	go func() { w.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait blocked through a cancelled retry delay")
	}
}

func TestWebhookRefusesInternalAddresses(t *testing.T) {
	h := newHookServer(t)
	var logs syncBuffer
	w := NewWebhooks(obs.NewLogger(&logs, slog.LevelInfo))
	w.Delays = []time.Duration{time.Hour}     // refused addresses must not wait for retries
	w.Notify(t.Context(), h.srv.URL, "msg_1") // httptest listens on 127.0.0.1
	w.Notify(t.Context(), "https://localhost:1/hook", "msg_2")
	w.Wait()
	if h.hits.Load() != 0 {
		t.Fatal("the production client reached a loopback address")
	}
	if n := strings.Count(logs.String(), "webhook address is not public"); n != 2 {
		t.Errorf("logs = %s", logs.String())
	}
}

func TestWebhookDoesNotFollowRedirects(t *testing.T) {
	var target atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal" {
			target.Add(1)
			return
		}
		http.Redirect(w, r, "/internal", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	w := NewWebhooks(obs.NewLogger(io.Discard, slog.LevelInfo))
	w.Client.Transport = srv.Client().Transport // keep the redirect policy, allow loopback
	w.Delays = nil
	w.Notify(t.Context(), srv.URL+"/hook", "msg_1")
	w.Wait()
	if target.Load() != 0 {
		t.Error("followed a redirect")
	}
}

func TestPublicOnly(t *testing.T) {
	tests := map[string]bool{
		"8.8.8.8:443":             true,
		"1.1.1.1:80":              true,
		"[2606:4700::1111]:443":   true,
		"127.0.0.1:443":           false,
		"10.0.0.1:443":            false,
		"172.16.5.4:443":          false,
		"192.168.1.1:443":         false,
		"169.254.169.254:80":      false, // cloud metadata
		"100.64.0.1:443":          false, // carrier-grade NAT / tailscale
		"0.0.0.0:443":             false,
		"255.255.255.255:443":     false,
		"224.0.0.1:443":           false,
		"[::1]:443":               false,
		"[fe80::1]:443":           false,
		"[fc00::1]:443":           false,
		"[::ffff:127.0.0.1]:443":  false,
		"[::ffff:192.168.0.1]:80": false,
		"[::]:443":                false,
		"not-an-address":          false,
	}
	for addr, ok := range tests {
		if err := publicOnly("tcp", addr, nil); (err == nil) != ok {
			t.Errorf("publicOnly(%s) = %v, want allowed=%v", addr, err, ok)
		}
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
