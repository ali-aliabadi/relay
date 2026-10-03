package core

import (
	"errors"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

func TestAliasRules(t *testing.T) {
	st := newStore(t)
	svc := NewRecipients(st)
	ctx := t.Context()
	now := time.Now()
	for _, u := range []string{"ali", "tara"} {
		if _, err := svc.Add(ctx, store.Recipient{Username: u, DisplayName: u}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.AddAlias(ctx, "ali", "admin", now); err != nil {
		t.Fatal(err)
	}
	for name, tt := range map[string]struct {
		username, alias string
		want            error
	}{
		"bad alias":             {"ali", "Admin!", ErrInvalid},
		"empty alias":           {"ali", "", ErrInvalid},
		"unknown recipient":     {"nobody", "boss", store.ErrNotFound},
		"alias of an alias":     {"admin", "boss", store.ErrNotFound},
		"taken by an alias":     {"tara", "admin", store.ErrConflict},
		"taken by a username":   {"ali", "tara", store.ErrConflict},
		"own username":          {"ali", "ali", store.ErrConflict},
		"same alias, same user": {"ali", "admin", store.ErrConflict},
	} {
		t.Run(name, func(t *testing.T) {
			if err := svc.AddAlias(ctx, tt.username, tt.alias, now); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
	// A new recipient can't take a name that's already an alias.
	if _, err := svc.Add(ctx, store.Recipient{Username: "admin", DisplayName: "x"}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("recipient named like an alias err = %v", err)
	}
	if err := svc.RemoveAlias(ctx, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveAlias(ctx, "admin"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second remove err = %v", err)
	}
	// Once free, the name can be reused, even as a username.
	if _, err := svc.Add(ctx, store.Recipient{Username: "admin", DisplayName: "x"}); err != nil {
		t.Errorf("freed name: %v", err)
	}
}

func TestSendToAliases(t *testing.T) {
	r := newAnswerRig(t) // ali (chat 777) with a linked contact
	ctx := t.Context()
	tara, err := r.st.CreateRecipient(ctx, store.Recipient{Username: "tara", DisplayName: "Tara"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.st.UpsertContact(ctx, store.Contact{RecipientID: tara.ID, Channel: "telegram", Address: "888"}); err != nil {
		t.Fatal(err)
	}
	svc := NewRecipients(r.st)
	for alias, user := range map[string]string{"admin": "ali", "co-admin": "tara"} {
		if err := svc.AddAlias(ctx, user, alias, r.clk.Now()); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name string
		to   []string
		want map[string]bool // recipient IDs with one delivery each
	}{
		{"alias", []string{"admin"}, map[string]bool{r.aliID(t): true}},
		{"alias and username dedupe", []string{"admin", "ali"}, map[string]bool{r.aliID(t): true}},
		{"username then alias", []string{"ali", "admin"}, map[string]bool{r.aliID(t): true}},
		{"two people", []string{"admin", "co-admin", "tara"}, map[string]bool{r.aliID(t): true, tara.ID: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := r.send(t, message.Request{To: tt.to, Text: "hi"})
			ds := r.deliveries(t, m.ID)
			if len(ds) != len(tt.want) {
				t.Fatalf("deliveries = %d, want %d", len(ds), len(tt.want))
			}
			for _, d := range ds {
				if !tt.want[d.RecipientID] {
					t.Errorf("unexpected delivery to %s", d.RecipientID)
				}
			}
		})
	}

	_, _, err = r.msgs.Create(ctx, r.client, "", message.Request{To: []string{"ali", "boss"}, Text: "hi"})
	var ve *message.ValidationError
	if !errors.As(err, &ve) || len(ve.Problems) != 1 || ve.Problems[0] != "to[1]: unknown recipient" {
		t.Errorf("unknown alias err = %v", err)
	}
	if err := svc.RemoveAlias(ctx, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.msgs.Create(ctx, r.client, "", message.Request{To: []string{"admin"}, Text: "hi"}); !errors.As(err, &ve) {
		t.Errorf("removed alias still delivers: %v", err)
	}
}

func TestAnswersViaAliasReportUsername(t *testing.T) {
	r := newAnswerRig(t)
	if err := NewRecipients(r.st).AddAlias(t.Context(), "ali", "admin", r.clk.Now()); err != nil {
		t.Fatal(err)
	}
	m := r.send(t, message.Request{To: []string{"admin"}, Blocks: []message.Block{yesNo}})
	if _, err := r.w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	d := r.deliveries(t, m.ID)[0]
	r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: 0})
	if as := r.take(t, m); len(as) != 1 || as[0].Username != "ali" {
		t.Errorf("answers = %+v", as)
	}
}

func (r *answerRig) aliID(t *testing.T) string {
	t.Helper()
	rcp, err := r.st.RecipientByUsername(t.Context(), "ali")
	if err != nil {
		t.Fatal(err)
	}
	return rcp.ID
}
