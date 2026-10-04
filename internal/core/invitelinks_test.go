package core

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

func TestInviteLinkAddsAndLinks(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	rcp, token, err := r.svc.InviteLink(ctx, "mina", "Mina", "Europe/Berlin", r.now)
	if err != nil || rcp.Username != "mina" || rcp.DisplayName != "Mina" || rcp.Timezone != "Europe/Berlin" {
		t.Fatalf("InviteLink = %+v, %v", rcp, err)
	}
	if len(token) != inviteTokenLen || strings.ContainsAny(token, "+/=") {
		t.Errorf("token %q isn't a 43-character base64url string", token)
	}
	got, ok, err := r.svc.ClaimInviteLink(ctx, token, "777", r.now.Add(InviteLinkTTL-time.Second))
	if err != nil || !ok || got.ID != rcp.ID {
		t.Fatalf("claim = %+v, %v, %v", got, ok, err)
	}
	if c, err := r.contact(t, "mina"); err != nil || c.Address != "777" || c.VerifiedAt == nil {
		t.Errorf("contact = %+v, %v", c, err)
	}
	// Single use.
	if _, ok, err := r.svc.ClaimInviteLink(ctx, token, "888", r.now); ok || err != nil {
		t.Errorf("second claim = %v, %v", ok, err)
	}
	if c, _ := r.contact(t, "mina"); c.Address != "777" {
		t.Errorf("second claim changed the contact to %q", c.Address)
	}
}

func TestInviteLinkMisses(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	_, old, err := r.svc.InviteLink(ctx, "sara", "", "UTC", r.now)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := r.svc.InviteLink(ctx, "sara", "ignored", "UTC", r.now)
	if err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct {
		token string
		at    time.Time
	}{
		"replaced":  {old, r.now},
		"expired":   {token, r.now.Add(InviteLinkTTL)},
		"malformed": {"short", r.now},
		"unknown":   {strings.Repeat("A", inviteTokenLen), r.now},
		"empty":     {"", r.now},
	} {
		if _, ok, err := r.svc.ClaimInviteLink(ctx, tt.token, "777", tt.at); ok || err != nil {
			t.Errorf("%s: claim = %v, %v", name, ok, err)
		}
	}
	if _, err := r.contact(t, "sara"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("sara got linked: %v", err)
	}
	if rcp, _ := r.svc.Get(ctx, "sara"); rcp.DisplayName != "Sara" {
		t.Errorf("re-inviting changed the display name to %q", rcp.DisplayName)
	}
}

func TestInviteLinkByAliasAndRules(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if err := r.svc.AddAlias(ctx, "ali", "admin", r.now); err != nil {
		t.Fatal(err)
	}
	if rcp, _, err := r.svc.InviteLink(ctx, "admin", "", "UTC", r.now); err != nil || rcp.Username != "ali" {
		t.Errorf("alias invite = %+v, %v", rcp, err)
	}
	for _, bad := range []struct{ name, display, tz string }{
		{"Bad!", "", "UTC"},
		{"newbie", strings.Repeat("x", 65), "UTC"},
		{"newbie", "", "Mars/Olympus"},
	} {
		if _, _, err := r.svc.InviteLink(ctx, bad.name, bad.display, bad.tz, r.now); !errors.Is(err, ErrInvalid) {
			t.Errorf("InviteLink(%q, %q, %q) err = %v", bad.name, bad.display, bad.tz, err)
		}
	}
	if _, err := r.svc.Get(ctx, "newbie"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("invalid invite added a recipient: %v", err)
	}
}

func TestInviteLinkDropsUsernameInvite(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, err := r.svc.InviteTelegram(ctx, "sara", "sara_tg", r.now); err != nil {
		t.Fatal(err)
	}
	_, token, err := r.svc.InviteLink(ctx, "sara", "", "UTC", r.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := r.svc.ClaimInviteLink(ctx, token, "777", r.now); !ok || err != nil {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	// The older @username invite can no longer move Sara to another chat.
	if _, ok, err := r.svc.ClaimTelegramInvite(ctx, "sara_tg", "999", r.now); ok || err != nil {
		t.Errorf("username claim after link = %v, %v", ok, err)
	}
}

func TestTelegramAdmin(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, _, ok, err := r.svc.TelegramAdmin(ctx, "ali"); ok || err != nil {
		t.Errorf("unlinked admin = %v, %v", ok, err)
	}
	if err := r.svc.Link(ctx, "ali", "telegram", "4242", r.now); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.AddAlias(ctx, "ali", "admin", r.now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ali", "admin"} {
		if rcp, chat, ok, err := r.svc.TelegramAdmin(ctx, name); !ok || err != nil || chat != "4242" || rcp.Username != "ali" {
			t.Errorf("TelegramAdmin(%q) = %+v, %q, %v, %v", name, rcp, chat, ok, err)
		}
	}
	for _, name := range []string{"", "nobody"} {
		if _, _, ok, err := r.svc.TelegramAdmin(ctx, name); ok || err != nil {
			t.Errorf("TelegramAdmin(%q) = %v, %v", name, ok, err)
		}
	}
}

func TestRetentionDeletesExpiredInviteLinks(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, _, err := r.svc.InviteLink(ctx, "sara", "", "UTC", r.now); err != nil {
		t.Fatal(err)
	}
	_, keep, err := r.svc.InviteLink(ctx, "ali", "", "UTC", r.now.Add(InviteLinkTTL))
	if err != nil {
		t.Fatal(err)
	}
	var logs syncBuffer
	at := r.now.Add(InviteLinkTTL + time.Hour)
	ret := &Retention{Store: r.st, Days: 30, Logger: obs.NewLogger(&logs, slog.LevelInfo), Clock: func() time.Time { return at }}
	ret.Pass(ctx)
	if !strings.Contains(logs.String(), `"invites_deleted":1`) {
		t.Errorf("logs = %s", logs.String())
	}
	if _, ok, err := r.svc.ClaimInviteLink(ctx, keep, "777", at); !ok || err != nil {
		t.Errorf("unexpired link was purged: %v, %v", ok, err)
	}
}
