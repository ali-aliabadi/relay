package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
)

const questionBody = `{"to":["ali"],"blocks":[
	{"type":"text","text":"v2 passed staging."},
	{"type":"question","text":"Ship it?","options":["Yes","No"],"webhook":"https://app.example/hook"}]}`

func TestAnswersEndpoint(t *testing.T) {
	h := newV1(t)
	ali := h.linkedRecipient()
	res := h.do("POST", "/v1/messages", questionBody)
	if res.status != http.StatusAccepted {
		t.Fatalf("create question = %d %s", res.status, res.raw)
	}
	id := res.body["id"].(string)

	res = h.do("GET", "/v1/messages/"+id+"/answers", "")
	if res.status != http.StatusOK || res.raw != `{"answers":[]}`+"\n" {
		t.Fatalf("answers before any reply = %d %q", res.status, res.raw)
	}
	// Polling with no answer must not lock anything: the answer still saves.
	if ok, err := h.st.SaveAnswer(t.Context(), id, ali.ID, "Yes"); !ok || err != nil {
		t.Fatalf("SaveAnswer = %v, %v", ok, err)
	}

	res = h.do("GET", "/v1/messages/"+id+"/answers", "")
	as, _ := res.body["answers"].([]any)
	if res.status != http.StatusOK || len(as) != 1 {
		t.Fatalf("answers = %d %s", res.status, res.raw)
	}
	a := as[0].(map[string]any)
	if a["recipient"] != "ali" || a["answer"] != "Yes" || len(a) != 3 {
		t.Errorf("answer = %v", a)
	}
	if _, err := time.Parse(time.RFC3339Nano, a["answered_at"].(string)); err != nil {
		t.Errorf("answered_at = %v", a["answered_at"])
	}
	if strings.Contains(res.raw, "1000001") || strings.Contains(res.raw, ali.ID) {
		t.Errorf("response exposes the chat or recipient id: %s", res.raw)
	}
	if ok, _ := h.st.SaveAnswer(t.Context(), id, ali.ID, "No"); ok {
		t.Error("answer changed after the app fetched it")
	}
	if res := h.do("GET", "/v1/messages/"+id+"/answers", ""); !strings.Contains(res.raw, `"answer":"Yes"`) {
		t.Errorf("refetch = %s", res.raw)
	}
	if strings.Contains(h.logs.String(), `"answer":"Yes"`) || strings.Contains(h.logs.String(), "Ship it") {
		t.Errorf("logs contain content: %s", h.logs.String())
	}
}

func TestAnswersEndpointAccess(t *testing.T) {
	h := newV1(t)
	ali := h.linkedRecipient()
	id := h.do("POST", "/v1/messages", questionBody).body["id"].(string)
	if _, err := h.st.SaveAnswer(t.Context(), id, ali.ID, "Yes"); err != nil {
		t.Fatal(err)
	}
	_, otherKey, err := core.NewClients(h.st).Create(t.Context(), "other-app")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, key, path string
		status          int
		code            string
	}{
		{"no key", "", "/v1/messages/" + id + "/answers", 401, "unauthorized"},
		{"other client", otherKey, "/v1/messages/" + id + "/answers", 404, "not_found"},
		{"unknown message", h.key, "/v1/messages/msg_01JAAAAAAAAAAAAAAAAAAAAAAA/answers", 404, "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := h.doKey(tt.key, "GET", tt.path, "")
			if res.status != tt.status || res.errCode() != tt.code || strings.Contains(res.raw, "Yes") {
				t.Errorf("= %d %s", res.status, res.raw)
			}
		})
	}
	if res := h.do("POST", "/v1/messages/"+id+"/answers", "{}"); res.status == http.StatusOK {
		t.Errorf("POST answers = %d", res.status)
	}
	// None of the refused reads made the answer final.
	if ok, _ := h.st.SaveAnswer(t.Context(), id, ali.ID, "No"); !ok {
		t.Error("a refused read locked the answer")
	}
}

func TestCreateQuestionValidation(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	res := h.do("POST", "/v1/messages", `{"to":["ali"],"blocks":[
		{"type":"question","text":"SECRET?","options":["a","a"],"webhook":"http://10.0.0.1/hook"},
		{"type":"question","text":"again?"}]}`)
	if res.status != http.StatusUnprocessableEntity || res.errCode() != "invalid_request" {
		t.Fatalf("= %d %s", res.status, res.raw)
	}
	for _, want := range []string{"blocks[0].options[1]: duplicate", "blocks[0].webhook", "at most 1 question"} {
		if !strings.Contains(res.raw, want) {
			t.Errorf("problems miss %q: %s", want, res.raw)
		}
	}
	if strings.Contains(res.raw, "SECRET") || strings.Contains(res.raw, "10.0.0.1") {
		t.Errorf("response echoes input: %s", res.raw)
	}
	res = h.do("POST", "/v1/messages", `{"to":["ali"],"blocks":[{"type":"question","text":"?","choices":["a"]}]}`)
	if res.status != http.StatusBadRequest || res.errCode() != "invalid_json" {
		t.Errorf("unknown question field = %d %s", res.status, res.raw)
	}
}
