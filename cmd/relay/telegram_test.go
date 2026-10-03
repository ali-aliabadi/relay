package main

import (
	"context"
	"net"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel/telegram/telegramtest"
)

const fakeToken = "999:fake-cli-token"

func newTelegramCLI(t *testing.T) (cli, *telegramtest.Server) {
	t.Helper()
	bot := telegramtest.New(fakeToken)
	srv := httptest.NewServer(bot)
	t.Cleanup(srv.Close)
	return cli{t: t, env: lookup(map[string]string{
		"RELAY_ENCRYPTION_KEY": testKey, "RELAY_DB_PATH": filepath.Join(t.TempDir(), "relay.db"),
		"RELAY_TELEGRAM_BOT_TOKEN": fakeToken, "RELAY_TELEGRAM_API_URL": srv.URL,
		"RELAY_WORKER_POLL_INTERVAL": "20ms",
	})}, bot
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// startServe runs `relay serve` in-process and returns its base URL.
func startServe(t *testing.T, c cli) (string, *syncBuffer) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, c.env, logs, ln) }()
	t.Cleanup(func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return "http://" + ln.Addr().String(), logs
}

var codeRe = regexp.MustCompile(`relay recipients link <your username> (\S+)`)

// linkCode sends /start from chat and returns the code the bot replies with.
func linkCode(t *testing.T, bot *telegramtest.Server, chat int64) string {
	t.Helper()
	before := len(bot.Calls("sendMessage"))
	bot.AddStart(chat)
	var code string
	waitFor(t, func() bool {
		for _, m := range bot.Calls("sendMessage")[before:] {
			if text, _ := m.Params["text"].(string); m.Params["chat_id"] == float64(chat) && codeRe.MatchString(text) {
				code = codeRe.FindStringSubmatch(text)[1]
				return true
			}
		}
		return false
	})
	return code
}

func TestRecipientsLink(t *testing.T) {
	c, bot := newTelegramCLI(t)
	if code, _, errOut := c.run("recipients", "add", "ali", "--name", "Ali"); code != 0 {
		t.Fatal(errOut)
	}
	startServe(t, c)
	code := linkCode(t, bot, 515151)
	if strings.Contains(code, "515151") {
		t.Errorf("link code shows the chat id: %s", code)
	}

	rc, out, errOut := c.run("recipients", "link", "ali", code)
	if rc != 0 || out != "Linked ali to telegram.\n" || errOut != "" {
		t.Fatalf("link = %d %q %q", rc, out, errOut)
	}
	sends := bot.Calls("sendMessage")
	last := sends[len(sends)-1]
	if last.Params["chat_id"] != float64(515151) || !strings.Contains(last.Params["text"].(string), "Notifications for Ali") {
		t.Errorf("confirmation = %+v", last.Params)
	}
	if _, list, _ := c.run("recipients", "list"); !strings.Contains(list, "telegram") || strings.Contains(list, "515151") {
		t.Errorf("list = %s", list)
	}

	for _, tt := range []struct {
		args []string
		code int
		err  string
	}{
		{[]string{"recipients", "link", "ali", "not-a-code"}, 1, "invalid or expired"},
		{[]string{"recipients", "link", "nobody", code}, 1, "not found"},
		{[]string{"recipients", "link", "ali"}, 2, "Usage"},
		{[]string{"recipients", "link"}, 2, "Usage"},
		{[]string{"recipients", "link", "ali", code, "extra"}, 2, "Usage"},
	} {
		if rc, _, errOut := c.run(tt.args...); rc != tt.code || !strings.Contains(errOut, tt.err) {
			t.Errorf("%v = %d %q", tt.args, rc, errOut)
		}
	}
}

// TestLinkWithoutToken: the code alone proves the chat, so linking works on a
// machine without the bot token; it just can't send the confirmation.
func TestLinkWithoutToken(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	startServe(t, c)
	code := linkCode(t, bot, 616161)
	sent := len(bot.Calls("sendMessage"))

	noToken := cli{t: t, env: func(k string) (string, bool) {
		if k == "RELAY_TELEGRAM_BOT_TOKEN" {
			return "", false
		}
		return c.env(k)
	}}
	if rc, out, errOut := noToken.run("recipients", "link", "ali", code); rc != 0 || !strings.Contains(out, "Linked ali") {
		t.Fatalf("link = %d %q %q", rc, out, errOut)
	}
	if len(bot.Calls("sendMessage")) != sent {
		t.Error("sent a confirmation without a token")
	}
}

// TestSendThroughServe is the end-to-end path in-process: `relay send`
// queues, the worker inside `relay serve` delivers to the fake Bot API.
func TestSendThroughServe(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	startServe(t, c)
	if code, out, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 515151)); code != 0 {
		t.Fatalf("link = %d %q %q", code, out, errOut)
	}

	code, out, errOut := c.run("send", "--to", "ali", "--title", "Hi", "--wait", "5s", "hello", "from", "cli")
	if code != 0 || !strings.Contains(out, "Status: delivered") {
		t.Fatalf("send = %d %q %q", code, out, errOut)
	}
	msgs := bot.Calls("sendMessage")
	last := msgs[len(msgs)-1]
	if last.Params["chat_id"] != "515151" || last.Params["text"] != "<b>Hi</b>\n\nhello from cli" {
		t.Errorf("sent = %+v", last.Params)
	}

	bot.FailNext(telegramtest.Failure{Status: 403, Desc: "Forbidden: bot was blocked by the user"})
	code, out, _ = c.run("send", "--to", "ali", "--wait", "5s", "again")
	if code != 1 || !strings.Contains(out, "Status: failed") || !strings.Contains(out, "bot was blocked") {
		t.Errorf("failed send = %d %q", code, out)
	}
	for _, args := range [][]string{{"send"}, {"send", "--to", "ali"}, {"send", "hello"}} {
		if code, _, _ := c.run(args...); code != 2 {
			t.Errorf("%v = %d, want usage", args, code)
		}
	}
}
