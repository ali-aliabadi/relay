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

func TestNormalizeTelegramUsername(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"@sara_tg", "sara_tg"},
		{"Sara_TG", "sara_tg"},
		{" @Ali1999 ", "ali1999"},
		{"abcd", "abcd"},
		{"a" + strings.Repeat("b", 31), "a" + strings.Repeat("b", 31)},
	} {
		if got, err := NormalizeTelegramUsername(tt.in); err != nil || got != tt.want {
			t.Errorf("NormalizeTelegramUsername(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{
		"", "@", "abc", "1sara", "_sara", "sara-tg", "sara tg", "sara.tg", "@@sara", "سارا_تست",
		"a" + strings.Repeat("b", 32), "https://t.me/sara",
	} {
		if _, err := NormalizeTelegramUsername(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("NormalizeTelegramUsername(%q) err = %v, want ErrInvalid", bad, err)
		}
	}
}

type inviteRig struct {
	st  *store.Store
	svc *Recipients
	now time.Time
}

func newInviteRig(t *testing.T) *inviteRig {
	t.Helper()
	st := newStore(t)
	svc := NewRecipients(st)
	for _, u := range []string{"ali", "sara"} {
		if _, err := svc.Add(t.Context(), store.Recipient{Username: u, DisplayName: strings.ToUpper(u[:1]) + u[1:]}); err != nil {
			t.Fatal(err)
		}
	}
	return &inviteRig{st: st, svc: svc, now: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)}
}

func (r *inviteRig) contact(t *testing.T, username string) (store.Contact, error) {
	t.Helper()
	rcp, err := r.svc.Get(t.Context(), username)
	if err != nil {
		t.Fatal(err)
	}
	return r.st.Contact(t.Context(), rcp.ID, "telegram")
}

func TestInviteThenClaim(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, err := r.svc.InviteTelegram(ctx, "sara", "@Sara_TG", r.now); err != nil {
		t.Fatal(err)
	}
	invited, err := r.svc.InvitedRecipients(ctx, r.now)
	if err != nil || len(invited) != 1 {
		t.Fatalf("InvitedRecipients = %v, %v", invited, err)
	}
	if _, err := r.contact(t, "sara"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an invite alone created a contact: %v", err)
	}

	// Telegram sends the username with its own casing.
	rcp, ok, err := r.svc.ClaimTelegramInvite(ctx, "sara_TG", "717171", r.now.Add(time.Hour))
	if err != nil || !ok || rcp.Username != "sara" || rcp.DisplayName != "Sara" {
		t.Fatalf("Claim = %+v %v %v", rcp, ok, err)
	}
	c, err := r.contact(t, "sara")
	if err != nil || c.Address != "717171" || c.VerifiedAt == nil || !c.VerifiedAt.Equal(r.now.Add(time.Hour)) {
		t.Errorf("contact = %+v, %v", c, err)
	}
	// The invite is used up: a second /start (or another account) can't reuse it.
	if _, ok, err := r.svc.ClaimTelegramInvite(ctx, "sara_tg", "818181", r.now.Add(time.Hour)); ok || err != nil {
		t.Errorf("second claim = %v, %v", ok, err)
	}
	if c, _ := r.contact(t, "sara"); c.Address != "717171" {
		t.Errorf("second claim changed the contact to %q", c.Address)
	}
	if invited, _ := r.svc.InvitedRecipients(ctx, r.now); len(invited) != 0 {
		t.Errorf("still invited after claiming: %v", invited)
	}
}

func TestClaimMisses(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, err := r.svc.InviteTelegram(ctx, "sara", "sara_tg", r.now); err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct {
		username string
		at       time.Time
	}{
		"other username":    {"sara_tg2", r.now},
		"prefix":            {"sara_t", r.now},
		"no username":       {"", r.now},
		"expired":           {"sara_tg", r.now.Add(InviteTTL + time.Second)},
		"with @ (not sent)": {"@sara_tg", r.now},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok, err := r.svc.ClaimTelegramInvite(ctx, tt.username, "717171", tt.at); ok || err != nil {
				t.Errorf("claim = %v, %v; want no match", ok, err)
			}
		})
	}
	if _, err := r.contact(t, "sara"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a miss linked something: %v", err)
	}
	// Just inside the window still works.
	if _, ok, _ := r.svc.ClaimTelegramInvite(ctx, "sara_tg", "717171", r.now.Add(InviteTTL)); !ok {
		t.Error("claim at exactly InviteTTL failed")
	}
}

func TestInviteKeepsExistingLinkUntilClaimed(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if err := r.svc.Link(ctx, "ali", "telegram", "111", r.now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.InviteTelegram(ctx, "ali", "ali_new_phone", r.now); err != nil {
		t.Fatal(err)
	}
	if c, _ := r.contact(t, "ali"); c.Address != "111" {
		t.Fatalf("invite replaced the working link early: %q", c.Address)
	}
	if _, ok, _ := r.svc.ClaimTelegramInvite(ctx, "ali_new_phone", "222", r.now); !ok {
		t.Fatal("claim failed")
	}
	if c, _ := r.contact(t, "ali"); c.Address != "222" {
		t.Errorf("contact after claim = %q", c.Address)
	}
}

func TestInviteRules(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, err := r.svc.InviteTelegram(ctx, "nobody", "valid_name", r.now); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown recipient err = %v", err)
	}
	if _, err := r.svc.InviteTelegram(ctx, "sara", "no", r.now); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad username err = %v", err)
	}
	if _, err := r.svc.InviteTelegram(ctx, "sara", "shared_acct", r.now); err != nil {
		t.Fatal(err)
	}
	// One Telegram account can't be invited for two people at once.
	if _, err := r.svc.InviteTelegram(ctx, "ali", "@Shared_Acct", r.now); !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), "shared") {
		t.Errorf("duplicate invite err = %v", err)
	}
	// ...unless the earlier invite expired.
	if _, err := r.svc.InviteTelegram(ctx, "ali", "shared_acct", r.now.Add(InviteTTL+time.Second)); err != nil {
		t.Errorf("invite after the other expired = %v", err)
	}
	// Re-inviting the same person replaces their handle and restarts the clock.
	if _, err := r.svc.InviteTelegram(ctx, "sara", "sara_second", r.now.Add(InviteTTL)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := r.svc.ClaimTelegramInvite(ctx, "shared_acct", "1", r.now.Add(InviteTTL+time.Minute)); !ok {
		t.Error("ali's invite should be the only shared_acct one left")
	}
	if _, ok, _ := r.svc.ClaimTelegramInvite(ctx, "sara_second", "2", r.now.Add(2*InviteTTL-time.Minute)); !ok {
		t.Error("re-invite didn't restart the clock")
	}
}

func TestInviteGoesWithRecipient(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, err := r.svc.InviteTelegram(ctx, "sara", "sara_tg", r.now); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Remove(ctx, "sara"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := r.svc.ClaimTelegramInvite(ctx, "sara_tg", "717171", r.now); ok || err != nil {
		t.Errorf("claim after removal = %v, %v", ok, err)
	}
}

func TestRetentionDeletesExpiredInvites(t *testing.T) {
	r := newInviteRig(t)
	ctx := t.Context()
	if _, err := r.svc.InviteTelegram(ctx, "sara", "sara_tg", r.now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.InviteTelegram(ctx, "ali", "ali_tg", r.now.Add(InviteTTL)); err != nil {
		t.Fatal(err)
	}
	var logs syncBuffer
	ret := &Retention{Store: r.st, Days: 30, Logger: obs.NewLogger(&logs, slog.LevelInfo), Clock: func() time.Time { return r.now.Add(InviteTTL + time.Hour) }}
	ret.Pass(ctx)
	if !strings.Contains(logs.String(), `"invites_deleted":1`) {
		t.Errorf("logs = %s", logs.String())
	}
	if strings.Contains(logs.String(), "sara_tg") {
		t.Error("retention logged a username")
	}
	invites, err := r.st.Invites(ctx, "telegram", time.Time{})
	if err != nil || len(invites) != 1 || invites[0].Handle != "ali_tg" {
		t.Errorf("invites left = %+v, %v", invites, err)
	}
}
