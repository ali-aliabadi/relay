package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ali-aliabadi/relay/internal/api"
	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

// shutdownTimeout bounds how long in-flight requests get after SIGTERM.
// Docker's default stop grace period is 10s, so stay under it.
const shutdownTimeout = 8 * time.Second

// serve runs the HTTP server until ctx is cancelled, then shuts down gracefully.
// ln may be nil, in which case serve listens on the configured address.
func serve(ctx context.Context, lookup config.LookupFunc, logOut io.Writer, ln net.Listener) error {
	cfg, err := config.Load(lookup)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	logger := obs.NewLogger(logOut, cfg.LogLevel)

	st, conn, err := openStore(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if ln == nil {
		var lc net.ListenConfig
		ln, err = lc.Listen(ctx, "tcp", cfg.Addr)
		if err != nil {
			return fmt.Errorf("listening on %s: %w", cfg.Addr, err)
		}
	}

	channels := buildChannels(cfg)
	metrics := obs.NewMetrics(ctx, st.QueueStats, time.Now)
	msgs := core.NewMessages(st, channels)
	msgs.OnCreated = metrics.MessageAccepted
	srv := newHTTPServer(ctx, cfg, logger, st, msgs)

	logger.Info("relay starting", slog.String("version", version), slog.Any("config", cfg),
		slog.String("listen", ln.Addr().String()))

	bg, err := startBackground(ctx, cfg, logger, st, channels, metrics)
	if err != nil {
		return err
	}
	httpErr := runHTTP(ctx, logger, srv, ln)
	// Stop background work after the API: no new messages arrive, and the
	// delivery in flight (if any) finishes first.
	if err := bg.stop(ctx); err != nil && httpErr == nil {
		return err
	}
	if httpErr == nil {
		logger.Info("relay stopped")
	}
	return httpErr
}

// newHTTPServer builds the public API server with its timeouts.
func newHTTPServer(ctx context.Context, cfg config.Config, logger *slog.Logger, st *store.Store, msgs *core.Messages) *http.Server {
	return &http.Server{
		Handler: api.NewHandler(api.Deps{
			Logger:            logger,
			Clock:             time.Now,
			Health:            st.Ping,
			Auth:              core.NewClients(st),
			Recipients:        core.NewRecipients(st),
			Messages:          msgs,
			MaxBodyBytes:      cfg.MaxBodyBytes,
			TrustForwardedFor: cfg.TrustForwardedFor,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
}

// runHTTP serves until ctx is cancelled, then drains in-flight requests.
func runHTTP(ctx context.Context, logger *slog.Logger, srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return fmt.Errorf("serving http: %w", err)
	case <-ctx.Done():
	}

	logger.Info("relay shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down http server: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving http: %w", err)
	}
	return nil
}
