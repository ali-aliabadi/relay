// Package store is the only package that talks SQL. It wraps the sqlc-generated
// queries in internal/store/db and encrypts private columns on the way in and
// decrypts them on the way out, so callers only ever see plaintext domain types.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/ali-aliabadi/relay/internal/crypto"
	"github.com/ali-aliabadi/relay/internal/store/db"
)

var (
	// ErrNotFound means the row doesn't exist (or isn't visible to the caller).
	ErrNotFound = errors.New("not found")
	// ErrConflict means a unique constraint (username, client name, idempotency key) was hit.
	ErrConflict = errors.New("already exists")
)

// Open opens (creating if needed) the SQLite database at path with WAL,
// foreign keys and a busy timeout. Transactions take the write lock up front
// (BEGIN IMMEDIATE) so concurrent writers wait instead of failing.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)",
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
	} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	conn, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("connecting to sqlite: %w", err)
	}
	return conn, nil
}

// Store reads and writes Relay's data.
type Store struct {
	db     *sql.DB
	q      *db.Queries
	cipher *crypto.Cipher
	clock  func() time.Time
}

// New returns a Store over an opened, migrated database.
func New(conn *sql.DB, cipher *crypto.Cipher, clock func() time.Time) *Store {
	return &Store{db: conn, q: db.New(conn), cipher: cipher, clock: clock}
}

// Ping checks that the database answers. Used by /healthz.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging sqlite: %w", err)
	}
	return nil
}

// inTx runs fn in a transaction, committing on nil and rolling back otherwise.
func (s *Store) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	if err := fn(s.q.WithTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// mapErr turns driver errors into the store's sentinel errors.
func mapErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return ErrConflict
		}
	}
	return err
}
