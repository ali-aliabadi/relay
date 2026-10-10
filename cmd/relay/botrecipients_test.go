package main

import (
	"strings"
	"testing"
)

// TestBotRecipients: the admin lists recipients from Telegram with their
// aliases and link state, never a chat ID; anyone else gets the usual hint.
func TestBotRecipients(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c = withAdmin(c, "admin")
	c.run("recipients", "add", "ali", "--name", "Ali")
	c.run("recipients", "alias", "ali", "admin")
	c.run("recipients", "add", "sara", "--name", "Sara")
	startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 4242)); rc != 0 {
		t.Fatal(errOut)
	}

	bot.AddText(5555, "/recipients", "mallory")
	if got := awaitTo(t, bot, 5555, "To answer"); strings.Contains(got, "ali") {
		t.Errorf("stranger got %q", got)
	}

	bot.AddText(4242, "/recipients", "")
	got := awaitTo(t, bot, 4242, "Recipients apps can send to")
	for _, want := range []string{"• ali (Ali), also admin: linked on telegram", "• sara (Sara): not linked yet"} {
		if !strings.Contains(got, want) {
			t.Errorf("/recipients lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, "4242") {
		t.Errorf("/recipients shows a chat ID: %q", got)
	}
}
