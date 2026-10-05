package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel/telegram/telegramtest"
)

var codeKeyRe = regexp.MustCompile(`<code>(rk_[A-Za-z0-9_-]+)</code>`)

// awaitKey waits for the next key message to chat and returns the key.
func awaitKey(t *testing.T, bot *telegramtest.Server, chat float64, seen int) string {
	t.Helper()
	var key string
	waitFor(t, func() bool {
		n := 0
		for _, m := range bot.Calls("sendMessage") {
			if m.Params["chat_id"] == chat && m.Params["parse_mode"] == "HTML" {
				if n++; n > seen {
					key = codeKeyRe.FindStringSubmatch(m.Params["text"].(string))[1]
				}
			}
		}
		return key != ""
	})
	return key
}

// TestBotKeys: the admin makes, lists, revokes and replaces an app's API key
// from Telegram, and the key works against the API.
func TestBotKeys(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c = withAdmin(c, "admin")
	c.run("recipients", "add", "ali", "--name", "Ali")
	c.run("recipients", "alias", "ali", "admin")
	base, logs := startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 4242)); rc != 0 {
		t.Fatal(errOut)
	}
	status := func(key string) int {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/v1/messages/msg_none", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Someone else gets the usual hint and no key.
	bot.AddText(5555, "/newkey evil", "mallory")
	if got := awaitTo(t, bot, 5555, "To answer"); strings.Contains(got, "key") {
		t.Errorf("stranger got %q", got)
	}

	bot.AddText(4242, "/newkey Photo-Sync", "")
	key := awaitKey(t, bot, 4242, 0)
	if sends := bot.Calls("sendMessage"); !strings.Contains(sends[len(sends)-2].Params["text"].(string), "RELAY_APP=photo-sync") {
		t.Errorf("text before the key = %q", sends[len(sends)-2].Params["text"])
	}
	if got := status(key); got != http.StatusNotFound {
		t.Errorf("new key: %d, want 404 (authenticated, no such message)", got)
	}

	bot.AddText(4242, "/newkey photo-sync", "")
	awaitTo(t, bot, 4242, "photo-sync already has a key. To replace it, send /revoke photo-sync")
	bot.AddText(4242, "/keys", "")
	if got := awaitTo(t, bot, 4242, "Apps with a working key"); !strings.Contains(got, "• photo-sync") || strings.Contains(got, "rk_") {
		t.Errorf("/keys = %q", got)
	}

	bot.AddText(4242, "/revoke photo-sync", "")
	awaitTo(t, bot, 4242, "photo-sync's key no longer works")
	if got := status(key); got != http.StatusUnauthorized {
		t.Errorf("revoked key: %d, want 401", got)
	}
	bot.AddText(4242, "/keys", "")
	awaitTo(t, bot, 4242, "Revoked:\n• photo-sync")

	bot.AddText(4242, "/newkey photo-sync", "")
	key2 := awaitKey(t, bot, 4242, 1)
	if key2 == key || status(key2) != http.StatusNotFound || status(key) != http.StatusUnauthorized {
		t.Errorf("replacement key: new %d, old %d", status(key2), status(key))
	}
	_, list, _ := c.run("clients", "list")
	if strings.Count(list, "photo-sync") != 1 || strings.Contains(list, "revoked") {
		t.Errorf("clients list = %s", list)
	}

	l := logs.String()
	if strings.Count(l, "api key created") != 2 || !strings.Contains(l, "api key revoked") {
		t.Errorf("missing log lines: %s", l)
	}
	for _, leak := range []string{key, key2, "4242", "5555"} {
		if strings.Contains(l, leak) {
			t.Errorf("serve logs contain %q", leak)
		}
	}
}

func TestBotKeysProblems(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c = withAdmin(c, "ali")
	c.run("recipients", "add", "ali", "--name", "Ali")
	startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 4242)); rc != 0 {
		t.Fatal(errOut)
	}
	for _, tt := range []struct{ cmd, want string }{
		{"/newkey", "Use: /newkey <app>"},
		{"/revoke", "Use: /newkey <app>"},
		{"/keys extra", "Use: /newkey <app>"},
		{"/newkey bad!name", "name may only use"},
		{"/revoke ghost", "No app called ghost has an active key"},
		{"/keys", "No apps have keys yet"},
	} {
		bot.AddText(4242, tt.cmd, "")
		awaitTo(t, bot, 4242, tt.want)
	}
	if n := len(bot.Calls("deleteMessage")); n != 0 {
		t.Errorf("deleteMessage called %d times with no key sent", n)
	}
}

func TestBotKeysOffWithoutAdmin(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 4242)); rc != 0 {
		t.Fatal(errOut)
	}
	bot.AddText(4242, "/newkey app", "")
	awaitTo(t, bot, 4242, "To answer")
	if _, list, _ := c.run("clients", "list"); strings.Contains(list, "app") {
		t.Errorf("key made with no admin configured: %s", list)
	}
}
