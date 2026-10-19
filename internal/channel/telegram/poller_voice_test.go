package telegram

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

func TestPollerVoiceReplyIsDownloaded(t *testing.T) {
	r := startPoller(t, nil)
	audio := []byte("OggS fake opus")
	r.bot.AddVoiceReply(42, 7, audio)
	sends := r.await(t, "sendMessage", 1)
	a := r.got()
	if len(a) != 1 || !bytes.Equal(a[0].Audio, audio) || a[0].AudioType != "audio/ogg" ||
		a[0].ProviderMessageID != "7" || a[0].Address != "42" || a[0].Option != -1 || a[0].Text != "" {
		t.Fatalf("answers = %+v", a)
	}
	if got := sends[0].Params["text"]; got != "✅ Answer saved." {
		t.Errorf("confirmation = %v", got)
	}
	if n := len(r.bot.Calls("getFile")); n != 1 {
		t.Errorf("getFile calls = %d", n)
	}
	if strings.Contains(r.logs.String(), "OggS") || strings.Contains(r.logs.String(), testToken) {
		t.Errorf("logs contain audio or the token: %s", r.logs.String())
	}
}

func TestPollerVoiceReplyTooLarge(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddVoiceReply(42, 7, make([]byte, message.MaxVoiceBytes+1))
	if got := r.await(t, "sendMessage", 1)[0].Params["text"]; got != tooLong {
		t.Errorf("feedback = %v", got)
	}
	if len(r.got()) != 0 || len(r.bot.Calls("getFile")) != 0 {
		t.Errorf("answers %v, getFile %d: an oversized note must not be fetched", r.got(), len(r.bot.Calls("getFile")))
	}
}

func TestPollerVoiceWithoutReplyGetsHint(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddVoiceReply(42, 0, []byte("x"))
	if got := r.await(t, "sendMessage", 1)[0].Params["text"]; got != replyHint {
		t.Errorf("reply = %v", got)
	}
	if len(r.got()) != 0 || len(r.bot.Calls("getFile")) != 0 {
		t.Errorf("a voice note without a reply was fetched or recorded")
	}
}

func TestPollerNeedVoiceFeedback(t *testing.T) {
	r := startPoller(t, nil)
	r.set(channel.AnswerResult{Outcome: channel.AnswerNeedVoice}, nil)
	r.bot.AddReply(42, 7, "typed instead")
	if got := r.await(t, "sendMessage", 1)[0].Params["text"].(string); !strings.Contains(got, "voice note") {
		t.Errorf("feedback = %q", got)
	}
}

func TestVoiceContentType(t *testing.T) {
	for in, want := range map[string]string{
		"":                                  "audio/ogg",
		"audio/mpeg":                        "audio/mpeg",
		"audio/ogg; codecs=opus":            "audio/ogg",
		"text/html":                         "audio/ogg",
		"audio/<script>":                    "audio/ogg",
		"audio/" + strings.Repeat("x", 300): "audio/ogg",
	} {
		if got := (&voiceFile{MimeType: in}).contentType(); got != want {
			t.Errorf("contentType(%q) = %q, want %q", in, got, want)
		}
	}
}
