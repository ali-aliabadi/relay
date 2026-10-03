package store

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const secretAnswer = "the new password hint is blue"

func (f fixture) delivery(t *testing.T, m Message) Delivery {
	t.Helper()
	ds, err := f.s.Deliveries(t.Context(), m.ID)
	if err != nil || len(ds) != 1 {
		t.Fatalf("deliveries = %v, %v", ds, err)
	}
	return ds[0]
}

func TestAnswerLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")

	got, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("TakeAnswers before any answer = %v, %v", got, err)
	}
	if ok, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, "first"); !ok || err != nil {
		t.Fatalf("SaveAnswer = %v, %v", ok, err)
	}
	if ok, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, secretAnswer); !ok || err != nil {
		t.Fatalf("replacing an unfetched answer = %v, %v", ok, err)
	}
	got, err = f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("TakeAnswers = %v, %v", got, err)
	}
	a := got[0]
	if a.Text != secretAnswer || a.Username != "ali" || a.RecipientID != f.recipient.ID || a.MessageID != m.ID || a.AnsweredAt.IsZero() {
		t.Errorf("answer = %+v", a)
	}
	if ok, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, "too late"); ok || err != nil {
		t.Fatalf("SaveAnswer after fetch = %v, %v; want false (final)", ok, err)
	}
	again, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(again) != 1 || again[0].Text != secretAnswer {
		t.Errorf("a fetched answer must stay readable and unchanged: %+v, %v", again, err)
	}
}

func TestAnswersArePerRecipient(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	sara, err := f.s.CreateRecipient(ctx, Recipient{Username: "sara", DisplayName: "Sara"})
	if err != nil {
		t.Fatal(err)
	}
	m := f.create(t, "")
	if _, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	// Sara answers after the app fetched ali's: hers is still new and saved.
	if ok, err := f.s.SaveAnswer(ctx, m.ID, sara.ID, "no"); !ok || err != nil {
		t.Fatalf("second recipient's answer = %v, %v", ok, err)
	}
	got, err = f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 2 || got[0].Username != "ali" || got[1].Username != "sara" || got[1].Text != "no" {
		t.Errorf("answers = %+v, %v", got, err)
	}
}

func TestSaveAnswerNeedsRealRows(t *testing.T) {
	f := newFixture(t)
	m := f.create(t, "")
	if _, err := f.s.SaveAnswer(t.Context(), "msg_missing", f.recipient.ID, "x"); err == nil {
		t.Error("answer for a missing message was stored")
	}
	if _, err := f.s.SaveAnswer(t.Context(), m.ID, "rcp_missing", "x"); err == nil {
		t.Error("answer for a missing recipient was stored")
	}
}

func TestAnswerIsEncryptedAndBoundToItsRow(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	other := f.create(t, "")
	if _, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, secretAnswer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SaveAnswer(ctx, other.ID, f.recipient.ID, "other"); err != nil {
		t.Fatal(err)
	}
	for col, raw := range rawValues(t, f.conn, "answers") {
		if bytes.Contains(raw, []byte("password hint")) {
			t.Errorf("answers.%s holds plaintext", col)
		}
	}
	// Moving one row's ciphertext onto another must fail to decrypt.
	if _, err := f.conn.ExecContext(ctx,
		`UPDATE answers SET answer = (SELECT answer FROM answers WHERE message_id = ?) WHERE message_id = ?`, m.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TakeAnswers(ctx, other.ID); err == nil {
		t.Error("swapped answer ciphertext decrypted")
	}
}

func TestPurgeDeletesAnswers(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if _, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, secretAnswer); err != nil {
		t.Fatal(err)
	}
	res, err := f.s.Purge(ctx, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), time.Time{})
	if err != nil || res.AnswersDeleted != 0 {
		t.Fatalf("purge before cutoff = %+v, %v", res, err)
	}
	res, err = f.s.Purge(ctx, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil || res.AnswersDeleted != 1 || res.Redacted != 1 {
		t.Fatalf("purge = %+v, %v", res, err)
	}
	got, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 0 {
		t.Errorf("answers after purge = %v, %v", got, err)
	}
}

func TestAnswersGoWithRecipientAndMessage(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if _, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRecipient(ctx, "ali"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.conn.QueryRowContext(ctx, "SELECT count(*) FROM answers").Scan(&n); err != nil || n != 0 {
		t.Errorf("answers after recipient delete = %d, %v", n, err)
	}
}

func TestDeliveryLookups(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	d := f.delivery(t, m)
	got, err := f.s.Delivery(ctx, d.ID)
	if err != nil || got.ID != d.ID || got.RecipientID != f.recipient.ID {
		t.Fatalf("Delivery = %+v, %v", got, err)
	}
	if _, err := f.s.Delivery(ctx, "dlv_missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing delivery err = %v", err)
	}
	if ds, err := f.s.DeliveriesByProviderID(ctx, "telegram", "55"); err != nil || len(ds) != 0 {
		t.Errorf("before delivery = %v, %v", ds, err)
	}
	if err := f.s.MarkDelivered(ctx, d.ID, "55"); err != nil {
		t.Fatal(err)
	}
	// Telegram message IDs are per chat: another chat's delivery can share one.
	m2 := f.create(t, "")
	if err := f.s.MarkDelivered(ctx, f.delivery(t, m2).ID, "55"); err != nil {
		t.Fatal(err)
	}
	ds, err := f.s.DeliveriesByProviderID(ctx, "telegram", "55")
	if err != nil || len(ds) != 2 {
		t.Errorf("by provider id = %v, %v", ds, err)
	}
	if ds, _ := f.s.DeliveriesByProviderID(ctx, "sms", "55"); len(ds) != 0 {
		t.Errorf("other channel matched: %v", ds)
	}
	if ds, _ := f.s.DeliveriesByProviderID(ctx, "telegram", ""); len(ds) != 0 {
		t.Errorf("empty provider id matched undelivered rows: %v", ds)
	}
}

func TestMessageByID(t *testing.T) {
	f := newFixture(t)
	m := f.create(t, "")
	got, err := f.s.MessageByID(t.Context(), m.ID)
	if err != nil || got.Title != secretTitle || got.ClientID != f.clientID {
		t.Fatalf("MessageByID = %+v, %v", got, err)
	}
	if _, err := f.s.MessageByID(t.Context(), "msg_missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v", err)
	}
}

func TestTokens(t *testing.T) {
	f := newFixture(t)
	tok, err := f.s.SealToken("purpose-a", []byte(secretAddress))
	if err != nil || strings.Contains(tok, secretAddress) || strings.ContainsAny(tok, "+/=") {
		t.Fatalf("SealToken = %q, %v", tok, err)
	}
	if got, err := f.s.OpenToken("purpose-a", tok); err != nil || string(got) != secretAddress {
		t.Fatalf("OpenToken = %q, %v", got, err)
	}
	if _, err := f.s.OpenToken("purpose-b", tok); err == nil {
		t.Error("token opened for another purpose")
	}
	flipped := []byte(tok)
	flipped[len(flipped)/2] ^= 1
	for _, bad := range []string{"", "!!!", tok[:10], string(flipped)} {
		if _, err := f.s.OpenToken("purpose-a", bad); err == nil {
			t.Errorf("OpenToken(%q) succeeded", bad)
		}
	}
}

func TestAnswerLogValueHidesText(t *testing.T) {
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", slog.Any("a", Answer{MessageID: "msg_1", Text: secretAnswer, Username: "ali"}))
	if strings.Contains(buf.String(), "password") || !strings.Contains(buf.String(), "msg_1") {
		t.Errorf("log = %s", buf.String())
	}
}
