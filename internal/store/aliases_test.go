package store

import (
	"errors"
	"testing"
	"time"
)

func TestAliases(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	sara, err := f.s.CreateRecipient(ctx, Recipient{Username: "sara", DisplayName: "Sara"})
	if err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"admin": f.recipient.ID, "boss": f.recipient.ID, "co-admin": sara.ID} {
		if err := f.s.CreateAlias(ctx, name, id, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.s.CreateAlias(ctx, "admin", sara.ID, now); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate alias err = %v", err)
	}
	if err := f.s.CreateAlias(ctx, "ghost", "rcp_missing", now); err == nil {
		t.Error("alias for a missing recipient was stored")
	}
	if got, err := f.s.Aliases(ctx, f.recipient.ID); err != nil || len(got) != 2 || got[0] != "admin" || got[1] != "boss" {
		t.Errorf("Aliases = %v, %v", got, err)
	}
	if got, err := f.s.Aliases(ctx, "rcp_none"); err != nil || got == nil || len(got) != 0 {
		t.Errorf("no aliases = %#v, %v; want an empty slice", got, err)
	}

	found, err := f.s.RecipientsByNames(ctx, []string{"admin", "ali", "co-admin", "nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 || found["admin"].ID != f.recipient.ID || found["ali"].ID != f.recipient.ID ||
		found["co-admin"].Username != "sara" || found["co-admin"].DisplayName != "Sara" {
		t.Errorf("RecipientsByNames = %+v", found)
	}

	if err := f.s.DeleteAlias(ctx, "boss"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteAlias(ctx, "boss"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting a missing alias err = %v", err)
	}
	if err := f.s.DeleteRecipient(ctx, "sara"); err != nil {
		t.Fatal(err)
	}
	if found, _ := f.s.RecipientsByNames(ctx, []string{"co-admin", "boss"}); len(found) != 0 {
		t.Errorf("aliases outlived their recipient or deletion: %+v", found)
	}
}
