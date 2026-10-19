package store

import (
	"bytes"
	"testing"
	"time"
)

var secretAudio = []byte("OggS secret voice note")

func TestVoiceAnswerLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if ok, err := f.s.SaveVoiceAnswer(ctx, m.ID, f.recipient.ID, "audio/ogg", secretAudio); !ok || err != nil {
		t.Fatalf("SaveVoiceAnswer = %v, %v", ok, err)
	}
	for col, raw := range rawValues(t, f.conn, "answers") {
		if bytes.Contains(raw, []byte("secret voice")) {
			t.Errorf("answers.%s holds plaintext audio", col)
		}
	}
	got, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("TakeAnswers = %v, %v", got, err)
	}
	if a := got[0]; !bytes.Equal(a.Audio, secretAudio) || a.AudioType != "audio/ogg" || a.Text != "" {
		t.Errorf("answer = %+v", a)
	}
	if ok, err := f.s.SaveVoiceAnswer(ctx, m.ID, f.recipient.ID, "audio/ogg", []byte("later")); ok || err != nil {
		t.Errorf("replacing a fetched voice answer = %v, %v; want false (final)", ok, err)
	}
}

func TestTextAnswerReplacesVoiceAnswer(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if _, err := f.s.SaveVoiceAnswer(ctx, m.ID, f.recipient.ID, "audio/ogg", secretAudio); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SaveAnswer(ctx, m.ID, f.recipient.ID, "typed"); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.TakeAnswers(ctx, m.ID)
	if err != nil || len(got) != 1 || got[0].Text != "typed" || got[0].Audio != nil || got[0].AudioType != "" {
		t.Fatalf("answers = %+v, %v; the old audio must be gone", got, err)
	}
}

func TestVoiceAudioIsBoundToItsRow(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m, other := f.create(t, ""), f.create(t, "")
	for _, id := range []string{m.ID, other.ID} {
		if _, err := f.s.SaveVoiceAnswer(ctx, id, f.recipient.ID, "audio/ogg", secretAudio); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.conn.ExecContext(ctx,
		`UPDATE answers SET audio = (SELECT audio FROM answers WHERE message_id = ?) WHERE message_id = ?`, m.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TakeAnswers(ctx, other.ID); err == nil {
		t.Error("swapped audio ciphertext decrypted")
	}
}

func TestPurgeDeletesVoiceAnswers(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	m := f.create(t, "")
	if _, err := f.s.SaveVoiceAnswer(ctx, m.ID, f.recipient.ID, "audio/ogg", secretAudio); err != nil {
		t.Fatal(err)
	}
	if res, err := f.s.Purge(ctx, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{}); err != nil || res.AnswersDeleted != 1 {
		t.Fatalf("purge = %+v, %v", res, err)
	}
	for col, raw := range rawValues(t, f.conn, "answers") {
		t.Errorf("answers.%s still has %d bytes after purge", col, len(raw))
	}
}
