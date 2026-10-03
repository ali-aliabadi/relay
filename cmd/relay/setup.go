package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/crypto"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

// openStore opens the database, applies pending migrations and returns a
// Store using the configured encryption key. The caller closes conn.
func openStore(ctx context.Context, cfg config.Config, logger *slog.Logger) (*store.Store, *sql.DB, error) {
	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("loading encryption key: %w", err)
	}
	conn, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	applied, err := store.Migrate(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if applied > 0 {
		logger.Info("migrations applied", slog.Int("count", applied))
	}
	return store.New(conn, cipher, time.Now), conn, nil
}

// migrate applies pending migrations and exits.
func migrate(ctx context.Context, lookup config.LookupFunc, logOut io.Writer) error {
	cfg, err := config.Load(lookup)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	_, conn, err := openStore(ctx, cfg, obs.NewLogger(logOut, cfg.LogLevel))
	if err != nil {
		return err
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("closing database: %w", err)
	}
	return nil
}
