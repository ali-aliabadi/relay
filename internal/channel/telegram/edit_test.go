package telegram

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

func TestEditTextEscapesAndUsesOriginalProviderID(t *testing.T) {
	b, ch := newBotAPI(t)
	msg := message.Message{Title: "Progress", Blocks: []message.Block{{Type: message.BlockText, Text: "<script> & 42%"}}}
	if err := ch.EditText(t.Context(), channel.Contact{Address: "1000001"}, "123", msg); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	got := b.calls[0]
	if got.Method != "editMessageText" || got.JSON["message_id"] != float64(123) || !strings.Contains(got.JSON["text"].(string), "&lt;script&gt;") {
		t.Fatalf("unexpected edit: %+v", got)
	}
}

func TestEditTextIsIdempotentAndNeverEchoesProviderContent(t *testing.T) {
	for _, unchanged := range []bool{true, false} {
		b, ch := newBotAPI(t)
		description := "private text from provider"
		if unchanged {
			description = "Bad Request: message is not modified: specified new message content and reply markup are exactly the same"
		}
		b.respond = func(string) (int, string) {
			return http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"` + description + `"}`
		}
		err := ch.EditText(t.Context(), channel.Contact{Address: "1000001"}, "123", message.Message{Blocks: []message.Block{{Type: message.BlockText, Text: "Hi"}}})
		if unchanged && err != nil {
			t.Fatal(err)
		}
		if !unchanged && (err == nil || strings.Contains(err.Error(), "private")) {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}

func TestEditTextRefusesInvalidIDAndRichContent(t *testing.T) {
	_, ch := newBotAPI(t)
	for _, id := range []string{"0", "bad"} {
		var ce *channel.Error
		err := ch.EditText(t.Context(), channel.Contact{}, id, message.Message{})
		if !errors.As(err, &ce) || !ce.Permanent {
			t.Fatalf("invalid edit: %v", err)
		}
	}
}
