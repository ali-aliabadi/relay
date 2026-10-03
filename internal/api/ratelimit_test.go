package api

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/obs"
)

func TestFailLimiter(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	l := newFailLimiter(func() time.Time { return now })
	for range failLimit - 1 {
		l.fail("192.0.2.1")
	}
	if l.blocked("192.0.2.1") != 0 {
		t.Fatal("blocked before the limit")
	}
	l.fail("192.0.2.1")
	if got := l.blocked("192.0.2.1"); got != failWindow {
		t.Fatalf("blocked = %v, want %v", got, failWindow)
	}
	if l.blocked("192.0.2.2") != 0 {
		t.Error("another IP is blocked")
	}
	now = now.Add(failWindow)
	if l.blocked("192.0.2.1") != 0 {
		t.Error("still blocked after the window")
	}
	l.fail("192.0.2.1")
	if l.blocked("192.0.2.1") != 0 {
		t.Error("a new window started with the old count")
	}
}

func TestFailLimiterBoundsMemory(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	l := newFailLimiter(func() time.Time { return now })
	for i := range failMaxIPs + 50 {
		l.fail(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	if len(l.entries) != failMaxIPs {
		t.Fatalf("entries = %d, want %d", len(l.entries), failMaxIPs)
	}
	now = now.Add(failWindow)
	l.fail("192.0.2.9")
	if len(l.entries) != 1 {
		t.Errorf("expired entries not pruned: %d", len(l.entries))
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		xff    []string
		trust  bool
		want   string
	}{
		{"peer", "192.0.2.1:5000", nil, false, "192.0.2.1"},
		{"xff ignored when untrusted", "192.0.2.1:5000", []string{"203.0.113.7"}, false, "192.0.2.1"},
		{"last xff entry", "172.18.0.3:5000", []string{"198.51.100.1, 203.0.113.7"}, true, "203.0.113.7"},
		{"last xff header", "172.18.0.3:5000", []string{"198.51.100.1", "203.0.113.7"}, true, "203.0.113.7"},
		{"bad xff falls back", "172.18.0.3:5000", []string{"not-an-ip"}, true, "172.18.0.3"},
		{"ipv6", "[2001:db8::1]:5000", nil, false, "2001:db8::1"},
		{"no port", "garbage", nil, false, unknownAddr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r, tt.trust); got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAuthRateLimit: after failLimit bad keys an IP gets 429 with
// Retry-After, even with a good key; other IPs are unaffected.
func TestAuthRateLimit(t *testing.T) {
	h := NewHandler(Deps{
		Logger: obs.NewLogger(&bytes.Buffer{}, slog.LevelInfo), Clock: time.Now, Auth: fakeAuth{},
		MaxBodyBytes: 1024, TrustForwardedFor: true,
	})
	do := func(ip, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/anything", nil)
		req.Header.Set("X-Forwarded-For", ip)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for range failLimit {
		if rec := do("203.0.113.7", "rk_wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("bad key = %d, want 401", rec.Code)
		}
	}
	rec := do("203.0.113.7", goodKey)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after limit = %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := do("203.0.113.8", goodKey); rec.Code != http.StatusNotFound {
		t.Errorf("other IP = %d, want 404 (authenticated)", rec.Code)
	}
}
