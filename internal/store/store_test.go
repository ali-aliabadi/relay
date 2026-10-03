package store

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/crypto"
)

// testClock advances one second per call so IDs and times are ordered.
func testClock() func() time.Time {
	t := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		t = t.Add(time.Second)
		return t
	}
}

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	ctx := t.Context()
	conn, err := Open(ctx, filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := Migrate(ctx, conn); err != nil {
		t.Fatal(err)
	}
	c, err := crypto.New(bytes.Repeat([]byte{9}, crypto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return New(conn, c, testClock()), conn
}

func TestOpenSetsPragmas(t *testing.T) {
	_, conn := newTestStore(t)
	ctx := t.Context()
	for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000"} {
		var got string
		if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s = %s, want %s", pragma, got, want)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	_, conn := newTestStore(t)
	n, err := Migrate(t.Context(), conn)
	if err != nil || n != 0 {
		t.Fatalf("second Migrate = %d, %v; want 0, nil", n, err)
	}
}

func TestOpenBadPath(t *testing.T) {
	if _, err := Open(t.Context(), filepath.Join(t.TempDir(), "missing", "dir", "relay.db")); err == nil {
		t.Fatal("want an error for a missing directory")
	}
}

func TestPing(t *testing.T) {
	s, conn := newTestStore(t)
	if err := s.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := s.Ping(t.Context()); err == nil {
		t.Fatal("ping on a closed db should fail")
	}
}

func TestClients(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	hash := bytes.Repeat([]byte{1}, 32)
	c, err := s.CreateClient(ctx, "backup-script", hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateClient(ctx, "backup-script", bytes.Repeat([]byte{2}, 32)); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name err = %v, want ErrConflict", err)
	}
	got, err := s.ClientByKeyHash(ctx, hash)
	if err != nil || got.ID != c.ID || !got.CreatedAt.Equal(c.CreatedAt) {
		t.Fatalf("ClientByKeyHash = %+v, %v", got, err)
	}
	if err := s.RevokeClient(ctx, "backup-script"); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeClient(ctx, "backup-script"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke err = %v", err)
	}
	if _, err := s.ClientByKeyHash(ctx, hash); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked client still authenticates: %v", err)
	}
	all, err := s.ListClients(ctx)
	if err != nil || len(all) != 1 || all[0].RevokedAt == nil {
		t.Errorf("ListClients = %+v, %v", all, err)
	}
}

func TestRecipientsAndContacts(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	r, err := s.CreateRecipient(ctx, Recipient{Username: "ali", DisplayName: "Ali"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Timezone != "UTC" || len(r.ChannelPreference) != 1 || r.ChannelPreference[0] != "telegram" {
		t.Errorf("defaults not applied: %+v", r)
	}
	if _, err := s.CreateRecipient(ctx, Recipient{Username: "ali", DisplayName: "Dup"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate username err = %v", err)
	}
	if _, err := s.CreateRecipient(ctx, Recipient{Username: "sara", DisplayName: "Sara"}); err != nil {
		t.Fatal(err)
	}

	r.DisplayName, r.Timezone, r.ChannelPreference = "Ali A", "Europe/Berlin", []string{"telegram", "sms"}
	if err := s.UpdateRecipient(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecipientByUsername(ctx, "ali")
	if err != nil || got.DisplayName != "Ali A" || got.Timezone != "Europe/Berlin" || len(got.ChannelPreference) != 2 {
		t.Fatalf("after update = %+v, %v", got, err)
	}
	some, err := s.RecipientsByUsernames(ctx, []string{"ali", "nobody", "sara"})
	if err != nil || len(some) != 2 {
		t.Errorf("RecipientsByUsernames = %+v, %v", some, err)
	}

	verified := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
	if err := s.UpsertContact(ctx, Contact{RecipientID: r.ID, Channel: "telegram", Address: "1000001", VerifiedAt: &verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertContact(ctx, Contact{RecipientID: r.ID, Channel: "telegram", Address: "1000002"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Contact(ctx, r.ID, "telegram")
	if err != nil || c.Address != "1000002" || c.VerifiedAt != nil {
		t.Errorf("Contact = %+v, %v", c, err)
	}
	if err := s.UpsertContact(ctx, Contact{RecipientID: "rcp_missing", Channel: "telegram", Address: "1"}); err == nil {
		t.Error("contact for a missing recipient accepted (foreign keys off?)")
	}

	if err := s.DeleteRecipient(ctx, "ali"); err != nil {
		t.Fatal(err)
	}
	if cs, err := s.ContactsForRecipient(ctx, r.ID); err != nil || len(cs) != 0 {
		t.Errorf("contacts not cascaded: %+v, %v", cs, err)
	}
	if err := s.DeleteRecipient(ctx, "ali"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete err = %v", err)
	}
	if err := s.UpdateRecipient(ctx, r); !errors.Is(err, ErrNotFound) {
		t.Errorf("update of deleted err = %v", err)
	}
	if _, err := s.RecipientByUsername(ctx, "ali"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get of deleted err = %v", err)
	}
	list, err := s.ListRecipients(ctx)
	if err != nil || len(list) != 1 || list[0].Username != "sara" {
		t.Errorf("ListRecipients = %+v, %v", list, err)
	}
}
