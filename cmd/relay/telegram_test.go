package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http/httptest"
	"path/filepath"
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

func runWithInput(c cli, stdin io.Reader, args ...string) (int, string, string) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(c.t.Context(), args, c.env, stdin, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
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

func TestRecipientsLink(t *testing.T) {
	c, bot := newTelegramCLI(t)
	if code, _, errOut := c.run("recipients", "add", "ali", "--name", "Ali"); code != 0 {
		t.Fatal(errOut)
	}
	bot.AddStart(111, "Old", "old_update") // before the command: must be skipped

	type res struct {
		code     int
		out, err string
	}
	done := make(chan res, 1)
	stdinR, stdinW := io.Pipe()
	go func() {
		code, out, errOut := runWithInput(c, stdinR, "recipients", "link", "ali")
		done <- res{code, out, errOut}
	}()
	waitFor(t, func() bool { return len(bot.Calls("getUpdates")) >= 2 })
	bot.AddStart(424242, "Stranger", "someone_else")
	bot.AddStart(515151, "Ali", "ali_real")
	_, _ = io.WriteString(stdinW, "n\ny\n")

	r := <-done
	if r.code != 0 || !strings.Contains(r.out, "Linked ali to Telegram") || !strings.Contains(r.out, "@relay_test_bot") {
		t.Fatalf("link = %d %q %q", r.code, r.out, r.err)
	}
	if strings.Contains(r.out, "424242") || strings.Contains(r.out, "515151") {
		t.Errorf("chat id printed: %s", r.out)
	}
	if strings.Contains(r.out, "Old") {
		t.Errorf("stale update was offered: %s", r.out)
	}
	sends := bot.Calls("sendMessage")
	if len(sends) != 1 || sends[0].Params["chat_id"] != float64(515151) {
		t.Errorf("confirmation sends = %+v", sends)
	}
	_, list, _ := c.run("recipients", "list")
	if !strings.Contains(list, "telegram  ") && !strings.Contains(list, "telegram\n") {
		t.Errorf("recipient not shown as linked: %s", list)
	}
}

func TestLinkNeedsToken(t *testing.T) {
	c := newCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	if code, _, errOut := c.run("recipients", "link", "ali"); code != 1 || !strings.Contains(errOut, "RELAY_TELEGRAM_BOT_TOKEN") {
		t.Errorf("= %d %q", code, errOut)
	}
}

// TestSendThroughServe is the end-to-end path in-process: `relay send`
// queues, the worker inside `relay serve` delivers to the fake Bot API.
func TestSendThroughServe(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	go func() {
		waitFor(t, func() bool { return len(bot.Calls("getUpdates")) >= 2 })
		bot.AddStart(515151, "Ali", "ali")
	}()
	if code, out, errOut := runWithInput(c, strings.NewReader("y\n"), "recipients", "link", "ali"); code != 0 {
		t.Fatalf("link = %d %q %q", code, out, errOut)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, c.env, &syncBuffer{}, ln) }()
	defer func() { cancel(); <-served }()

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
