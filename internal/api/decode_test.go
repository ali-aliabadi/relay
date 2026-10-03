package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/message"
)

// FuzzDecodeMessage: whatever the body, decodeJSON either accepts it or
// writes a well-formed error that never quotes the caller's input.
func FuzzDecodeMessage(f *testing.F) {
	const marker = "PRIVATEzq"
	f.Add(`{"to":["ali"],"text":"hi"}`)
	f.Add(`{"to":["ali"],"text":"PRIVATEzq"}`)
	f.Add(`{"PRIVATEzq":1}`)
	f.Add(`{"to":"PRIVATEzq"}`)
	f.Add(`{"blocks":[{"type":"text","PRIVATEzq":"x"}]}`)
	f.Add(`{"to":["ali"]} {"PRIVATEzq":1}`)
	f.Fuzz(func(t *testing.T, body string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/messages", strings.NewReader(body))
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var req message.Request
		if decodeJSON(w, r, &req) {
			return
		}
		if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d", w.Code)
		}
		var resp struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Error.Code == "" {
			t.Fatalf("malformed error body %q", w.Body.String())
		}
		if strings.Contains(body, marker) && strings.Contains(w.Body.String(), marker) {
			t.Fatalf("error echoes input: %s", w.Body.String())
		}
	})
}
