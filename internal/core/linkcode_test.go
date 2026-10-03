package core

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/store"
)

func TestLinkCodeLinksTheChat(t *testing.T) {
	st := newStore(t)
	svc := NewRecipients(st)
	ctx := t.Context()
	if _, err := svc.Add(ctx, store.Recipient{Username: "ali", DisplayName: "Ali"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	code, err := svc.LinkCode("telegram", "515151", now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "515151") || strings.ContainsAny(code, " +/=\n") {
		t.Errorf("code %q shows the chat id or isn't copy-paste safe", code)
	}
	ch, addr, err := svc.LinkWithCode(ctx, "ali", "  "+code+"\n", now.Add(59*time.Minute))
	if err != nil || ch != "telegram" || addr != "515151" {
		t.Fatalf("LinkWithCode = %q %q %v", ch, addr, err)
	}
	rcp, _ := svc.Get(ctx, "ali")
	c, err := st.Contact(ctx, rcp.ID, "telegram")
	if err != nil || c.Address != "515151" || c.VerifiedAt == nil {
		t.Errorf("contact = %+v, %v", c, err)
	}
	// Relinking to a new chat replaces the old one.
	code2, _ := svc.LinkCode("telegram", "616161", now)
	if _, _, err := svc.LinkWithCode(ctx, "ali", code2, now); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Contact(ctx, rcp.ID, "telegram"); c.Address != "616161" {
		t.Errorf("relink kept %q", c.Address)
	}
}

func TestLinkCodeRejects(t *testing.T) {
	st := newStore(t)
	svc := NewRecipients(st)
	ctx := t.Context()
	if _, err := svc.Add(ctx, store.Recipient{Username: "ali", DisplayName: "Ali"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	code, _ := svc.LinkCode("telegram", "515151", now)
	otherKey, _ := NewRecipients(newStoreWithKey(t, 8)).LinkCode("telegram", "515151", now)
	wrongPurpose, _ := st.SealToken("something-else", []byte("telegram|515151|9999999999"))
	malformed, _ := st.SealToken(linkPurpose, []byte("telegram|515151"))
	badExpiry, _ := st.SealToken(linkPurpose, []byte("telegram|515151|soon"))
	tampered := []byte(code)
	tampered[5] ^= 1

	for name, tt := range map[string]struct {
		code string
		at   time.Time
	}{
		"expired":          {code, now.Add(LinkCodeTTL + time.Second)},
		"empty":            {"", now},
		"garbage":          {"hello", now},
		"tampered":         {string(tampered), now},
		"other server key": {otherKey, now},
		"other purpose":    {wrongPurpose, now},
		"malformed":        {malformed, now},
		"bad expiry":       {badExpiry, now},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := svc.LinkWithCode(ctx, "ali", tt.code, tt.at); !errors.Is(err, ErrBadLinkCode) {
				t.Errorf("err = %v, want ErrBadLinkCode", err)
			}
		})
	}
	if _, _, err := svc.LinkWithCode(ctx, "nobody", code, now); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown username err = %v", err)
	}
	rcp, _ := svc.Get(ctx, "ali")
	if _, err := st.Contact(ctx, rcp.ID, "telegram"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a rejected code linked something: %v", err)
	}
}
