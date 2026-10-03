package main

import (
	"strings"
	"testing"
)

// TestInviteByTelegramUsername: the admin invites @sara_tg, Sara taps Start,
// and she's linked with no code to copy; messages then reach her chat.
func TestInviteByTelegramUsername(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "sara", "--name", "Sara")
	_, logs := startServe(t, c)

	rc, out, errOut := c.run("recipients", "link", "sara", "@Sara_TG")
	if rc != 0 || out != "Invited sara as @Sara_TG. Ask Sara to open https://t.me/relay_test_bot and tap Start within 7 days.\n" {
		t.Fatalf("invite = %d %q %q", rc, out, errOut)
	}
	if _, list, _ := c.run("recipients", "list"); !strings.Contains(list, "telegram (invited)") {
		t.Errorf("list before Start = %s", list)
	}

	bot.AddStartFrom(424242, "someone_else") // a stranger gets a code, not Sara's link
	bot.AddStartFrom(717171, "sara_tg")
	var linked bool
	waitFor(t, func() bool {
		for _, m := range bot.Calls("sendMessage") {
			if m.Params["chat_id"] == float64(717171) && m.Params["text"] == "Linked to Relay as Sara. Notifications will arrive here." {
				linked = true
			}
		}
		return linked
	})
	_, list, _ := c.run("recipients", "list")
	if !strings.Contains(list, "telegram") || strings.Contains(list, "invited") || strings.Contains(list, "717171") {
		t.Errorf("list after Start = %s", list)
	}
	for _, m := range bot.Calls("sendMessage") {
		if m.Params["chat_id"] == float64(424242) && strings.Contains(m.Params["text"].(string), "Linked") {
			t.Errorf("the stranger was linked: %v", m.Params)
		}
	}

	if rc, out, _ := c.run("send", "--to", "sara", "--wait", "5s", "hi"); rc != 0 || !strings.Contains(out, "Status: delivered") {
		t.Fatalf("send = %d %q", rc, out)
	}
	sends := bot.Calls("sendMessage")
	if last := sends[len(sends)-1]; last.Params["chat_id"] != "717171" || last.Params["text"] != "hi" {
		t.Errorf("delivered = %v", last.Params)
	}

	l := logs.String()
	if !strings.Contains(l, "recipient linked by invite") {
		t.Errorf("no link log line: %s", l)
	}
	for _, leak := range []string{"sara_tg", "Sara_TG", "717171", "someone_else"} {
		if strings.Contains(l, leak) {
			t.Errorf("serve logs contain %q", leak)
		}
	}
}

func TestInviteErrors(t *testing.T) {
	c, _ := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	c.run("recipients", "add", "sara", "--name", "Sara")
	if rc, _, errOut := c.run("recipients", "link", "sara", "@shared_acct"); rc != 0 {
		t.Fatal(errOut)
	}
	for _, tt := range []struct {
		args []string
		err  string
	}{
		{[]string{"recipients", "link", "ali", "@no"}, "4-32 letters"},
		{[]string{"recipients", "link", "ali", "@bad-name"}, "4-32 letters"},
		{[]string{"recipients", "link", "nobody", "@valid_name"}, "not found"},
		{[]string{"recipients", "link", "ali", "@Shared_Acct"}, "already invited"},
	} {
		if rc, _, errOut := c.run(tt.args...); rc != 1 || !strings.Contains(errOut, tt.err) {
			t.Errorf("%v = %d %q", tt.args, rc, errOut)
		}
	}
}

func TestInviteWithoutToken(t *testing.T) {
	c := newCLI(t)
	c.run("recipients", "add", "sara", "--name", "Sara")
	if rc, out, _ := c.run("recipients", "link", "sara", "@sara_tg"); rc != 0 || !strings.Contains(out, "open the Relay bot and tap Start") {
		t.Errorf("invite = %d %q", rc, out)
	}
}
