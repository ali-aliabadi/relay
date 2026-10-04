package telegram

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
)

func TestSplitCommand(t *testing.T) {
	tests := map[string][2]string{
		"/invite sara Sara Smith":  {"/invite", "sara Sara Smith"},
		"/invite@relay_bot  sara ": {"/invite", "sara"},
		"/start":                   {"/start", ""},
		"hello /invite":            {"", ""},
		"/start abc_DEF-123":       {"/start", "abc_DEF-123"},
		"/invite@relay_bot":        {"/invite", ""},
	}
	for in, want := range tests {
		cmd, args := splitCommand(in)
		if cmd != want[0] || args != want[1] {
			t.Errorf("splitCommand(%q) = %q, %q; want %q, %q", in, cmd, args, want[0], want[1])
		}
	}
}

func TestPollerInviteSendsLinkToAdmin(t *testing.T) {
	r := startPoller(t, nil)
	r.with(func() { r.invite = InviteReply{Admin: true, Name: "Sara", Token: "TOKEN123", ValidFor: 24 * time.Hour} })
	r.bot.AddText(42, "/invite@relay_test_bot sara Sara", "")
	text := r.await(t, "sendMessage", 1)[0].Params["text"].(string)
	if !strings.Contains(text, "https://t.me/relay_test_bot?start=TOKEN123") || !strings.Contains(text, "Sara") ||
		!strings.Contains(text, "24 hours") {
		t.Errorf("reply = %q", text)
	}
	r.with(func() {
		if r.invites[0] != "42 sara Sara" {
			t.Errorf("invites = %v", r.invites)
		}
	})
	// The bot's username is looked up once.
	r.bot.AddText(42, "/invite sara", "")
	r.await(t, "sendMessage", 2)
	if n := len(r.bot.Calls("getMe")); n != 1 {
		t.Errorf("getMe called %d times", n)
	}
	if strings.Contains(r.logs.String(), "TOKEN123") {
		t.Errorf("token in logs: %s", r.logs.String())
	}
}

func TestPollerInviteFromNonAdminGetsUsualHint(t *testing.T) {
	r := startPoller(t, nil) // Invite answers Admin: false
	r.bot.AddText(7, "/invite sara", "mallory")
	text := r.await(t, "sendMessage", 1)[0].Params["text"].(string)
	if text != replyHint {
		t.Errorf("reply = %q", text)
	}
	if len(r.bot.Calls("getMe")) != 0 {
		t.Error("non-admin made the bot look itself up")
	}
}

func TestPollerInviteProblemShowsUsage(t *testing.T) {
	r := startPoller(t, nil)
	r.with(func() { r.invite = InviteReply{Admin: true, Problem: "name must be 1-32 characters"} })
	r.bot.AddText(42, "/invite", "")
	text := r.await(t, "sendMessage", 1)[0].Params["text"].(string)
	if !strings.Contains(text, "name must be 1-32 characters") || !strings.Contains(text, inviteUsage) {
		t.Errorf("reply = %q", text)
	}
}

func TestPollerInviteLinkLinksAndTellsAdmin(t *testing.T) {
	r := startPoller(t, nil)
	r.with(func() { r.claimed = Claimed{OK: true, Name: "Sara", AdminChat: "42"} })
	r.bot.AddText(99, "/start TOKEN123", "")
	sends := r.await(t, "sendMessage", 2)
	if sends[0].Params["chat_id"] != float64(99) || !strings.Contains(sends[0].Params["text"].(string), "Linked to Relay as Sara") {
		t.Errorf("to invitee: %v", sends[0].Params)
	}
	if sends[1].Params["chat_id"] != float64(42) || !strings.Contains(sends[1].Params["text"].(string), "Sara opened your invite") {
		t.Errorf("to admin: %v", sends[1].Params)
	}
	r.with(func() {
		if r.links[0] != "TOKEN123 99" || len(r.codes)+len(r.claims) != 0 {
			t.Errorf("links %v, codes %v, claims %v", r.links, r.codes, r.claims)
		}
	})
}

func TestPollerInviteLinkAdminLinkingThemselfIsToldOnce(t *testing.T) {
	r := startPoller(t, nil)
	r.with(func() { r.claimed = Claimed{OK: true, Name: "Ali", AdminChat: "42"} })
	r.bot.AddText(42, "/start TOKEN123", "")
	r.bot.AddReply(42, 0, "next") // processed after the link
	sends := r.await(t, "sendMessage", 2)
	if !strings.Contains(sends[0].Params["text"].(string), "Linked") || sends[1].Params["text"] != replyHint {
		t.Errorf("sends = %+v", sends)
	}
}

func TestPollerInviteLinkUsedOrExpired(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddText(99, "/start OLDTOKEN", "")
	if text := r.await(t, "sendMessage", 1)[0].Params["text"]; text != linkGone {
		t.Errorf("reply = %q", text)
	}
}

func TestPollerInviteLinkErrorIsLoggedWithoutToken(t *testing.T) {
	r := startPoller(t, nil)
	r.set(channel.AnswerResult{}, errors.New("db locked"))
	r.bot.AddText(99, "/start SECRETTOKEN", "")
	if text := r.await(t, "sendMessage", 1)[0].Params["text"].(string); !strings.Contains(text, "try again") {
		t.Errorf("reply = %q", text)
	}
	waitLogs(t, r.logs, 1)
	if strings.Contains(r.logs.String(), "SECRETTOKEN") {
		t.Errorf("token in logs: %s", r.logs.String())
	}
}

// with runs fn holding the rig's lock, for fields the poller goroutine reads.
func (r *pollerRig) with(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}
