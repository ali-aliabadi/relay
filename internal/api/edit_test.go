package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

func TestEditStatusMessageOwnershipDeliveryAndDestination(t *testing.T) {
	h := newV1(t)
	recipient := h.linkedRecipient()
	verified := time.Now().Add(-time.Hour)
	if err := h.st.UpsertContact(t.Context(), store.Contact{RecipientID: recipient.ID, Channel: "telegram", Address: "1000001", VerifiedAt: &verified}); err != nil {
		t.Fatal(err)
	}
	id := h.do("POST", "/v1/messages", `{"to":["ali"],"urgency":"low","title":"Progress","text":"Starting"}`).body["id"].(string)
	path := "/v1/messages/" + id + "/text"
	if r := h.do("PATCH", path, `{"text":"42%"}`); r.status != http.StatusConflict {
		t.Fatal(r.raw)
	}
	w := &core.Worker{Store: h.st, Channels: []channel.Channel{h.ch}, Logger: h.srvLogger(), Clock: time.Now}
	if _, err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r := h.do("PATCH", path, `{"text":"42%"}`); r.status != http.StatusOK {
		t.Fatal(r.raw)
	}
	if len(h.ch.Sent()) != 1 || len(h.ch.Edits()) != 1 || h.ch.Edits()[0].Msg.Title != "Progress" {
		t.Fatal("edit sent another message or lost title")
	}
	_, otherKey, err := core.NewClients(h.st).Create(t.Context(), "other-app")
	if err != nil {
		t.Fatal(err)
	}
	if r := h.doKey(otherKey, "PATCH", path, `{"text":"private replacement"}`); r.status != http.StatusNotFound {
		t.Fatal(r.raw)
	}
	verified = time.Now().Add(time.Minute)
	if err := h.st.UpsertContact(t.Context(), store.Contact{RecipientID: recipient.ID, Channel: "telegram", Address: "2000002", VerifiedAt: &verified}); err != nil {
		t.Fatal(err)
	}
	if r := h.do("PATCH", path, `{"text":"new destination"}`); r.status != http.StatusConflict {
		t.Fatal(r.raw)
	}
	if len(h.ch.Edits()) != 1 || strings.Contains(h.logs.String(), "42%") {
		t.Fatal("edit escaped destination guard or leaked content")
	}
}

func TestEditTextValidatesInputAndRefusesQuestionMessages(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	for _, body := range []string{`{}`, `{"text":""}`, `{"text":"ok","to":["ali"]}`, `null`, `{"text":42}`} {
		if r := h.do("PATCH", "/v1/messages/msg_missing/text", body); r.status == http.StatusOK {
			t.Fatal(r.raw)
		}
	}
	id := h.do("POST", "/v1/messages", `{"to":["ali"],"blocks":[{"type":"question","text":"Pick?"}]}`).body["id"].(string)
	w := &core.Worker{Store: h.st, Channels: []channel.Channel{h.ch}, Logger: h.srvLogger(), Clock: time.Now}
	if _, err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r := h.do("PATCH", "/v1/messages/"+id+"/text", `{"text":"replace question"}`); r.status != http.StatusConflict {
		t.Fatal(r.raw)
	}
}

func TestEditProviderFailureDoesNotLeakContentAndRetentionStillApplies(t *testing.T) {
	h := newV1(t)
	r := h.linkedRecipient()
	verified := time.Now().Add(-time.Hour)
	if err := h.st.UpsertContact(t.Context(), store.Contact{RecipientID: r.ID, Channel: "telegram", Address: "1000001", VerifiedAt: &verified}); err != nil {
		t.Fatal(err)
	}
	id := h.do("POST", "/v1/messages", `{"to":["ali"],"text":"starting"}`).body["id"].(string)
	w := &core.Worker{Store: h.st, Channels: []channel.Channel{h.ch}, Logger: h.srvLogger(), Clock: time.Now}
	if _, err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.ch.FailNext(channel.Transient("private provider text"))
	path := "/v1/messages/" + id + "/text"
	result := h.do("PATCH", path, `{"text":"private replacement"}`)
	if result.status != http.StatusBadGateway || strings.Contains(result.raw+h.logs.String(), "private") {
		t.Fatal("edit failure leaked content")
	}
	if _, err := h.st.Purge(t.Context(), time.Now().Add(time.Hour), time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if result := h.do("PATCH", path, `{"text":"42%"}`); result.status != http.StatusConflict {
		t.Fatal(result.raw)
	}
}
