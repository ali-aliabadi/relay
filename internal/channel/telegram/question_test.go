package telegram

import (
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

func TestAnswerData(t *testing.T) {
	data := AnswerData("dlv_01JABCDEFGHJKMNPQRSTVWXYZ0", 9)
	if len(data) > 64 {
		t.Errorf("callback data is %d bytes, Telegram allows 64", len(data))
	}
	if id, n, ok := parseAnswerData(data); !ok || id != "dlv_01JABCDEFGHJKMNPQRSTVWXYZ0" || n != 9 {
		t.Errorf("round trip = %q %d %v", id, n, ok)
	}
	for _, bad := range []string{"", "a:", "a::1", "a:dlv_x", "a:dlv_x:", "a:dlv_x:-1", "a:dlv_x:one", "b:dlv_x:1", "dlv_x:1"} {
		if _, _, ok := parseAnswerData(bad); ok {
			t.Errorf("parseAnswerData(%q) accepted", bad)
		}
	}
}

// TestQuestionAlwaysFits: the longest valid content still renders within
// Telegram's limit with the question and its buttons present.
func TestQuestionAlwaysFits(t *testing.T) {
	long := strings.Repeat("ž", message.MaxTextLen)
	opts := make([]string, message.MaxOptions)
	for i := range opts {
		opts[i] = strings.Repeat(string(rune('a'+i)), message.MaxOptionLen)
	}
	parts := Render(message.Message{
		Urgency: "critical", Title: strings.Repeat("t", message.MaxTitleLen), Source: strings.Repeat("s", message.MaxSourceLen),
		DeliveryID: "dlv_01JABCDEFGHJKMNPQRSTVWXYZ0",
		Blocks: []message.Block{
			{Type: "text", Text: long},
			{Type: "text", Text: long},
			{Type: "question", Text: long, Options: opts},
		},
	})
	last := parts[len(parts)-1]
	if visibleLen(stripTags(last.Text)) > maxTextLen {
		t.Errorf("visible length %d > %d", visibleLen(stripTags(last.Text)), maxTextLen)
	}
	if !strings.Contains(last.Text, "❓ <b>žž") || len(last.Buttons) != message.MaxOptions {
		t.Errorf("question or buttons missing: %d buttons", len(last.Buttons))
	}
}

// stripTags drops markup so lengths count what Telegram shows (entities in
// test content are only the layout's own tags).
func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestSendQuestionReturnsLastMessageID(t *testing.T) {
	api, ch := newBotAPI(t)
	msg := goldenCases["question_photo_long"]
	id, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 2 || id != "12" {
		t.Fatalf("Send = %q after %d calls, want the second part's id 12", id, len(api.calls))
	}
	kb := api.calls[1].JSON["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	btn := kb[1].([]any)[0].(map[string]any)
	if btn["callback_data"] != "a:dlv_TEST:1" || btn["text"] != "No" || btn["url"] != nil {
		t.Errorf("answer button = %v", btn)
	}
	if _, ok := api.calls[0].JSON["reply_markup"]; ok {
		t.Error("photo part carries buttons; they belong on the question part")
	}
}
