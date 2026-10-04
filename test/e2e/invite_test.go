//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

const inviteeChatID = "5151"

// waitSent waits for the bot to send chat a message matching re and returns
// the submatches.
func (s *stack) waitSent(chat string, re *regexp.Regexp) []string {
	s.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		for _, c := range s.sentMessages() {
			if m := re.FindStringSubmatch(fmt.Sprint(c.Params["text"])); m != nil && fmt.Sprint(c.Params["chat_id"]) == chat {
				return m
			}
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("the bot never sent chat %s a message matching %s", chat, re)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestInviteLinkEndToEnd: the admin asks the bot for an invite link, the
// invitee opens it and is linked, and messages then reach them.
func TestInviteLinkEndToEnd(t *testing.T) {
	s := startStack(t)
	if code, body := s.api(http.MethodPost, "/v1/recipients", `{"username":"ali","display_name":"Ali"}`); code != http.StatusCreated {
		t.Fatalf("create recipient = %d %v", code, body)
	}
	s.exec("recipients", "alias", "ali", "admin")
	s.link("ali")

	s.control(http.MethodPost, "/_reply?chat_id="+fakeChatID+"&text="+url.QueryEscape("/invite sara Sara"), "")
	token := s.waitSent(fakeChatID, regexp.MustCompile(`\?start=([A-Za-z0-9_-]{43})`))[1]

	s.control(http.MethodPost, "/_reply?chat_id="+inviteeChatID+"&text="+url.QueryEscape("/start "+token), "")
	s.waitSent(inviteeChatID, regexp.MustCompile(`^Linked to Relay as Sara\.`))
	s.waitSent(fakeChatID, regexp.MustCompile(`^Sara opened your invite`))

	if code, msg := s.api(http.MethodPost, "/v1/messages", `{"to":["sara"],"text":"welcome"}`); code != http.StatusAccepted {
		t.Fatalf("POST /v1/messages = %d %v", code, msg)
	}
	s.waitSent(inviteeChatID, regexp.MustCompile(`^welcome$`))

	logs, err := s.relay.Logs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(logs)
	for _, secret := range []string{token, `"` + inviteeChatID + `"`, "welcome"} {
		if strings.Contains(string(out), secret) {
			t.Errorf("container logs contain %q", secret)
		}
	}
}
