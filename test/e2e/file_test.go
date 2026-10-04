//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestFileEndToEnd sends a file block through the real image and checks the
// fake Bot API received it as an uploaded document.
func TestFileEndToEnd(t *testing.T) {
	s := startStack(t)
	if code, body := s.api(http.MethodPost, "/v1/recipients", `{"username":"ali","display_name":"Ali"}`); code != http.StatusCreated {
		t.Fatalf("create recipient = %d %v", code, body)
	}
	s.invite("ali", "ali_e2e")

	data := bytes.Repeat([]byte("e2e file "), 1000)
	body := `{"to":["ali"],"blocks":[{"type":"file","filename":"e2e.txt","content_type":"text/plain",` +
		`"base64":"` + base64.StdEncoding.EncodeToString(data) + `"}]}`
	code, created := s.api(http.MethodPost, "/v1/messages", body)
	if code != http.StatusAccepted {
		t.Fatalf("POST /v1/messages = %d %v", code, created)
	}
	id, _ := created["id"].(string)
	s.waitDelivered(id, 15*time.Second)

	var docs []struct {
		Form map[string]string `json:"form"`
		File int               `json:"file_bytes"`
	}
	if err := json.Unmarshal(s.control(http.MethodGet, "/_calls?method=sendDocument", ""), &docs); err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].File != len(data) || docs[0].Form["chat_id"] != fakeChatID {
		t.Fatalf("sendDocument calls = %+v", docs)
	}
}
