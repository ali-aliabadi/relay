package telegram

import (
	"strings"
	"testing"
)

func commandNames(params map[string]any) []string {
	var out []string
	for _, c := range params["commands"].([]any) {
		out = append(out, c.(map[string]any)["command"].(string))
	}
	return out
}

// With no admin linked, only everyone's menu is set at start.
func TestPollerMenuWithoutAdmin(t *testing.T) {
	r := startPoller(t, nil)
	set := r.await(t, "setMyCommands", 1)[0].Params
	if set["scope"] != nil || strings.Join(commandNames(set), ",") != "start,help" {
		t.Errorf("menu = %v", set)
	}
	r.bot.AddText(7, "/help", "")
	if text := r.await(t, "sendMessage", 1)[0].Params["text"]; text != userHelp {
		t.Errorf("/help = %q", text)
	}
	for _, c := range r.bot.Calls("setMyCommands") {
		if c.Params["scope"] != nil {
			t.Errorf("admin menu set with no admin: %v", c.Params)
		}
	}
}

// Once the admin is linked, /help refreshes the menu: the admin commands go
// to the admin's chat only, and only the admin's /help lists them.
func TestPollerMenuAndHelpForAdmin(t *testing.T) {
	r := startPoller(t, nil)
	r.await(t, "setMyCommands", 1)
	r.with(func() { r.admin = "42" })

	r.bot.AddText(7, "/help", "mallory")
	if text := r.await(t, "sendMessage", 1)[0].Params["text"].(string); text != userHelp {
		t.Errorf("stranger's /help = %q", text)
	}
	r.bot.AddText(42, "/help", "")
	text := r.await(t, "sendMessage", 2)[1].Params["text"].(string)
	for _, want := range []string{"/invite", "/newkey", "/keys", "/revoke", "/start"} {
		if !strings.Contains(text, want) {
			t.Errorf("admin /help lacks %s: %q", want, text)
		}
	}
	var adminMenus int
	for _, c := range r.bot.Calls("setMyCommands") {
		scope, _ := c.Params["scope"].(map[string]any)
		switch {
		case scope == nil && strings.Join(commandNames(c.Params), ",") != "start,help":
			t.Errorf("everyone's menu = %v", c.Params)
		case scope != nil:
			adminMenus++
			if scope["type"] != "chat" || scope["chat_id"] != float64(42) || len(commandNames(c.Params)) != 6 {
				t.Errorf("admin menu = %v", c.Params)
			}
		}
	}
	if adminMenus == 0 {
		t.Error("admin menu never set")
	}
}
