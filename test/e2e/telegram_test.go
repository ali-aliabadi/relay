//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// A made-up token and chat ID: the fake accepts only this token.
const (
	fakeToken  = "123456:e2e-fake-token"
	fakeChatID = "4242"
)

// stack is Relay plus the fake Telegram Bot API on a private network.
type stack struct {
	t      *testing.T
	relay  testcontainers.Container
	fake   string // fake Telegram base URL from the host
	apiKey string
}

func startStack(t *testing.T) *stack {
	t.Helper()
	image := os.Getenv("RELAY_E2E_FAKE_TELEGRAM_IMAGE")
	if image == "" {
		t.Fatal("RELAY_E2E_FAKE_TELEGRAM_IMAGE is not set; run `make test-e2e`")
	}
	nw, err := network.New(t.Context())
	testcontainers.CleanupNetwork(t, nw)
	if err != nil {
		t.Fatal(err)
	}
	fake, err := testcontainers.Run(t.Context(), image,
		testcontainers.WithExposedPorts("8081/tcp"),
		testcontainers.WithEnv(map[string]string{"FAKE_TELEGRAM_TOKEN": fakeToken}),
		network.WithNetwork([]string{"telegram"}, nw),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/_health").WithPort("8081/tcp").
			WithStartupTimeout(30*time.Second)),
	)
	testcontainers.CleanupContainer(t, fake)
	if err != nil {
		t.Fatalf("starting fake telegram: %v", err)
	}
	fakeURL, err := fake.PortEndpoint(t.Context(), "8081/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	relay := startRelay(t,
		network.WithNetwork([]string{"relay"}, nw),
		testcontainers.WithEnv(map[string]string{
			"RELAY_TELEGRAM_BOT_TOKEN":   fakeToken,
			"RELAY_TELEGRAM_API_URL":     "http://telegram:8081",
			"RELAY_WORKER_POLL_INTERVAL": "200ms",
		}),
	)
	s := &stack{t: t, relay: relay, fake: fakeURL}
	out := s.exec("clients", "create", "e2e")
	s.apiKey = regexp.MustCompile(`rk_[A-Za-z0-9_-]{43}`).FindString(out)
	if s.apiKey == "" {
		t.Fatalf("no key in: %s", out)
	}
	return s
}

// exec runs the relay CLI inside the container and fails on a non-zero exit.
func (s *stack) exec(args ...string) string {
	s.t.Helper()
	code, r, err := s.relay.Exec(s.t.Context(), append([]string{"/usr/local/bin/relay"}, args...), exec.Multiplexed())
	if err != nil {
		s.t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	if code != 0 {
		s.t.Fatalf("relay %v = %d: %s", args, code, out)
	}
	return string(out)
}

// api calls the public API. The endpoint is looked up each time because a
// restarted container gets a new host port.
func (s *stack) api(method, path, body string) (int, map[string]any) {
	s.t.Helper()
	endpoint, err := s.relay.PortEndpoint(s.t.Context(), "8080/tcp", "http")
	if err != nil {
		s.t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(s.t.Context(), method, endpoint+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// control calls one of the fake's /_ endpoints.
func (s *stack) control(method, path, body string) []byte {
	s.t.Helper()
	req, _ := http.NewRequestWithContext(s.t.Context(), method, s.fake+path, bytes.NewBufferString(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("fake %s: %v", path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return out
}

// sends counts sendMessage calls the fake received for the test chat.
func (s *stack) sends() int {
	s.t.Helper()
	var calls []struct {
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(s.control(http.MethodGet, "/_calls?method=sendMessage", ""), &calls); err != nil {
		s.t.Fatal(err)
	}
	n := 0
	for _, c := range calls {
		if fmt.Sprint(c.Params["chat_id"]) == fakeChatID {
			n++
		}
	}
	return n
}

// link runs `relay recipients link --yes` while the "user" keeps sending /start.
func (s *stack) link(username string) {
	s.t.Helper()
	done := make(chan string, 1)
	go func() {
		code, r, err := s.relay.Exec(s.t.Context(),
			[]string{"/usr/local/bin/relay", "recipients", "link", username, "--yes"}, exec.Multiplexed())
		out, _ := io.ReadAll(r)
		if err != nil || code != 0 {
			done <- "failed: " + string(out)
			return
		}
		done <- string(out)
	}()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(time.Minute)
	for {
		select {
		case out := <-done:
			if strings.HasPrefix(out, "failed: ") {
				s.t.Fatalf("link %s", out)
			}
			if strings.Contains(out, fakeChatID) {
				s.t.Errorf("link output prints the chat ID: %s", out)
			}
			return
		case <-tick.C:
			s.control(http.MethodPost, "/_start?chat_id="+fakeChatID+"&name=Ali&username=ali_e2e", "")
		case <-deadline:
			s.t.Fatal("link did not finish")
		}
	}
}

// waitStatus polls a message until it reaches want.
func (s *stack) waitStatus(id, want string, within time.Duration) map[string]any {
	s.t.Helper()
	deadline := time.Now().Add(within)
	for {
		_, msg := s.api(http.MethodGet, "/v1/messages/"+id, "")
		if msg["status"] == want {
			return msg
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("message %s status = %v, want %s: %v", id, msg["status"], want, msg)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestTelegramDeliveryEndToEnd covers link, delivery, retry, idempotency and a
// restart against the real image and a fake Telegram container.
func TestTelegramDeliveryEndToEnd(t *testing.T) {
	s := startStack(t)
	if code, body := s.api(http.MethodPost, "/v1/recipients", `{"username":"ali","display_name":"Ali"}`); code != http.StatusCreated {
		t.Fatalf("create recipient = %d %v", code, body)
	}
	s.link("ali")
	linked := s.sends() // link sends a confirmation to the chat

	// Delivery, sent once even when the same idempotency key is posted twice.
	body := `{"to":["ali"],"title":"Backup","text":"done <ok>","idempotency_key":"e2e-1"}`
	code, first := s.api(http.MethodPost, "/v1/messages", body)
	if code != http.StatusAccepted {
		t.Fatalf("POST /v1/messages = %d %v", code, first)
	}
	id, _ := first["id"].(string)
	if code, again := s.api(http.MethodPost, "/v1/messages", body); code != http.StatusOK || again["id"] != id {
		t.Fatalf("idempotent retry = %d %v", code, again)
	}
	s.waitStatus(id, "delivered", 15*time.Second)
	if n := s.sends() - linked; n != 1 {
		t.Fatalf("sendMessage calls for the message = %d, want 1", n)
	}

	// A 502 from Telegram is retried after the first backoff step (10s).
	s.control(http.MethodPost, "/_fail", `[{"status":502,"description":"Bad Gateway"}]`)
	_, second := s.api(http.MethodPost, "/v1/messages", `{"to":["ali"],"text":"retry me"}`)
	id2, _ := second["id"].(string)
	msg := s.waitStatus(id2, "delivered", 40*time.Second)
	if d := msg["deliveries"].([]any)[0].(map[string]any); d["attempts"] != float64(2) {
		t.Errorf("attempts = %v, want 2", d["attempts"])
	}

	// Data survives a restart (the /data volume), and the API key still works.
	timeout := 10 * time.Second
	if err := s.relay.Stop(context.WithoutCancel(t.Context()), &timeout); err != nil {
		t.Fatal(err)
	}
	if err := s.relay.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if code, got := s.api(http.MethodGet, "/v1/messages/"+id, ""); code != http.StatusOK || got["status"] != "delivered" {
		t.Fatalf("after restart = %d %v", code, got)
	}

	// Logs carry IDs, never content or the chat ID.
	logs, err := s.relay.Logs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(logs)
	for _, secret := range []string{"Backup", "done <ok>", "retry me", fakeToken, `"` + fakeChatID + `"`} {
		if bytes.Contains(out, []byte(secret)) {
			t.Errorf("container logs contain %q", secret)
		}
	}
	if !bytes.Contains(out, []byte(id)) {
		t.Errorf("container logs never mention message %s", id)
	}
}
