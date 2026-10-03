package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ali-aliabadi/relay/internal/core"
)

func TestCreateAndGetMessage(t *testing.T) {
	h := newV1(t)
	ali := h.linkedRecipient()
	png := base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...))
	body := `{"to":["ali"],"urgency":"high","source":"backup-script","title":"Backup failed",
		"blocks":[{"type":"text","text":"Nightly backup stopped."},
		          {"type":"image","base64":"` + png + `","content_type":"image/png"}]}`
	res := h.do("POST", "/v1/messages", body)
	if res.status != http.StatusAccepted || res.body["status"] != "queued" {
		t.Fatalf("create = %d %s", res.status, res.raw)
	}
	id, _ := res.body["id"].(string)
	if !strings.HasPrefix(id, "msg_") {
		t.Fatalf("id = %q", id)
	}

	res = h.do("GET", "/v1/messages/"+id, "")
	if res.status != http.StatusOK || res.body["urgency"] != "high" || res.body["source"] != "backup-script" ||
		res.body["request_id"] == "" || res.body["redacted"] != false {
		t.Fatalf("get = %d %s", res.status, res.raw)
	}
	ds, _ := res.body["deliveries"].([]any)
	if len(ds) != 1 {
		t.Fatalf("deliveries = %v", ds)
	}
	d, _ := ds[0].(map[string]any)
	if d["channel"] != "telegram" || d["status"] != "queued" || d["recipient_id"] != ali.ID || d["next_attempt_at"] == nil {
		t.Errorf("delivery = %v", d)
	}
	if strings.Contains(res.raw, "Backup failed") || strings.Contains(res.raw, "Nightly") {
		t.Errorf("GET returns content: %s", res.raw)
	}

	msg, err := h.st.Message(t.Context(), mustClientID(t, h), id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(msg.Blocks), png) || !strings.Contains(string(msg.Blocks), `"attachment":0`) {
		t.Errorf("stored blocks = %s", msg.Blocks)
	}
	atts, err := h.st.Attachments(t.Context(), id)
	if err != nil || len(atts) != 1 || atts[0].ContentType != "image/png" {
		t.Errorf("attachments = %v, %v", atts, err)
	}
	for _, leaked := range []string{"Backup failed", "Nightly", "1000001"} {
		if strings.Contains(h.logs.String(), leaked) {
			t.Errorf("logs contain %q", leaked)
		}
	}
}

func mustClientID(t *testing.T, h *v1) string {
	t.Helper()
	c, err := core.NewClients(h.st).Authenticate(t.Context(), h.key)
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func TestTextShorthand(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	if res := h.do("POST", "/v1/messages", `{"to":["ali"],"text":"Deploy done: v1.2 is live"}`); res.status != http.StatusAccepted {
		t.Fatalf("= %d %s", res.status, res.raw)
	}
}

func TestIdempotency(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	body := `{"to":["ali"],"text":"once","idempotency_key":"job-42"}`
	first := h.do("POST", "/v1/messages", body)
	second := h.do("POST", "/v1/messages", body)
	if first.status != http.StatusAccepted || second.status != http.StatusOK || first.body["id"] != second.body["id"] {
		t.Fatalf("first %d %s, second %d %s", first.status, first.raw, second.status, second.raw)
	}
	res := h.do("GET", "/v1/messages", "")
	if ms, _ := res.body["messages"].([]any); len(ms) != 1 {
		t.Errorf("messages after retry = %d", len(ms))
	}
}

func TestCreateMessageRejects(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	if _, err := h.st.CreateRecipient(t.Context(), storeRecipient("unlinked")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, body, want string
	}{
		{"unknown recipient", `{"to":["ali","ghost-user"],"text":"x"}`, "to[1]: unknown recipient"},
		{"unlinked recipient", `{"to":["unlinked"],"text":"x"}`, "to[0]: recipient has no linked channel"},
		{"unconfigured channel", `{"to":["ali"],"text":"x","channels":["sms"]}`, "channels[0]: not a configured channel"},
		{"bad block", `{"to":["ali"],"blocks":[{"type":"html","text":"<b>"}]}`, "blocks[0].type"},
		{"http link", `{"to":["ali"],"blocks":[{"type":"link","text":"x","url":"http://x.y"}]}`, "https URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := h.do("POST", "/v1/messages", tt.body)
			if res.status != http.StatusUnprocessableEntity || res.errCode() != "invalid_request" || !strings.Contains(res.raw, tt.want) {
				t.Errorf("= %d %s, want %q", res.status, res.raw, tt.want)
			}
			if strings.Contains(res.raw, "ghost-user") {
				t.Errorf("username echoed: %s", res.raw)
			}
		})
	}
	if res := h.do("GET", "/v1/messages", ""); len(res.body["messages"].([]any)) != 0 {
		t.Error("rejected requests stored messages")
	}
}

func TestMessagesAreScopedToClient(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	id := h.do("POST", "/v1/messages", `{"to":["ali"],"text":"mine"}`).body["id"].(string)
	_, otherKey, err := core.NewClients(h.st).Create(t.Context(), "other-app")
	if err != nil {
		t.Fatal(err)
	}
	if res := h.doKey(otherKey, "GET", "/v1/messages/"+id, ""); res.status != http.StatusNotFound {
		t.Errorf("other client GET = %d", res.status)
	}
	if res := h.doKey(otherKey, "GET", "/v1/messages", ""); len(res.body["messages"].([]any)) != 0 {
		t.Errorf("other client list = %s", res.raw)
	}
}

func TestListMessagesPagination(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	var ids []string
	for i := range 5 {
		res := h.do("POST", "/v1/messages", fmt.Sprintf(`{"to":["ali"],"text":"m%d"}`, i))
		ids = append(ids, res.body["id"].(string))
	}
	page := h.do("GET", "/v1/messages?limit=2", "")
	ms, _ := page.body["messages"].([]any)
	cursor, _ := page.body["next_cursor"].(string)
	if len(ms) != 2 || ms[0].(map[string]any)["id"] != ids[4] || cursor != ids[3] {
		t.Fatalf("page 1 = %s", page.raw)
	}
	seen := 2
	for cursor != "" {
		page = h.do("GET", "/v1/messages?limit=2&cursor="+cursor, "")
		ms, _ = page.body["messages"].([]any)
		seen += len(ms)
		cursor, _ = page.body["next_cursor"].(string)
	}
	if seen != 5 {
		t.Errorf("paged through %d messages, want 5", seen)
	}
	if res := h.do("GET", "/v1/messages?status=queued&since=2020-01-01T00:00:00Z", ""); len(res.body["messages"].([]any)) != 5 {
		t.Errorf("filtered list = %s", res.raw)
	}
	for _, q := range []string{"status=lost", "since=yesterday", "limit=0", "limit=101", "limit=x"} {
		if res := h.do("GET", "/v1/messages?"+q, ""); res.status != http.StatusUnprocessableEntity {
			t.Errorf("?%s = %d", q, res.status)
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	h := newV1(t)
	big := `{"to":["ali"],"text":"` + strings.Repeat("x", 8<<20) + `"}`
	if res := h.do("POST", "/v1/messages", big); res.status != http.StatusRequestEntityTooLarge {
		t.Errorf("= %d %s", res.status, res.raw[:min(len(res.raw), 200)])
	}
}

func TestMessageNotFound(t *testing.T) {
	h := newV1(t)
	if res := h.do("GET", "/v1/messages/msg_nope", ""); res.status != http.StatusNotFound {
		t.Errorf("= %d", res.status)
	}
}
