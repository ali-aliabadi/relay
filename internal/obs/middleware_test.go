package obs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixedClock() func() time.Time {
	t0 := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	calls := 0
	return func() time.Time {
		calls++
		return t0.Add(time.Duration(calls) * 5 * time.Millisecond)
	}
}

func newTestHandler(buf *bytes.Buffer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/recipients/{username}", func(w http.ResponseWriter, r *http.Request) {
		SetClient(r.Context(), "cli_test")
		w.WriteHeader(http.StatusTeapot)
		w.WriteHeader(http.StatusOK) // ignored: first status wins
	})
	return Middleware(NewLogger(buf, slog.LevelInfo), fixedClock(), mux)
}

func decodeLine(tb testing.TB, buf *bytes.Buffer) map[string]any {
	tb.Helper()
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		tb.Fatalf("log is not one JSON line: %v: %q", err, buf.String())
	}
	return line
}

func TestMiddlewareLogsOneLine(t *testing.T) {
	var buf bytes.Buffer
	h := newTestHandler(&buf)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/recipients/ali?secret=x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	line := decodeLine(t, &buf)
	want := map[string]any{
		"msg":    "request",
		"client": "cli_test",
		"method": "GET",
		"route":  "GET /v1/recipients/{username}",
		"status": float64(http.StatusTeapot),
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	if line["duration"] == nil {
		t.Error("missing duration")
	}
	if strings.Contains(buf.String(), "ali") || strings.Contains(buf.String(), "secret") {
		t.Errorf("raw path or query leaked: %s", buf.String())
	}
	id := rec.Header().Get(RequestIDHeader)
	if !strings.HasPrefix(id, "req_") || line["request_id"] != id {
		t.Errorf("request id header %q vs log %v", id, line["request_id"])
	}
}

func TestMiddlewareRequestIDHeader(t *testing.T) {
	tests := []struct {
		name, in string
		keep     bool
	}{
		{"accepts well-formed", "abc-123_X.y", true},
		{"rejects injection", "abc\ninjected", false},
		{"rejects spaces", "a b", false},
		{"rejects too long", strings.Repeat("a", 65), false},
		{"generates when missing", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)
			if tc.in != "" {
				req.Header.Set(RequestIDHeader, tc.in)
			}
			rec := httptest.NewRecorder()
			newTestHandler(&buf).ServeHTTP(rec, req)
			got := rec.Header().Get(RequestIDHeader)
			if tc.keep && got != tc.in {
				t.Errorf("id = %q, want %q", got, tc.in)
			}
			if !tc.keep && !strings.HasPrefix(got, "req_") {
				t.Errorf("id = %q, want a generated one", got)
			}
			if line := decodeLine(t, &buf); line["route"] != "unmatched" || line["status"] != float64(404) {
				t.Errorf("unmatched request logged as %v", line)
			}
		})
	}
}

func TestNewRequestIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := NewRequestID()
		if seen[id] || !validRequestID(id) {
			t.Fatalf("bad or duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestSetClientWithoutMiddleware(t *testing.T) {
	SetClient(t.Context(), "cli_x") // must not panic
}
