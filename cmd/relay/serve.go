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
	"github.com/ali-aliabadi/relay/internal/obs"
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

	srv := &http.Server{
		Handler: api.NewHandler(api.Deps{
			Logger:       logger,
			Clock:        time.Now,
			Health:       st.Ping,
			MaxBodyBytes: cfg.MaxBodyBytes,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	logger.Info("relay starting", slog.String("version", version), slog.Any("config", cfg),
		slog.String("listen", ln.Addr().String()))

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
	logger.Info("relay stopped")
	return nil
}
