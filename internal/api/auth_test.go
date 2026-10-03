package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

const goodKey = "rk_fake-good-key"

type fakeAuth struct{ err error }

func (f fakeAuth) Authenticate(_ context.Context, key string) (store.Client, error) {
	if f.err != nil {
		return store.Client{}, f.err
	}
	if key != goodKey {
		return store.Client{}, core.ErrUnauthorized
	}
	return store.Client{ID: "cli_test", Name: "test"}, nil
}

func TestRequireAuth(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		auth       fakeAuth
		wantStatus int
	}{
		{"no header", "", fakeAuth{}, http.StatusUnauthorized},
		{"wrong scheme", "Basic " + goodKey, fakeAuth{}, http.StatusUnauthorized},
		{"empty token", "Bearer  ", fakeAuth{}, http.StatusUnauthorized},
		{"wrong key", "Bearer rk_nope", fakeAuth{}, http.StatusUnauthorized},
		{"good key", "Bearer " + goodKey, fakeAuth{}, http.StatusTeapot},
		{"lowercase scheme", "bearer " + goodKey, fakeAuth{}, http.StatusTeapot},
		{"store down", "Bearer " + goodKey, fakeAuth{err: errors.New("db gone")}, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := obs.NewLogger(&logs, slog.LevelDebug)
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if clientFrom(r.Context()).ID != "cli_test" {
					t.Error("client missing from context")
				}
				w.WriteHeader(http.StatusTeapot)
			})
			mux := http.NewServeMux()
			mux.Handle("GET /v1/thing", requireAuth(logger, tt.auth, inner))
			h := obs.Middleware(logger, time.Now, mux)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/thing", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate")
			}
			if strings.Contains(logs.String(), goodKey) || strings.Contains(rec.Body.String(), goodKey) {
				t.Errorf("key leaked: logs %s body %s", logs.String(), rec.Body.String())
			}
			if tt.wantStatus == http.StatusTeapot && !strings.Contains(logs.String(), `"client":"cli_test"`) {
				t.Errorf("request log missing client: %s", logs.String())
			}
		})
	}
}

func TestV1RoutesNeedAuth(t *testing.T) {
	h := NewHandler(Deps{
		Logger: obs.NewLogger(&bytes.Buffer{}, slog.LevelInfo), Clock: time.Now, Auth: fakeAuth{}, MaxBodyBytes: 1024,
	})
	for _, tc := range []struct {
		header string
		want   int
	}{{"", http.StatusUnauthorized}, {"Bearer " + goodKey, http.StatusNotFound}} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/anything", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("auth %q: status = %d, want %d", tc.header, rec.Code, tc.want)
		}
	}
}
