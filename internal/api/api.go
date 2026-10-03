// Package api holds Relay's HTTP handlers, middleware and wire types.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/ali-aliabadi/relay/internal/obs"
)

// HealthFunc reports whether Relay's dependencies (the database) are usable.
type HealthFunc func(ctx context.Context) error

// Deps are what the HTTP layer needs from the rest of the program.
type Deps struct {
	Logger       *slog.Logger
	Clock        func() time.Time
	Health       HealthFunc
	Auth         Authenticator
	MaxBodyBytes int64
}

// NewHandler returns the public HTTP handler with every route registered.
func NewHandler(d Deps) http.Handler {
	// Every /v1 route is wrapped in authed; only /healthz is public. Routes are
	// registered on one mux so the request log sees the full route pattern.
	authed := func(h http.Handler) http.Handler { return requireAuth(d.Logger, d.Auth, h) }

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", healthz(d.Logger, d.Health))
	mux.Handle("/v1/", authed(notFound()))
	mux.Handle("/", notFound())
	return obs.Middleware(d.Logger, d.Clock, limitBody(d.MaxBodyBytes, mux))
}

// limitBody caps every request body; handlers see an error past the limit.
func limitBody(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

func healthz(logger *slog.Logger, check HealthFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			if err := check(r.Context()); err != nil {
				logger.WarnContext(r.Context(), "health check failed", slog.String("error", err.Error()))
				writeError(w, http.StatusServiceUnavailable, "unavailable", "service is not healthy")
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func notFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	})
}
