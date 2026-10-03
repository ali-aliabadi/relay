package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

// Authenticator resolves an API key to its client.
type Authenticator interface {
	Authenticate(ctx context.Context, key string) (store.Client, error)
}

type clientCtxKey struct{}

// clientFrom returns the authenticated client. Only valid behind requireAuth.
func clientFrom(ctx context.Context) store.Client {
	c, _ := ctx.Value(clientCtxKey{}).(store.Client)
	return c
}

// requireAuth rejects requests without a valid "Authorization: Bearer <key>".
// The key itself is never logged. An IP with too many recent failures gets
// 429 without its key being checked.
func requireAuth(logger *slog.Logger, auth Authenticator, lim *failLimiter, trustXFF bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, trustXFF)
		if wait := lim.blocked(ip); wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many failed authentication attempts")
			return
		}
		key, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			lim.fail(ip)
			unauthorized(w)
			return
		}
		client, err := auth.Authenticate(r.Context(), key)
		if errors.Is(err, core.ErrUnauthorized) {
			lim.fail(ip)
			unauthorized(w)
			return
		}
		if err != nil {
			logger.ErrorContext(r.Context(), "authenticating request", slog.String("error", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		obs.SetClient(r.Context(), client.ID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientCtxKey{}, client)))
	})
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="relay"`)
	writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
}
