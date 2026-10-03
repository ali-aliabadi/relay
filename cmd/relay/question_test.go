package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel/telegram/telegramtest"
)

// apiClient calls the in-process server's public API.
type apiClient struct {
	t        *testing.T
	base     string
	key      string
	lastBody string
}

func (a *apiClient) call(method, path, body string) map[string]any {
	a.t.Helper()
	req, err := http.NewRequestWithContext(a.t.Context(), method, a.base+path, strings.NewReader(body))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	a.lastBody = string(raw)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		a.t.Fatalf("%s %s: %s", method, path, raw)
	}
	return out
}

// sentTo returns the last sendMessage to chat once it has the question.
func sentQuestion(t *testing.T, bot *telegramtest.Server, question string) telegramtest.Call {
	t.Helper()
	var got telegramtest.Call
	waitFor(t, func() bool {
		for _, c := range bot.Calls("sendMessage") {
			if text, _ := c.Params["text"].(string); c.MessageID != 0 && strings.Contains(text, question) {
				got = c
				return true
			}
		}
		return false
	})
	return got
}

func callbackData(t *testing.T, c telegramtest.Call, option int) string {
	t.Helper()
	rows := c.Params["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	return rows[option].([]any)[0].(map[string]any)["callback_data"].(string)
}

// TestQuestionThroughServe drives the whole answer flow in-process: API →
// worker → Telegram → button taps and a typed reply → poller → API.
func TestQuestionThroughServe(t *testing.T) {
	c, bot := newTelegramCLI(t)
	c.run("recipients", "add", "ali", "--name", "Ali")
	base, logs := startServe(t, c)
	if rc, _, errOut := c.run("recipients", "link", "ali", linkCode(t, bot, 515151)); rc != 0 {
		t.Fatal(errOut)
	}
	_, out, _ := c.run("clients", "create", "deploy-bot")
	api := &apiClient{t: t, base: base, key: keyRe.FindString(out)}

	// Buttons, with a webhook Relay must refuse to call (loopback).
	created := api.call("POST", "/v1/messages", `{"to":["ali"],"blocks":[
		{"type":"text","text":"v2 passed staging."},
		{"type":"question","text":"Ship v2?","options":["Yes","No"],"webhook":"https://127.0.0.1:1/hook"}]}`)
	id, _ := created["id"].(string)
	sent := sentQuestion(t, bot, "Ship v2?")
	if got := api.call("GET", "/v1/messages/"+id+"/answers", ""); len(got["answers"].([]any)) != 0 {
		t.Fatalf("answers before tapping = %v", got)
	}

	bot.AddTap(515151, sent.MessageID, callbackData(t, sent, 1), sent.Params["reply_markup"])
	bot.AddTap(515151, sent.MessageID, callbackData(t, sent, 0), sent.Params["reply_markup"]) // changed mind
	waitFor(t, func() bool { return len(bot.Calls("editMessageReplyMarkup")) == 2 })
	got := api.call("GET", "/v1/messages/"+id+"/answers", "")
	if !strings.Contains(api.lastBody, `"recipient":"ali","answer":"Yes"`) {
		t.Fatalf("answers = %v", got)
	}

	bot.AddTap(515151, sent.MessageID, callbackData(t, sent, 1), sent.Params["reply_markup"])
	waitFor(t, func() bool { return len(bot.Calls("answerCallbackQuery")) == 3 })
	if toast := bot.Calls("answerCallbackQuery")[2].Params["text"].(string); !strings.Contains(toast, "can't be changed") {
		t.Errorf("toast after fetch = %q", toast)
	}
	api.call("GET", "/v1/messages/"+id+"/answers", "")
	if !strings.Contains(api.lastBody, `"answer":"Yes"`) {
		t.Errorf("final answer changed: %s", api.lastBody)
	}

	// A typed reply to a free-text question; a stranger's reply is refused.
	created = api.call("POST", "/v1/messages", `{"to":["ali"],"blocks":[{"type":"question","text":"New hostname?"}]}`)
	typedID, _ := created["id"].(string)
	typed := sentQuestion(t, bot, "New hostname?")
	before := len(bot.Calls("sendMessage"))
	bot.AddReply(424242, typed.MessageID, "evil")
	bot.AddReply(515151, typed.MessageID, "nas-02")
	waitFor(t, func() bool { return len(bot.Calls("sendMessage")) >= before+2 })
	replies := bot.Calls("sendMessage")[before:]
	if !strings.Contains(replies[0].Params["text"].(string), "isn't a question") || replies[1].Params["text"] != "✅ Answer saved." {
		t.Errorf("replies = %v / %v", replies[0].Params, replies[1].Params)
	}
	api.call("GET", "/v1/messages/"+typedID+"/answers", "")
	if !strings.Contains(api.lastBody, `"answer":"nas-02"`) || strings.Contains(api.lastBody, "evil") {
		t.Errorf("typed answers = %s", api.lastBody)
	}

	waitFor(t, func() bool { return strings.Contains(logs.String(), "answer webhook failed") })
	l := logs.String()
	if !strings.Contains(l, "webhook address is not public") {
		t.Errorf("webhook to loopback not refused: %s", l)
	}
	for _, leak := range []string{"nas-02", "evil", "515151", "424242", "Ship v2", "127.0.0.1:1"} {
		if strings.Contains(l, leak) {
			t.Errorf("serve logs contain %q", leak)
		}
	}
}
