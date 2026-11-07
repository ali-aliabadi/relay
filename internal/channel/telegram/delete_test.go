package telegram

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/channel"
)

func TestDeleteTextUsesOriginalIDAndRejectsInvalidIDs(t *testing.T) {
	b, ch := newBotAPI(t)
	for _, id := range []string{"0", "-1", "bad"} {
		if err := ch.DeleteText(t.Context(), channel.Contact{Address: "1000001"}, id); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	if err := ch.DeleteText(t.Context(), channel.Contact{Address: "1000001"}, "123"); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) != 1 || b.calls[0].Method != "deleteMessage" || b.calls[0].JSON["message_id"] != float64(123) {
		t.Fatal("wrong delete call")
	}
}

func TestDeleteTextRetriesAreIdempotentAndErrorsAreSafe(t *testing.T) {
	for _, description := range []string{"Bad Request: message to delete not found", "private content from provider"} {
		b, ch := newBotAPI(t)
		b.respond = func(string) (int, string) {
			return http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"` + description + `"}`
		}
		err := ch.DeleteText(t.Context(), channel.Contact{Address: "1000001"}, "123")
		if strings.Contains(description, "not found") {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe error %v", err)
		}
	}
}

func TestDeleteTextHonorsCancellation(t *testing.T) {
	_, ch := newBotAPI(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ch.DeleteText(ctx, channel.Contact{Address: "1000001"}, "123"); err == nil {
		t.Fatal("canceled delete succeeded")
	}
}
