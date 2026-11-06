//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestProgressEditsReuseTheDeliveredMessage(t *testing.T) {
	s := startStack(t)
	if code, result := s.api(http.MethodPost, "/v1/recipients", `{"username":"ali","display_name":"Ali"}`); code != http.StatusCreated {
		t.Fatalf("recipient: %d %v", code, result)
	}
	s.link("ali")
	code, created := s.api(http.MethodPost, "/v1/messages", `{"to":["ali"],"urgency":"low","text":"Starting video"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create = %d", code)
	}
	id := created["id"].(string)
	s.waitDelivered(id, 15*time.Second)
	before := s.sends()
	for _, text := range []string{"Rendering 42%", "Rendering 70%"} {
		body, _ := json.Marshal(map[string]string{"text": text})
		if code, result := s.api(http.MethodPatch, "/v1/messages/"+id+"/text", string(body)); code != http.StatusOK {
			t.Fatalf("edit = %d %v", code, result)
		}
	}
	var edits []fakeCall
	if err := json.Unmarshal(s.control(http.MethodGet, "/_calls?method=editMessageText", ""), &edits); err != nil {
		t.Fatal(err)
	}
	if len(edits) != 2 || s.sends() != before || fmt.Sprint(edits[0].Params["message_id"]) != fmt.Sprint(edits[1].Params["message_id"]) {
		t.Fatal("progress did not edit the same message")
	}
}
