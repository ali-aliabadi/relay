package store

import (
	"bytes"
	"testing"
	"time"
)

var secretPicture = []byte("\x89PNG secret picture")

func TestImageAnswerLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if ok, err := f.s.SaveImageAnswer(ctx, m.ID, f.recipient.ID, "image/png", secretPicture); !ok || err != nil {
		t.Fatalf("SaveImageAnswer = %v, %v", ok, err)
	}
	for col, raw := range rawValues(t, f.conn, "answers") {
		if bytes.Contains(raw, []byte("secret picture")) {
			t.Errorf("answers.%s holds the plaintext picture", col)
		}
	}
	got, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("TakeAnswers = %v, %v", got, err)
	}
	if a := got[0]; !bytes.Equal(a.Image, secretPicture) || a.ImageType != "image/png" || a.Text != "" || a.Audio != nil {
		t.Errorf("answer = %+v", a)
	}
	if ok, err := f.s.SaveImageAnswer(ctx, m.ID, f.recipient.ID, "image/png", []byte("later")); ok || err != nil {
		t.Errorf("replacing a fetched picture = %v, %v; want false (final)", ok, err)
	}
}

func TestImageAnswerReplacesVoiceAnswer(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if _, err := f.s.SaveVoiceAnswer(ctx, m.ID, f.recipient.ID, "audio/ogg", secretAudio); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SaveImageAnswer(ctx, m.ID, f.recipient.ID, "image/png", secretPicture); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 1 || got[0].Audio != nil || got[0].AudioType != "" || !bytes.Equal(got[0].Image, secretPicture) {
		t.Fatalf("answers = %+v, %v; the old audio must be gone", got, err)
	}
}

func TestImageIsBoundToItsRow(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m, other := f.create(t, ""), f.create(t, "")
	for _, id := range []string{m.ID, other.ID} {
		if _, err := f.s.SaveImageAnswer(ctx, id, f.recipient.ID, "image/png", secretPicture); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.conn.ExecContext(ctx,
		`UPDATE answers SET image = (SELECT image FROM answers WHERE message_id = ?) WHERE message_id = ?`, m.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TakeAnswers(ctx, other.ID); err == nil {
		t.Error("swapped picture ciphertext decrypted")
	}
}

func TestPurgeDeletesImageAnswers(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if _, err := f.s.SaveImageAnswer(ctx, m.ID, f.recipient.ID, "image/png", secretPicture); err != nil {
		t.Fatal(err)
	}
	if res, err := f.s.Purge(ctx, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}); err != nil || res.AnswersDeleted != 1 {
		t.Fatalf("purge = %+v, %v", res, err)
	}
	for col, raw := range rawValues(t, f.conn, "answers") {
		t.Errorf("answers.%s still has %d bytes after purge", col, len(raw))
	}
}
