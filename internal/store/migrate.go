package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies every pending migration. It runs on startup, so a deploy is
// just a restart; it returns how many migrations were applied.
func Migrate(ctx context.Context, conn *sql.DB) (int, error) {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return 0, fmt.Errorf("loading migrations: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, conn, fsys)
	if err != nil {
		return 0, fmt.Errorf("creating migration provider: %w", err)
	}
	results, err := p.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("applying migrations: %w", err)
	}
	return len(results), nil
}
