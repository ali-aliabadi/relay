//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// question finds the sent message holding text and returns it.
func (s *stack) question(text string) fakeCall {
	s.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		for _, c := range s.sentMessages() {
			if strings.Contains(fmt.Sprint(c.Params["text"]), text) {
				return c
			}
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("question %q was never sent", text)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitAnswer polls the answers endpoint until one arrives. Reading an
// answer makes it final, which is what an app polling like this gets.
func (s *stack) waitAnswer(id string) map[string]any {
	s.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		code, body := s.api(http.MethodGet, "/v1/messages/"+id+"/answers", "")
		if code != http.StatusOK {
			s.t.Fatalf("answers = %d %v", code, body)
		}
		if as, _ := body["answers"].([]any); len(as) > 0 {
			return as[0].(map[string]any)
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("no answer for %s", id)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestQuestionEndToEnd: a button answer and a typed answer travel from the
// fake Telegram through the real image to the API, and survive a restart.
func TestQuestionEndToEnd(t *testing.T) {
	s := startStack(t)
	if code, body := s.api(http.MethodPost, "/v1/recipients", `{"username":"ali","display_name":"Ali"}`); code != http.StatusCreated {
		t.Fatalf("create recipient = %d %v", code, body)
	}
	s.link("ali")

	_, created := s.api(http.MethodPost, "/v1/messages", `{"to":["ali"],"blocks":[
		{"type":"question","text":"Ship v2?","options":["Yes","No"]}]}`)
	buttonsID, _ := created["id"].(string)
	sent := s.question("Ship v2?")
	rows := sent.Params["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	data := rows[1].([]any)[0].(map[string]any)["callback_data"].(string)
	s.control(http.MethodPost, fmt.Sprintf("/_tap?chat_id=%s&message_id=%d&data=%s", fakeChatID, sent.MessageID, url.QueryEscape(data)), "")
	if a := s.waitAnswer(buttonsID); a["answer"] != "No" || a["recipient"] != "ali" {
		t.Fatalf("button answer = %v", a)
	}

	_, created = s.api(http.MethodPost, "/v1/messages", `{"to":["ali"],"blocks":[{"type":"question","text":"New hostname?"}]}`)
	typedID, _ := created["id"].(string)
	typed := s.question("New hostname?")
	s.control(http.MethodPost, fmt.Sprintf("/_reply?chat_id=%s&reply_to=%d&text=nas-02", fakeChatID, typed.MessageID), "")
	if a := s.waitAnswer(typedID); a["answer"] != "nas-02" {
		t.Fatalf("typed answer = %v", a)
	}

	// Answers are stored (encrypted) in /data and readable after a restart.
	timeout := 10 * time.Second
	if err := s.relay.Stop(context.WithoutCancel(t.Context()), &timeout); err != nil {
		t.Fatal(err)
	}
	if err := s.relay.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if a := s.waitAnswer(typedID); a["answer"] != "nas-02" {
		t.Errorf("after restart = %v", a)
	}

	logs, err := s.relay.Logs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(logs)
	for _, secret := range []string{"nas-02", "Ship v2", "New hostname", fakeToken, `"` + fakeChatID + `"`} {
		if bytes.Contains(out, []byte(secret)) {
			t.Errorf("container logs contain %q", secret)
		}
	}
	if !bytes.Contains(out, []byte(`"msg":"answer"`)) {
		t.Error("no answer log line")
	}
}
