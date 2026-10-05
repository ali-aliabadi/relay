package core

import (
	"bytes"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/crypto"
	"github.com/ali-aliabadi/relay/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	return newStoreWithKey(t, 3)
}

// newStoreWithKey opens a store whose encryption key is keyByte repeated.
func newStoreWithKey(t *testing.T, keyByte byte) *store.Store {
	t.Helper()
	conn, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := store.Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	c, err := crypto.New(bytes.Repeat([]byte{keyByte}, crypto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return store.New(conn, c, time.Now)
}

func TestClientKeyLifecycle(t *testing.T) {
	svc := NewClients(newStore(t))
	ctx := t.Context()
	c, key, err := svc.Create(ctx, "backup-script")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "rk_") || len(key) != 3+43 {
		t.Errorf("key format = %q", key)
	}
	if bytes.Contains(c.APIKeyHash, []byte(key)) || len(c.APIKeyHash) != 32 {
		t.Error("stored hash is not a sha256 of the key")
	}
	_, key2, err := svc.Create(ctx, "other")
	if err != nil || key2 == key {
		t.Fatalf("second key = %q, %v", key2, err)
	}

	got, err := svc.Authenticate(ctx, key)
	if err != nil || got.ID != c.ID {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "rk_", "rk_wrong", key + "x", strings.TrimPrefix(key, "rk_"), "rk_" + strings.Repeat("a", 200)} {
		if _, err := svc.Authenticate(ctx, bad); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("Authenticate(%q) err = %v, want ErrUnauthorized", bad, err)
		}
	}
	if err := svc.Revoke(ctx, "backup-script"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, key); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("revoked key err = %v", err)
	}
	list, err := svc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Errorf("List = %d, %v", len(list), err)
	}

	// An active name can't be taken again; a revoked one gets a new key and
	// keeps its ID, and its old key stays dead.
	if _, _, err := svc.Create(ctx, "other"); !errors.Is(err, store.ErrConflict) {
		t.Errorf("Create(active name) err = %v, want ErrConflict", err)
	}
	again, key3, err := svc.Create(ctx, "backup-script")
	if err != nil || again.ID != c.ID || again.RevokedAt != nil || key3 == key {
		t.Fatalf("Create(revoked name) = %+v, %v", again, err)
	}
	if got, err := svc.Authenticate(ctx, key3); err != nil || got.ID != c.ID {
		t.Errorf("reissued key: %+v, %v", got, err)
	}
	if _, err := svc.Authenticate(ctx, key); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("old key after reissue err = %v", err)
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"ali", true},
		{"backup-script", true},
		{"a_1", true},
		{"9lives", true},
		{"", false},
		{"Ali", false},
		{"-ali", false},
		{"_ali", false},
		{"ali smith", false},
		{"ali@home", false},
		{"علی", false},
		{strings.Repeat("a", 33), false},
		{strings.Repeat("a", 32), true},
	}
	for _, tt := range tests {
		err := validateName(tt.name)
		if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrInvalid)) {
			t.Errorf("validateName(%q) = %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
}

func TestRecipients(t *testing.T) {
	svc := NewRecipients(newStore(t))
	ctx := t.Context()
	r, err := svc.Add(ctx, store.Recipient{Username: "ali", DisplayName: "Ali"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Timezone != "UTC" || r.ChannelPreference[0] != "telegram" {
		t.Errorf("defaults = %+v", r)
	}
	invalid := []store.Recipient{
		{Username: "Bad Name", DisplayName: "x"},
		{Username: "sara", DisplayName: ""},
		{Username: "sara", DisplayName: strings.Repeat("x", 65)},
		{Username: "sara", DisplayName: "Sara", Timezone: "Mars/Olympus"},
		{Username: "sara", DisplayName: "Sara", ChannelPreference: []string{"pigeon"}},
		{Username: "sara", DisplayName: "Sara", ChannelPreference: []string{"telegram", "telegram"}},
	}
	for _, rc := range invalid {
		if _, err := svc.Add(ctx, rc); !errors.Is(err, ErrInvalid) {
			t.Errorf("Add(%+v) err = %v, want ErrInvalid", rc, err)
		}
	}
	if _, err := svc.Add(ctx, store.Recipient{Username: "sara", DisplayName: "Sara", Timezone: "Asia/Tehran"}); err != nil {
		t.Fatalf("valid timezone rejected: %v", err)
	}

	r.DisplayName = "Ali A"
	if err := svc.Update(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.ChannelPreference = nil
	if err := svc.Update(ctx, r); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty preference err = %v", err)
	}
	got, err := svc.Get(ctx, "ali")
	if err != nil || got.DisplayName != "Ali A" {
		t.Errorf("Get = %+v, %v", got, err)
	}
	chans, err := svc.Channels(ctx, got.ID)
	if err != nil || len(chans) != 0 {
		t.Errorf("Channels = %v, %v", chans, err)
	}
	if err := svc.Remove(ctx, "ali"); err != nil {
		t.Fatal(err)
	}
	if list, err := svc.List(ctx); err != nil || len(list) != 1 {
		t.Errorf("List = %v, %v", list, err)
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
