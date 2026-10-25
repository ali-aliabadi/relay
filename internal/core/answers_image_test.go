package core

import (
	"bytes"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

var drawThis = message.Block{Type: "question", Text: "Please make this picture.", Reply: message.ReplyImage, Webhook: hook}

func TestRecordImageAnswer(t *testing.T) {
	r := newAnswerRig(t)
	m, d := r.ask(t, drawThis)
	png := []byte("\x89PNG picture")
	res := r.record(t, channel.Answer{Address: "777", ProviderMessageID: d.ProviderMessageID, Option: -1, Image: png, ImageType: "image/png"})
	if res.Outcome != channel.AnswerSaved {
		t.Fatalf("picture = %+v", res)
	}
	if r.hk.count() != 1 {
		t.Errorf("webhook calls = %v, want one", r.hk.calls)
	}
	as := r.take(t, m)
	if len(as) != 1 || as[0].Text != "" || as[0].Audio != nil || !bytes.Equal(as[0].Image, png) || as[0].ImageType != "image/png" {
		t.Fatalf("answers = %+v", as)
	}
}

func TestRecordImageRejects(t *testing.T) {
	r := newAnswerRig(t)
	_, image := r.ask(t, drawThis)
	_, voice := r.ask(t, readAloud)
	_, typed := r.ask(t, message.Block{Type: "question", Text: "Name?"})
	_, buttons := r.ask(t, message.Block{Type: "question", Text: "Ok?", Options: []string{"Yes"}})
	png := []byte("\x89PNG")
	tests := []struct {
		name string
		in   channel.Answer
		want string
	}{
		{"text to an image question", channel.Answer{Address: "777", DeliveryID: image.ID, Option: -1, Text: "hi"}, channel.AnswerNeedImage},
		{"voice note to an image question", channel.Answer{Address: "777", DeliveryID: image.ID, Option: -1, Audio: []byte("OggS")}, channel.AnswerNeedImage},
		{"picture to a voice question", channel.Answer{Address: "777", DeliveryID: voice.ID, Option: -1, Image: png}, channel.AnswerNeedVoice},
		{"picture to a typed question", channel.Answer{Address: "777", DeliveryID: typed.ID, Option: -1, Image: png}, channel.AnswerNeedText},
		{"picture to a question with buttons", channel.Answer{Address: "777", DeliveryID: buttons.ID, Option: -1, Image: png}, channel.AnswerNeedOption},
		{"picture from someone else", channel.Answer{Address: "666", DeliveryID: image.ID, Option: -1, Image: png}, channel.AnswerUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if res := r.record(t, tt.in); res.Outcome != tt.want {
				t.Errorf("outcome = %q, want %q", res.Outcome, tt.want)
			}
		})
	}
	for _, d := range []string{image.MessageID, voice.MessageID, typed.MessageID, buttons.MessageID} {
		if as, err := r.st.TakeAnswers(t.Context(), d); err != nil || len(as) != 0 {
			t.Errorf("rejected answers stored: %+v, %v", as, err)
		}
	}
}

func TestRecordImageLogsNoPicture(t *testing.T) {
	r := newAnswerRig(t)
	_, d := r.ask(t, drawThis)
	r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: -1, Image: []byte("SECRET-picture"), ImageType: "image/png"})
	if logs := r.logs.String(); bytes.Contains([]byte(logs), []byte("SECRET")) {
		t.Errorf("logs contain the picture: %s", logs)
	}
}
