package core

import (
	"bytes"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

var readAloud = message.Block{Type: "question", Text: "Please read this aloud.", Reply: message.ReplyVoice, Webhook: hook}

func TestRecordVoiceAnswer(t *testing.T) {
	r := newAnswerRig(t)
	m, d := r.ask(t, readAloud)
	audio := []byte("OggS fake opus")
	res := r.record(t, channel.Answer{Address: "777", ProviderMessageID: d.ProviderMessageID, Option: -1, Audio: audio, AudioType: "audio/ogg"})
	if res.Outcome != channel.AnswerSaved {
		t.Fatalf("voice note = %+v", res)
	}
	if r.hk.count() != 1 {
		t.Errorf("webhook calls = %v, want one", r.hk.calls)
	}
	as := r.take(t, m)
	if len(as) != 1 || as[0].Text != "" || !bytes.Equal(as[0].Audio, audio) || as[0].AudioType != "audio/ogg" {
		t.Fatalf("answers = %+v", as)
	}
	again := channel.Answer{Address: "777", DeliveryID: d.ID, Option: -1, Audio: []byte("second take"), AudioType: "audio/ogg"}
	if res := r.record(t, again); res.Outcome != channel.AnswerFinal {
		t.Errorf("voice note after the app fetched = %+v, want final", res)
	}
}

func TestRecordVoiceRejects(t *testing.T) {
	r := newAnswerRig(t)
	_, voice := r.ask(t, readAloud)
	_, typed := r.ask(t, message.Block{Type: "question", Text: "Name?"})
	audio := []byte("OggS")
	tests := []struct {
		name string
		in   channel.Answer
		want string
	}{
		{"text to a voice question", channel.Answer{Address: "777", DeliveryID: voice.ID, Option: -1, Text: "hi"}, channel.AnswerNeedVoice},
		{"voice note to a typed question", channel.Answer{Address: "777", DeliveryID: typed.ID, Option: -1, Audio: audio}, channel.AnswerNeedText},
		{"voice note from someone else", channel.Answer{Address: "666", DeliveryID: voice.ID, Option: -1, Audio: audio}, channel.AnswerUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if res := r.record(t, tt.in); res.Outcome != tt.want {
				t.Errorf("outcome = %q, want %q", res.Outcome, tt.want)
			}
		})
	}
	for _, d := range []string{voice.MessageID, typed.MessageID} {
		if as, err := r.st.TakeAnswers(t.Context(), d); err != nil || len(as) != 0 {
			t.Errorf("rejected answers stored: %+v, %v", as, err)
		}
	}
}

func TestRecordVoiceLogsNoAudio(t *testing.T) {
	r := newAnswerRig(t)
	_, d := r.ask(t, readAloud)
	r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: -1, Audio: []byte("SECRET-audio"), AudioType: "audio/ogg"})
	if logs := r.logs.String(); bytes.Contains([]byte(logs), []byte("SECRET")) {
		t.Errorf("logs contain the audio: %s", logs)
	}
}
