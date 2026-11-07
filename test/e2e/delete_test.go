//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestDeleteProgressCardUsesOriginalProviderMessage(t *testing.T) {
	s := startStack(t)
	if code, result := s.api(http.MethodPost, "/v1/recipients", `{"username":"status-user","display_name":"Status User"}`); code != http.StatusCreated {
		t.Fatalf("recipient: %d %v", code, result)
	}
	s.link("status-user")
	code, created := s.api(http.MethodPost, "/v1/messages", `{"to":["status-user"],"text":"Starting video"}`)
	if code != http.StatusAccepted {
		t.Fatal(code)
	}
	id := created["id"].(string)
	s.waitDelivered(id, 15*time.Second)
	before := s.sends()
	for range 2 {
		if code, result := s.api(http.MethodDelete, "/v1/messages/"+id+"/text", ""); code != http.StatusNoContent {
			t.Fatalf("delete: %d %v", code, result)
		}
	}
	var calls []fakeCall
	if err := json.Unmarshal(s.control(http.MethodGet, "/_calls?method=deleteMessage", ""), &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || s.sends() != before || fmt.Sprint(calls[0].Params["message_id"]) != fmt.Sprint(calls[1].Params["message_id"]) {
		t.Fatal("delete did not reuse original ID")
	}
}
