package telegram

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel/telegram/telegramtest"
	"github.com/ali-aliabadi/relay/internal/obs"
)

func TestPollerNewKeySendsKeyThenDeletesIt(t *testing.T) {
	r := startPoller(t, nil)
	r.with(func() { r.keys = KeyReply{Admin: true, Text: "New key for app.", Key: "rk_SECRET<&>"} })
	r.bot.AddText(42, "/newkey@relay_test_bot app", "")
	sends := r.await(t, "sendMessage", 2)
	if sends[0].Params["text"] != "New key for app." || sends[0].Params["parse_mode"] != nil {
		t.Errorf("first reply = %v", sends[0].Params)
	}
	key := sends[1]
	if key.Params["parse_mode"] != "HTML" || key.Params["chat_id"] != float64(42) ||
		!strings.Contains(key.Params["text"].(string), "<code>rk_SECRET&lt;&amp;&gt;</code>") ||
		!strings.Contains(key.Params["text"].(string), "deletes itself in 0 seconds") {
		t.Errorf("key message = %v", key.Params)
	}
	del := r.await(t, "deleteMessage", 1)[0]
	if del.Params["chat_id"] != float64(42) || del.Params["message_id"] != float64(key.MessageID) {
		t.Errorf("deleteMessage = %v, want message %d", del.Params, key.MessageID)
	}
	r.with(func() {
		if len(r.keyCmds) != 1 || r.keyCmds[0] != "42 /newkey app" {
			t.Errorf("key commands = %v", r.keyCmds)
		}
	})
	if strings.Contains(r.logs.String(), "SECRET") {
		t.Errorf("key in logs: %s", r.logs.String())
	}
}

func TestPollerKeysAndRevokeSendTextOnly(t *testing.T) {
	r := startPoller(t, nil)
	r.with(func() { r.keys = KeyReply{Admin: true, Text: "Apps with a working key: app"} })
	r.bot.AddText(42, "/keys", "")
	r.bot.AddText(42, "/revoke app", "")
	r.await(t, "sendMessage", 2)
	time.Sleep(100 * time.Millisecond) // longer than KeyTTL: nothing to delete
	if n := len(r.bot.Calls("deleteMessage")); n != 0 {
		t.Errorf("deleteMessage called %d times", n)
	}
	r.with(func() {
		if len(r.keyCmds) != 2 || r.keyCmds[0] != "42 /keys " || r.keyCmds[1] != "42 /revoke app" {
			t.Errorf("key commands = %v", r.keyCmds)
		}
	})
}

func TestPollerKeysFromNonAdminGetsUsualHint(t *testing.T) {
	r := startPoller(t, nil) // Keys answers Admin: false
	r.bot.AddText(7, "/newkey app", "mallory")
	if text := r.await(t, "sendMessage", 1)[0].Params["text"]; text != replyHint {
		t.Errorf("reply = %q", text)
	}
}

func TestPollerKeysError(t *testing.T) {
	r := startPoller(t, nil)
	r.set(r.result, errors.New("db down"))
	r.bot.AddText(42, "/keys", "")
	if text := r.await(t, "sendMessage", 1)[0].Params["text"]; text != "Something went wrong, please try again." {
		t.Errorf("reply = %q", text)
	}
}

func TestTTLText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		time.Minute: "1 minute", 2 * time.Minute: "2 minutes", 30 * time.Second: "30 seconds", 90 * time.Second: "90 seconds",
	} {
		if got := ttlText(d); got != want {
			t.Errorf("ttlText(%v) = %q, want %q", d, got, want)
		}
	}
}

// A key message is deleted when Relay stops, not left in the chat until the
// next start.
func TestPollerDeletesKeyOnShutdown(t *testing.T) {
	bot := telegramtest.New(testToken)
	srv := httptest.NewServer(bot)
	t.Cleanup(srv.Close)
	p := &Poller{
		Client: NewClient(srv.URL, obs.Secret(testToken), srv.Client()),
		Keys: func(context.Context, string, string, string) (KeyReply, error) {
			return KeyReply{Admin: true, Text: "New key.", Key: "rk_x"}, nil
		},
		KeyTTL: time.Hour,
		Logger: obs.NewLogger(io.Discard, slog.LevelInfo),
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	bot.AddText(42, "/newkey app", "")
	deadline := time.Now().Add(5 * time.Second)
	for len(bot.Calls("sendMessage")) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("no key message")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := len(bot.Calls("deleteMessage")); n != 0 {
		t.Fatalf("deleted before the hour was up: %d", n)
	}
	cancel()
	<-done // Run waits for the delete
	if n := len(bot.Calls("deleteMessage")); n != 1 {
		t.Errorf("deleteMessage called %d times after shutdown, want 1", n)
	}
}
