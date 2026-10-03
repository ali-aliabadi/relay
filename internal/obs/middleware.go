package obs

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// requestInfo is filled in by inner handlers (for example the auth middleware
// sets the client ID) and read by the request log line.
type requestInfo struct {
	clientID string
}

// SetClient records the authenticated client ID for the request log line.
func SetClient(ctx context.Context, clientID string) {
	if info, ok := ctx.Value(requestInfoKey).(*requestInfo); ok {
		info.clientID = clientID
	}
}

// Middleware assigns a request ID (accepting a well-formed X-Request-ID),
// returns it in the response header and writes one info line per request.
// The line holds only metadata: never the path's raw values, query or body.
func Middleware(logger *slog.Logger, clock func() time.Time, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := clock()
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			id = NewRequestID()
		}
		info := &requestInfo{}
		ctx := context.WithValue(WithRequestID(r.Context(), id), requestInfoKey, info)
		r = r.WithContext(ctx)
		w.Header().Set(RequestIDHeader, id)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := r.Pattern // set by http.ServeMux on this same *Request
		if route == "" {
			route = "unmatched"
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "request",
			slog.String("client", info.clientID),
			slog.String("method", r.Method),
			slog.String("route", route),
			slog.Int("status", rec.status),
			slog.Duration("duration", clock().Sub(start)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
