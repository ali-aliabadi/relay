package store

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestInvitesStore(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	sara, err := f.s.CreateRecipient(ctx, Recipient{Username: "sara", DisplayName: "Sara"})
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for _, inv := range []Invite{
		{RecipientID: f.recipient.ID, Channel: "telegram", Handle: "secret_handle_ali", CreatedAt: day},
		{RecipientID: sara.ID, Channel: "telegram", Handle: "secret_handle_sara", CreatedAt: day.Add(time.Hour)},
		{RecipientID: sara.ID, Channel: "sms", Handle: "+100", CreatedAt: day},
	} {
		if err := f.s.UpsertInvite(ctx, inv); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.s.Invites(ctx, "telegram", day)
	if err != nil || len(got) != 2 || got[0].Handle != "secret_handle_ali" || got[1].Handle != "secret_handle_sara" || !got[1].CreatedAt.Equal(day.Add(time.Hour)) {
		t.Fatalf("Invites = %+v, %v", got, err)
	}
	if got, _ := f.s.Invites(ctx, "telegram", day.Add(time.Minute)); len(got) != 1 || got[0].RecipientID != sara.ID {
		t.Errorf("since filter = %+v", got)
	}
	for col, raw := range rawValues(t, f.conn, "invites") {
		if bytes.Contains(raw, []byte("secret_handle")) {
			t.Errorf("invites.%s holds plaintext", col)
		}
	}

	// Upsert replaces the handle and time.
	if err := f.s.UpsertInvite(ctx, Invite{RecipientID: f.recipient.ID, Channel: "telegram", Handle: "new_handle", CreatedAt: day.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.s.Invites(ctx, "telegram", day.Add(90*time.Minute)); len(got) != 1 || got[0].Handle != "new_handle" {
		t.Errorf("after upsert = %+v", got)
	}

	if n, err := f.s.DeleteInvitesBefore(ctx, day.Add(90*time.Minute)); err != nil || n != 2 {
		t.Errorf("DeleteInvitesBefore = %d, %v; want sara's telegram and sms invites", n, err)
	}
	if err := f.s.DeleteInvite(ctx, f.recipient.ID, "telegram"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteInvite(ctx, f.recipient.ID, "telegram"); err != nil {
		t.Errorf("deleting a missing invite = %v", err)
	}
	if got, _ := f.s.Invites(ctx, "telegram", time.Time{}); len(got) != 0 {
		t.Errorf("left = %+v", got)
	}
}

func TestInviteCiphertextIsBoundToRow(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	sara, _ := f.s.CreateRecipient(ctx, Recipient{Username: "sara", DisplayName: "Sara"})
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{f.recipient.ID, sara.ID} {
		if err := f.s.UpsertInvite(ctx, Invite{RecipientID: id, Channel: "telegram", Handle: "h_" + id, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	// Copying ali's invite onto sara's row must not decrypt as sara's.
	if _, err := f.conn.ExecContext(ctx, `UPDATE invites SET handle = (SELECT handle FROM invites WHERE recipient_id = ?) WHERE recipient_id = ?`,
		f.recipient.ID, sara.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Invites(ctx, "telegram", time.Time{}); err == nil {
		t.Error("swapped invite ciphertext decrypted")
	}
}

func TestInviteLogValueHidesHandle(t *testing.T) {
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", slog.Any("i", Invite{RecipientID: "rcp_1", Channel: "telegram", Handle: "secret_handle"}))
	if strings.Contains(buf.String(), "secret") || !strings.Contains(buf.String(), "rcp_1") {
		t.Errorf("log = %s", buf.String())
	}
}

func TestRecipientByID(t *testing.T) {
	f := newFixture(t)
	if r, err := f.s.RecipientByID(t.Context(), f.recipient.ID); err != nil || r.Username != "ali" {
		t.Errorf("RecipientByID = %+v, %v", r, err)
	}
	if _, err := f.s.RecipientByID(t.Context(), "rcp_missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing = %v", err)
	}
}
