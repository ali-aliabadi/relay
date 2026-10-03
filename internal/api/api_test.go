package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/obs"
)

func newServer(t *testing.T, health HealthFunc, logs *bytes.Buffer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewHandler(Deps{
		Logger:       obs.NewLogger(logs, slog.LevelInfo),
		Clock:        time.Now,
		Health:       health,
		MaxBodyBytes: 16,
	}))
	t.Cleanup(srv.Close)
	return srv
}

type response struct {
	StatusCode int
	Header     http.Header
}

// get fetches url and returns the status, headers and decoded JSON body.
func get(t *testing.T, url string) (response, map[string]any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	return response{resp.StatusCode, resp.Header}, body
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		name       string
		health     HealthFunc
		wantStatus int
		wantBody   string
	}{
		{"no check", nil, http.StatusOK, `"status":"ok"`},
		{"healthy", func(context.Context) error { return nil }, http.StatusOK, `"status":"ok"`},
		{
			"unhealthy",
			func(context.Context) error { return errors.New("db locked") },
			http.StatusServiceUnavailable, `"code":"unavailable"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			srv := newServer(t, tt.health, &logs)
			resp, body := get(t, srv.URL+"/healthz")
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			raw, _ := json.Marshal(body)
			if !strings.Contains(string(raw), tt.wantBody) {
				t.Errorf("body = %s, want %s", raw, tt.wantBody)
			}
			if strings.Contains(string(raw), "db locked") {
				t.Errorf("internal error leaked to client: %s", raw)
			}
			if resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get(obs.RequestIDHeader) == "" {
				t.Errorf("headers = %v", resp.Header)
			}
			if !strings.Contains(logs.String(), `"route":"GET /healthz"`) {
				t.Errorf("request not logged: %s", logs.String())
			}
		})
	}
}

func TestUnknownRouteUsesErrorShape(t *testing.T) {
	srv := newServer(t, nil, &bytes.Buffer{})
	resp, body := get(t, srv.URL+"/nope")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
	detail, _ := body["error"].(map[string]any)
	if detail["code"] != "not_found" || detail["message"] == "" {
		t.Errorf("body = %v", body)
	}
}

func TestBodyIsLimited(t *testing.T) {
	var got error
	h := limitBody(16, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, got = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 17)))
	h.ServeHTTP(httptest.NewRecorder(), req)
	var mbe *http.MaxBytesError
	if !errors.As(got, &mbe) {
		t.Fatalf("err = %v, want *http.MaxBytesError", got)
	}
}
