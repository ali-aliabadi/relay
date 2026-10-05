package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel/telegram/telegramtest"
)

var inviteLinkRe = regexp.MustCompile(`https://t\.me/relay_test_bot\?start=([A-Za-z0-9_-]+)`)

// withAdmin returns c with RELAY_ADMIN_RECIPIENT set to admin.
func withAdmin(c cli, admin string) cli {
	prev := c.env
	c.env = func(k string) (string, bool) {
		if k == "RELAY_ADMIN_RECIPIENT" {
			return admin, true
		}
		return prev(k)
	}
	return c
}

// lastTo returns the text of the newest message the bot sent to chat.
func lastTo(bot *telegramtest.Server, chat float64) string {
	sends := bot.Calls("sendMessage")
	for i := len(sends) - 1; i >= 0; i-- {
		if sends[i].Params["chat_id"] == chat {
			return sends[i].Params["text"].(string)
		}
	}
	return ""
}

// awaitTo waits for a message to chat containing want and returns it.
func awaitTo(t *testing.T, bot *telegramtest.Server, chat float64, want string) string {
	t.Helper()
	waitFor(t, func() bool { return strings.Contains(lastTo(bot, chat), want) })
	return lastTo(bot, chat)
}

// TestBotInviteLink: the admin sends /invite in Telegram, forwards the link,
// the invitee taps Start and is linked, and the admin hears about it. No
// terminal involved after the admin's own first link.
func TestBotInviteLink(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c = withAdmin(c, "admin")
	c.run("recipients", "add", "ali", "--name", "Ali", "--timezone", "Europe/Berlin")
	c.run("recipients", "alias", "ali", "admin")
	_, logs := startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 4242)); rc != 0 {
		t.Fatal(errOut)
	}

	// Someone else can't invite, and learns nothing about the command.
	bot.AddText(5555, "/invite eve Eve", "mallory")
	if got := awaitTo(t, bot, 5555, "To answer"); strings.Contains(got, "invite") {
		t.Errorf("stranger got %q", got)
	}

	bot.AddText(4242, "/invite Sara Sara Aliabadi", "")
	reply := awaitTo(t, bot, 4242, "t.me")
	m := inviteLinkRe.FindStringSubmatch(reply)
	if m == nil || !strings.Contains(reply, "Sara Aliabadi") || !strings.Contains(reply, "24 hours") {
		t.Fatalf("invite reply = %q", reply)
	}
	token := m[1]

	bot.AddText(717171, "/start "+token, "")
	awaitTo(t, bot, 717171, "Linked to Relay as Sara Aliabadi.")
	awaitTo(t, bot, 4242, "Sara Aliabadi opened your invite")

	// The link works once.
	bot.AddText(818181, "/start "+token, "")
	awaitTo(t, bot, 818181, "expired or was already used")

	_, list, _ := c.run("recipients", "list")
	if !strings.Contains(list, "sara") || !strings.Contains(list, "Europe/Berlin") || strings.Contains(list, "eve") {
		t.Errorf("list = %s", list)
	}
	if rc, out, _ := c.run("send", "--to", "sara", "--wait", "5s", "hi"); rc != 0 || !strings.Contains(out, "Status: delivered") {
		t.Fatalf("send = %d %q", rc, out)
	}
	sends := bot.Calls("sendMessage")
	if last := sends[len(sends)-1]; last.Params["chat_id"] != "717171" || last.Params["text"] != "hi" {
		t.Errorf("delivered = %v", last.Params)
	}

	l := logs.String()
	if !strings.Contains(l, "invite link created") || !strings.Contains(l, "recipient linked by invite link") {
		t.Errorf("missing log lines: %s", l)
	}
	for _, leak := range []string{token, "717171", "4242", "Sara Aliabadi"} {
		if strings.Contains(l, leak) {
			t.Errorf("serve logs contain %q", leak)
		}
	}
}

func TestBotInviteProblems(t *testing.T) {
	c, bot := newTelegramCLI(t)
	admin := withAdmin(c, "ali")
	admin.run("recipients", "add", "ali", "--name", "Ali")
	admin.run("recipients", "alias", "ali", "boss")
	startServe(t, admin)
	if rc, _, errOut := admin.run("recipients", "link", "ali", linkCode(t, bot, 4242)); rc != 0 {
		t.Fatal(errOut)
	}
	for _, tt := range []struct{ cmd, want string }{
		{"/invite", "say who to invite"},
		{"/invite bad!name", "name may only use"},
		{"/invite sara " + strings.Repeat("x", 65), "display name must be"},
	} {
		bot.AddText(4242, tt.cmd, "")
		if got := awaitTo(t, bot, 4242, tt.want); !strings.Contains(got, "/invite <username>") {
			t.Errorf("%q: reply = %q", tt.cmd, got)
		}
	}
	// An alias re-invites the person it names rather than adding someone.
	bot.AddText(4242, "/invite boss", "")
	awaitTo(t, bot, 4242, "Invite for Ali is ready")
}

func TestBotInviteOffWithoutAdmin(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 4242)); rc != 0 {
		t.Fatal(errOut)
	}
	bot.AddText(4242, "/invite sara", "")
	awaitTo(t, bot, 4242, "To answer")
	if _, list, _ := c.run("recipients", "list"); strings.Contains(list, "sara") {
		t.Errorf("invite ran with no admin configured: %s", list)
	}
}
