package telegram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/channel/telegram/telegramtest"
	"github.com/ali-aliabadi/relay/internal/obs"
)

// pollerRig runs a Poller against the fake Bot API and records answers.
type pollerRig struct {
	bot     *telegramtest.Server
	logs    *syncBuf
	mu      sync.Mutex
	answers []channel.Answer
	result  channel.AnswerResult
	err     error
	codes   []string
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func startPoller(t *testing.T, wrap func(http.Handler) http.Handler) *pollerRig {
	t.Helper()
	r := &pollerRig{bot: telegramtest.New(testToken), logs: &syncBuf{}, result: channel.AnswerResult{Outcome: channel.AnswerSaved}}
	var h http.Handler = r.bot
	if wrap != nil {
		h = wrap(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p := &Poller{
		Client: NewClient(srv.URL, obs.Secret(testToken), srv.Client()),
		OnAnswer: func(_ context.Context, a channel.Answer) (channel.AnswerResult, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.answers = append(r.answers, a)
			return r.result, r.err
		},
		LinkCode: func(chatID string) (string, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.codes = append(r.codes, chatID)
			return "CODE-" + chatID, nil
		},
		Logger:  obs.NewLogger(r.logs, slog.LevelInfo),
		Backoff: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return r
}

func (r *pollerRig) set(res channel.AnswerResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.result, r.err = res, err
}

func (r *pollerRig) got() []channel.Answer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]channel.Answer(nil), r.answers...)
}

// await waits until method has been called n times and returns the calls.
func (r *pollerRig) await(t *testing.T, method string, n int) []telegramtest.Call {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		calls := r.bot.Calls(method)
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s called %d times, want %d", method, len(calls), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func keyboard(buttons ...inlineButton) replyMarkup {
	rows := make([][]inlineButton, len(buttons))
	for i, b := range buttons {
		rows[i] = []inlineButton{b}
	}
	return replyMarkup{InlineKeyboard: rows}
}

func TestPollerTapSavesAndTicksChoice(t *testing.T) {
	r := startPoller(t, nil)
	r.set(channel.AnswerResult{Outcome: channel.AnswerSaved, Options: []string{"Yes", "No"}}, nil)
	// The keyboard as last drawn: "Yes" was ticked before; a link button stays.
	r.bot.AddTap(42, 7, AnswerData("dlv_1", 1), keyboard(
		inlineButton{Text: "✅ Yes", CallbackData: AnswerData("dlv_1", 0)},
		inlineButton{Text: "No", CallbackData: AnswerData("dlv_1", 1)},
		inlineButton{Text: "Diff", URL: "https://git.example/diff"},
	))
	edits := r.await(t, "editMessageReplyMarkup", 1)
	acks := r.await(t, "answerCallbackQuery", 1)

	if a := r.got(); len(a) != 1 || a[0] != (channel.Answer{Channel: "telegram", Address: "42", DeliveryID: "dlv_1", Option: 1}) {
		t.Fatalf("answers = %+v", a)
	}
	if acks[0].Params["text"] != "✅ Answer saved." || acks[0].Params["callback_query_id"] == "" {
		t.Errorf("ack = %v", acks[0].Params)
	}
	p := edits[0].Params
	if p["chat_id"] != float64(42) || p["message_id"] != float64(7) {
		t.Errorf("edit target = %v", p)
	}
	rows := p["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	var texts []string
	for _, row := range rows {
		b := row.([]any)[0].(map[string]any)
		texts = append(texts, b["text"].(string))
	}
	if strings.Join(texts, "|") != "Yes|✅ No|Diff" {
		t.Errorf("redrawn buttons = %v", texts)
	}
	if link := rows[2].([]any)[0].(map[string]any); link["url"] != "https://git.example/diff" {
		t.Errorf("link button lost: %v", link)
	}
}

func TestPollerTapOutcomes(t *testing.T) {
	tests := []struct {
		outcome string
		toast   string
	}{
		{channel.AnswerFinal, "can't be changed"},
		{channel.AnswerExpired, "expired"},
		{channel.AnswerUnknown, "isn't a question"},
	}
	for _, tt := range tests {
		t.Run(tt.outcome, func(t *testing.T) {
			r := startPoller(t, nil)
			r.set(channel.AnswerResult{Outcome: tt.outcome, Options: []string{"Yes"}}, nil)
			r.bot.AddTap(42, 7, AnswerData("dlv_1", 0), keyboard(inlineButton{Text: "Yes", CallbackData: AnswerData("dlv_1", 0)}))
			acks := r.await(t, "answerCallbackQuery", 1)
			if !strings.Contains(acks[0].Params["text"].(string), tt.toast) {
				t.Errorf("toast = %v", acks[0].Params["text"])
			}
			if n := len(r.bot.Calls("editMessageReplyMarkup")); n != 0 {
				t.Errorf("buttons redrawn %d times for %s", n, tt.outcome)
			}
		})
	}
}

func TestPollerTapWithForeignDataIsNotRecorded(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddTap(42, 7, "something-else", nil)
	acks := r.await(t, "answerCallbackQuery", 1)
	if len(r.got()) != 0 || !strings.Contains(acks[0].Params["text"].(string), "isn't a question") {
		t.Errorf("answers = %v, ack = %v", r.got(), acks[0].Params)
	}
}

func TestPollerTapWithoutKeyboardSkipsRedraw(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddTap(42, 7, AnswerData("dlv_1", 0), nil)
	r.await(t, "answerCallbackQuery", 1)
	r.bot.AddReply(42, 0, "ping") // a later update proves the tap was fully handled
	r.await(t, "sendMessage", 1)
	if n := len(r.bot.Calls("editMessageReplyMarkup")); n != 0 {
		t.Errorf("edited %d times without a keyboard", n)
	}
}

func TestPollerTypedReply(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddReply(42, 7, "nas-02")
	sends := r.await(t, "sendMessage", 1)
	want := channel.Answer{Channel: "telegram", Address: "42", ProviderMessageID: "7", Option: -1, Text: "nas-02"}
	if a := r.got(); len(a) != 1 || a[0] != want {
		t.Fatalf("answers = %+v", a)
	}
	p := sends[0].Params
	if p["chat_id"] != float64(42) || p["text"] != "✅ Answer saved." || p["reply_parameters"] == nil {
		t.Errorf("confirmation = %v", p)
	}
}

func TestPollerTypedReplyOutcomes(t *testing.T) {
	for outcome, want := range map[string]string{
		channel.AnswerNeedOption: "one of the buttons",
		channel.AnswerNeedText:   "with text",
		channel.AnswerFinal:      "can't be changed",
	} {
		t.Run(outcome, func(t *testing.T) {
			r := startPoller(t, nil)
			r.set(channel.AnswerResult{Outcome: outcome}, nil)
			r.bot.AddReply(42, 7, "x")
			if got := r.await(t, "sendMessage", 1)[0].Params["text"].(string); !strings.Contains(got, want) {
				t.Errorf("feedback = %q", got)
			}
		})
	}
}

func TestPollerMessageWithoutReplyGetsHint(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddReply(42, 0, "yes")
	if got := r.await(t, "sendMessage", 1)[0].Params["text"]; got != replyHint {
		t.Errorf("reply = %v", got)
	}
	if len(r.got()) != 0 {
		t.Errorf("a non-reply was recorded as an answer: %+v", r.got())
	}
}

func TestPollerStartSendsLinkCode(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddStart(42)
	got := r.await(t, "sendMessage", 1)[0].Params
	if got["chat_id"] != float64(42) || !strings.Contains(got["text"].(string), "relay recipients link <your username> CODE-42") {
		t.Errorf("start reply = %v", got)
	}
	if len(r.got()) != 0 || len(r.codes) != 1 {
		t.Errorf("answers %v, codes %v", r.got(), r.codes)
	}
}

func TestPollerIgnoresGroupChats(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddGroupMessage(-100, "/start")
	r.bot.AddStart(42) // processed after the group message
	if got := r.await(t, "sendMessage", 1); len(got) != 1 || got[0].Params["chat_id"] != float64(42) {
		t.Errorf("sends = %+v", got)
	}
}

func TestPollerHandlerErrorIsReportedNotFatal(t *testing.T) {
	r := startPoller(t, nil)
	r.set(channel.AnswerResult{}, errors.New("db is down for message msg_1"))
	r.bot.AddTap(42, 7, AnswerData("dlv_1", 0), nil)
	r.bot.AddReply(42, 7, "SECRET-answer")
	acks := r.await(t, "answerCallbackQuery", 1)
	sends := r.await(t, "sendMessage", 1)
	if !strings.Contains(acks[0].Params["text"].(string), "try again") || !strings.Contains(sends[0].Params["text"].(string), "try again") {
		t.Errorf("ack %v, send %v", acks[0].Params, sends[0].Params)
	}
	waitLogs(t, r.logs, 2)
	if logs := r.logs.String(); strings.Contains(logs, "SECRET") || strings.Contains(logs, `"42"`) {
		t.Errorf("logs leak content or chat id: %s", logs)
	}
	// Still polling afterwards.
	r.set(channel.AnswerResult{Outcome: channel.AnswerSaved}, nil)
	r.bot.AddReply(42, 7, "again")
	r.await(t, "sendMessage", 2)
}

func waitLogs(t *testing.T, logs *syncBuf, lines int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Count(logs.String(), "\n") < lines {
		if time.Now().After(deadline) {
			t.Fatalf("logs = %s", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPollerHandlesEachUpdateOnce(t *testing.T) {
	r := startPoller(t, nil)
	r.bot.AddReply(42, 7, "one")
	r.bot.AddReply(42, 8, "two")
	r.await(t, "sendMessage", 2)
	r.await(t, "getUpdates", len(r.bot.Calls("getUpdates"))+3) // several more polls
	if a := r.got(); len(a) != 2 || a[0].Text != "one" || a[1].Text != "two" {
		t.Errorf("answers = %+v", a)
	}
}

func TestPollerBacksOffAndRecovers(t *testing.T) {
	var fails atomic.Int32
	fails.Store(3)
	r := startPoller(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasSuffix(req.URL.Path, "/getUpdates") && fails.Add(-1) >= 0 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"ok":false,"error_code":502,"description":"Bad Gateway"}`))
				return
			}
			next.ServeHTTP(w, req)
		})
	})
	r.bot.AddReply(42, 7, "after outage")
	r.await(t, "sendMessage", 1)
	logs := r.logs.String()
	if n := strings.Count(logs, "telegram polling failed"); n != 1 {
		t.Errorf("logged the outage %d times, want once: %s", n, logs)
	}
	if strings.Contains(logs, testToken) {
		t.Error("bot token in logs")
	}
}
