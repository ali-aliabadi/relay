package main

import (
	"strings"
	"testing"
)

func TestRecipientsAliasCommands(t *testing.T) {
	c := newCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	c.run("recipients", "add", "tara", "--name", "Tara")

	for _, tt := range []struct {
		args []string
		out  string
	}{
		{[]string{"recipients", "alias", "ali", "admin"}, "admin now also reaches ali.\n"},
		{[]string{"recipients", "alias", "ali", "boss"}, "boss now also reaches ali.\n"},
		{[]string{"recipients", "alias", "tara", "co-admin"}, "co-admin now also reaches tara.\n"},
		{[]string{"recipients", "unalias", "boss"}, "Removed alias boss.\n"},
	} {
		if rc, out, errOut := c.run(tt.args...); rc != 0 || out != tt.out {
			t.Errorf("%v = %d %q %q", tt.args, rc, out, errOut)
		}
	}
	_, list, _ := c.run("recipients", "list")
	lines := strings.Split(list, "\n")
	if !strings.HasPrefix(lines[0], "USERNAME  ALIASES") ||
		!strings.HasPrefix(lines[1], "ali") || !strings.Contains(lines[1], "admin") || strings.Contains(lines[1], "boss") ||
		!strings.HasPrefix(lines[2], "tara") || !strings.Contains(lines[2], "co-admin") {
		t.Errorf("list =\n%s", list)
	}

	for _, tt := range []struct {
		args []string
		code int
		err  string
	}{
		{[]string{"recipients", "alias", "tara", "admin"}, 1, "already a recipient's username or alias"},
		{[]string{"recipients", "alias", "ali", "tara"}, 1, "already a recipient's username or alias"},
		{[]string{"recipients", "alias", "nobody", "x"}, 1, "not found"},
		{[]string{"recipients", "alias", "ali", "Bad Name"}, 1, "name may only use"},
		{[]string{"recipients", "add", "admin", "--name", "X"}, 1, "already a recipient's username or alias"},
		{[]string{"recipients", "unalias", "boss"}, 1, "not found"},
		{[]string{"recipients", "alias", "ali"}, 2, "Usage"},
		{[]string{"recipients", "alias", "ali", "a", "b"}, 2, "Usage"},
		{[]string{"recipients", "unalias"}, 2, "Usage"},
		{[]string{"recipients", "unalias", "a", "b"}, 2, "Usage"},
	} {
		if rc, _, errOut := c.run(tt.args...); rc != tt.code || !strings.Contains(errOut, tt.err) {
			t.Errorf("%v = %d %q", tt.args, rc, errOut)
		}
	}
	// Removing a person removes their aliases, freeing the names.
	c.run("recipients", "remove", "tara")
	if rc, _, errOut := c.run("recipients", "alias", "ali", "co-admin"); rc != 0 {
		t.Errorf("reusing a removed person's alias = %d %q", rc, errOut)
	}
}

// TestSendToAliasThroughServe: `relay send --to admin` reaches ali's chat once,
// even when ali is also named.
func TestSendToAliasThroughServe(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 515151)); rc != 0 {
		t.Fatal(errOut)
	}
	c.run("recipients", "alias", "ali", "admin")
	before := len(bot.Calls("sendMessage"))
	if rc, out, _ := c.run("send", "--to", "admin,ali", "--wait", "5s", "hi admin"); rc != 0 || !strings.Contains(out, "Status: delivered") {
		t.Fatalf("send = %d %q", rc, out)
	}
	sends := bot.Calls("sendMessage")[before:]
	if len(sends) != 1 || sends[0].Params["chat_id"] != "515151" || sends[0].Params["text"] != "hi admin" {
		t.Errorf("sends = %+v; want exactly one to ali's chat", sends)
	}
}
