package core

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

const hook = "https://app.example/hook?token=SECRET-hook-token"

type notified struct {
	mu    sync.Mutex
	calls []string // "url message_id"
}

func (n *notified) notify(_ context.Context, url, messageID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, url+" "+messageID)
}

func (n *notified) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.calls)
}

type answerRig struct {
	*harness
	svc *Answers
	hk  *notified
}

func newAnswerRig(t *testing.T) *answerRig {
	t.Helper()
	h := newHarness(t)
	hk := &notified{}
	return &answerRig{
		harness: h, hk: hk,
		svc: &Answers{Store: h.st, Logger: obs.NewLogger(h.logs, slog.LevelInfo), Notify: hk.notify},
	}
}

// ask sends a question to "ali" (chat 777) and delivers it.
func (r *answerRig) ask(t *testing.T, q message.Block) (store.Message, store.Delivery) {
	t.Helper()
	m := r.send(t, message.Request{To: []string{"ali"}, Blocks: []message.Block{{Type: "text", Text: "ctx"}, q}})
	if _, err := r.w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	ds := r.deliveries(t, m.ID)
	if len(ds) != 1 || ds[0].Status != store.StatusDelivered {
		t.Fatalf("deliveries = %+v", ds)
	}
	return m, ds[0]
}

func (r *answerRig) record(t *testing.T, a channel.Answer) channel.AnswerResult {
	t.Helper()
	if a.Channel == "" {
		a.Channel = "telegram"
	}
	res, err := r.svc.Record(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (r *answerRig) take(t *testing.T, m store.Message) []store.Answer {
	t.Helper()
	as, err := r.msgs.TakeAnswers(t.Context(), r.client, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	return as
}

var yesNo = message.Block{Type: "question", Text: "Ship?", Options: []string{"Yes", "No"}, Webhook: hook}

func TestRecordButtonAnswerChangeAndFinal(t *testing.T) {
	r := newAnswerRig(t)
	m, d := r.ask(t, yesNo)
	tap := func(opt int) channel.AnswerResult {
		return r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: opt})
	}

	res := tap(0)
	if res.Outcome != channel.AnswerSaved || strings.Join(res.Options, ",") != "Yes,No" {
		t.Fatalf("first tap = %+v", res)
	}
	if res := tap(1); res.Outcome != channel.AnswerSaved {
		t.Fatalf("changed answer = %+v", res)
	}
	if r.hk.count() != 2 || r.hk.calls[0] != hook+" "+m.ID {
		t.Errorf("webhook calls = %v; want one per saved answer", r.hk.calls)
	}
	as := r.take(t, m)
	if len(as) != 1 || as[0].Text != "No" || as[0].Username != "ali" {
		t.Fatalf("answers = %+v", as)
	}
	if res := tap(0); res.Outcome != channel.AnswerFinal {
		t.Errorf("tap after the app fetched = %+v, want final", res)
	}
	if r.hk.count() != 2 {
		t.Errorf("a final answer fired the webhook")
	}
	if as := r.take(t, m); as[0].Text != "No" {
		t.Errorf("final answer changed to %q", as[0].Text)
	}
}

func TestRecordTypedReply(t *testing.T) {
	r := newAnswerRig(t)
	m, d := r.ask(t, message.Block{Type: "question", Text: "New hostname?"})
	res := r.record(t, channel.Answer{Address: "777", ProviderMessageID: d.ProviderMessageID, Option: -1, Text: "nas-02"})
	if res.Outcome != channel.AnswerSaved {
		t.Fatalf("typed reply = %+v", res)
	}
	if r.hk.count() != 0 {
		t.Error("webhook fired for a question without one")
	}
	if as := r.take(t, m); len(as) != 1 || as[0].Text != "nas-02" {
		t.Errorf("answers = %+v", as)
	}
}

func TestRecordTruncatesLongTypedReply(t *testing.T) {
	r := newAnswerRig(t)
	m, d := r.ask(t, message.Block{Type: "question", Text: "Essay?"})
	r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: -1, Text: strings.Repeat("é", message.MaxTextLen+50)})
	if as := r.take(t, m); len([]rune(as[0].Text)) != message.MaxTextLen {
		t.Errorf("stored %d runes, want %d", len([]rune(as[0].Text)), message.MaxTextLen)
	}
}

func TestRecordRejects(t *testing.T) {
	r := newAnswerRig(t)
	_, buttons := r.ask(t, yesNo)
	_, typed := r.ask(t, message.Block{Type: "question", Text: "Name?"})
	plain := r.send(t, message.Request{To: []string{"ali"}, Text: "just a notification"})
	if _, err := r.w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	notQuestion := r.deliveries(t, plain.ID)[0]

	tests := []struct {
		name string
		in   channel.Answer
		want string
	}{
		{"typed reply to buttons", channel.Answer{Address: "777", DeliveryID: buttons.ID, Option: -1, Text: "Yes"}, channel.AnswerNeedOption},
		{"option out of range", channel.Answer{Address: "777", DeliveryID: buttons.ID, Option: 2}, channel.AnswerUnknown},
		{"option on typed question", channel.Answer{Address: "777", DeliveryID: typed.ID, Option: 0}, channel.AnswerUnknown},
		{"blank typed reply", channel.Answer{Address: "777", DeliveryID: typed.ID, Option: -1, Text: " \n"}, channel.AnswerNeedText},
		{"photo or sticker (no text)", channel.Answer{Address: "777", ProviderMessageID: typed.ProviderMessageID, Option: -1}, channel.AnswerNeedText},
		{"not a question", channel.Answer{Address: "777", DeliveryID: notQuestion.ID, Option: 0}, channel.AnswerUnknown},
		{"reply to a notification", channel.Answer{Address: "777", ProviderMessageID: notQuestion.ProviderMessageID, Option: -1, Text: "x"}, channel.AnswerUnknown},
		{"unknown delivery", channel.Answer{Address: "777", DeliveryID: "dlv_missing", Option: 0}, channel.AnswerUnknown},
		{"unknown replied message", channel.Answer{Address: "777", ProviderMessageID: "999999", Option: -1, Text: "x"}, channel.AnswerUnknown},
		{"someone else's chat", channel.Answer{Address: "666", DeliveryID: buttons.ID, Option: 0}, channel.AnswerUnknown},
		{"someone else replying", channel.Answer{Address: "666", ProviderMessageID: typed.ProviderMessageID, Option: -1, Text: "x"}, channel.AnswerUnknown},
		{"other channel", channel.Answer{Channel: "sms", Address: "777", DeliveryID: buttons.ID, Option: 0}, channel.AnswerUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if res := r.record(t, tt.in); res.Outcome != tt.want {
				t.Errorf("outcome = %q, want %q", res.Outcome, tt.want)
			}
		})
	}
	var n int
	for _, d := range []store.Delivery{buttons, typed, notQuestion} {
		as, err := r.st.TakeAnswers(t.Context(), d.MessageID)
		if err != nil {
			t.Fatal(err)
		}
		n += len(as)
	}
	if n != 0 || r.hk.count() != 0 {
		t.Errorf("rejected answers stored %d rows, fired %d webhooks", n, r.hk.count())
	}
}

func TestRecordExpiredQuestion(t *testing.T) {
	r := newAnswerRig(t)
	_, d := r.ask(t, yesNo)
	r.clk.Advance(31 * 24 * time.Hour)
	ret := &Retention{Store: r.st, Days: 30, Logger: r.svc.Logger, Clock: r.clk.Now}
	ret.Pass(t.Context())
	if res := r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: 0}); res.Outcome != channel.AnswerExpired {
		t.Errorf("outcome = %+v, want expired", res)
	}
}

func TestRecordEachRecipientSeparately(t *testing.T) {
	r := newAnswerRig(t)
	sara, err := r.st.CreateRecipient(t.Context(), store.Recipient{Username: "sara", DisplayName: "Sara"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.st.UpsertContact(t.Context(), store.Contact{RecipientID: sara.ID, Channel: "telegram", Address: "888"}); err != nil {
		t.Fatal(err)
	}
	m := r.send(t, message.Request{To: []string{"ali", "sara"}, Blocks: []message.Block{yesNo}})
	if _, err := r.w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	ds := r.deliveries(t, m.ID)
	// Telegram message IDs are per chat, so both chats can get the same one.
	for _, d := range ds {
		if err := r.st.MarkDelivered(t.Context(), d.ID, "5"); err != nil {
			t.Fatal(err)
		}
	}
	for addr, opt := range map[string]int{"777": 0, "888": 1} {
		res := r.record(t, channel.Answer{Address: addr, ProviderMessageID: "5", Option: -1, Text: "typed"})
		if res.Outcome != channel.AnswerNeedOption {
			t.Errorf("typed reply from %s = %+v", addr, res)
		}
		for _, d := range ds {
			r.record(t, channel.Answer{Address: addr, DeliveryID: d.ID, Option: opt}) // only the own delivery counts
		}
	}
	as := r.take(t, m)
	if len(as) != 2 || as[0].Username != "ali" || as[0].Text != "Yes" || as[1].Username != "sara" || as[1].Text != "No" {
		t.Errorf("answers = %+v", as)
	}
}

func TestRecordLogsNoContent(t *testing.T) {
	r := newAnswerRig(t)
	_, d := r.ask(t, message.Block{Type: "question", Text: "Name?", Webhook: hook})
	r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: -1, Text: "SECRET-answer"})
	logs := r.logs.String()
	if !strings.Contains(logs, `"msg":"answer"`) || !strings.Contains(logs, d.ID) {
		t.Errorf("no answer log line: %s", logs)
	}
	for _, leak := range []string{"SECRET", "777", "Name?"} {
		if strings.Contains(logs, leak) {
			t.Errorf("logs contain %q: %s", leak, logs)
		}
	}
}

func TestTakeAnswersIsScopedToClient(t *testing.T) {
	r := newAnswerRig(t)
	m, d := r.ask(t, yesNo)
	r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: 0})
	other, err := r.st.CreateClient(t.Context(), "other", []byte(strings.Repeat("o", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.msgs.TakeAnswers(t.Context(), other.ID, m.ID); err == nil {
		t.Fatal("another client read the answers")
	}
	// ...and that attempt didn't make the answer final.
	if res := r.record(t, channel.Answer{Address: "777", DeliveryID: d.ID, Option: 1}); res.Outcome != channel.AnswerSaved {
		t.Errorf("answer after a foreign read = %+v", res)
	}
}

func TestWorkerPassesDeliveryIDToLayout(t *testing.T) {
	r := newAnswerRig(t)
	_, d := r.ask(t, yesNo)
	sent := r.ch.Sent()
	if len(sent) != 1 || sent[0].Msg.DeliveryID != d.ID {
		t.Errorf("sent DeliveryID = %q, want %q", sent[0].Msg.DeliveryID, d.ID)
	}
}
