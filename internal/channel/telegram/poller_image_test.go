package telegram

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

var (
	pngData  = []byte("\x89PNG\r\n\x1a\nsecret picture")
	jpegData = []byte("\xff\xd8\xff\xe0secret photo")
)

func TestPollerPhotoReplyTakesTheLargestSize(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddPhotoReply(42, 7, []byte("\xff\xd8\xff thumbnail"), jpegData)
	sends := r.await(t, "sendMessage", 1)
	a := r.got()
	if len(a) != 1 || !bytes.Equal(a[0].Image, jpegData) || a[0].ImageType != "image/jpeg" || a[0].Audio != nil ||
		a[0].ProviderMessageID != "7" || a[0].Address != "42" || a[0].Option != -1 || a[0].Text != "" {
		t.Fatalf("answers = %+v", a)
	}
	if got := sends[0].Params["text"]; got != "✅ Answer saved." {
		t.Errorf("confirmation = %v", got)
	}
	if n := len(r.bot.Calls("getFile")); n != 1 {
		t.Errorf("getFile calls = %d, want only the largest size", n)
	}
	if strings.Contains(r.logs.String(), "secret") || strings.Contains(r.logs.String(), testToken) {
		t.Errorf("logs contain the picture or the token: %s", r.logs.String())
	}
}

func TestPollerDocumentReply(t *testing.T) {
	tests := []struct {
		name, mime string
		data       []byte
		wantType   string
	}{
		{"png", "image/png", pngData, "image/png"},
		{"bytes decide, not the sender's type", "application/pdf", pngData, "image/png"},
		{"not a picture", "image/png", []byte("%PDF-1.7 secret"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := startPoller(t, nil)
			r.bot.AddDocumentReply(42, 7, tt.data, tt.mime)
			r.await(t, "sendMessage", 1)
			a := r.got()
			if len(a) != 1 || a[0].ImageType != tt.wantType || (tt.wantType == "") != (a[0].Image == nil) {
				t.Fatalf("answers = %+v", a)
			}
		})
	}
}

func TestPollerPhotoTooLarge(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddDocumentReply(42, 7, make([]byte, message.MaxAnswerFileBytes+1), "image/png")
	if got := r.await(t, "sendMessage", 1)[0].Params["text"]; got != tooLong {
		t.Errorf("feedback = %v", got)
	}
	if len(r.got()) != 0 || len(r.bot.Calls("getFile")) != 0 {
		t.Errorf("an oversized picture was fetched or recorded")
	}
}

func TestPollerNeedImageFeedback(t *testing.T) {
	r := startPoller(t, nil)
	r.set(channel.AnswerResult{Outcome: channel.AnswerNeedImage}, nil)
	r.bot.AddReply(42, 7, "typed instead")
	if got := r.await(t, "sendMessage", 1)[0].Params["text"].(string); !strings.Contains(got, "picture") {
		t.Errorf("feedback = %q", got)
	}
}

func TestImageType(t *testing.T) {
	for name, tt := range map[string]struct {
		data []byte
		want string
	}{
		"png":   {pngData, "image/png"},
		"jpeg":  {jpegData, "image/jpeg"},
		"webp":  {[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "image/webp"},
		"gif":   {[]byte("GIF89a"), ""},
		"html":  {[]byte("<html><img>"), ""},
		"empty": {nil, ""},
	} {
		if got := imageType(tt.data); got != tt.want {
			t.Errorf("%s: imageType = %q, want %q", name, got, tt.want)
		}
	}
}
