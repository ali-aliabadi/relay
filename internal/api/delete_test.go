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

func TestDeleteTextOwnershipDestinationAndRetry(t *testing.T) {
	h := newV1(t)
	recipient := h.linkedRecipient()
	verified := time.Now().Add(-time.Hour)
	if err := h.st.UpsertContact(t.Context(), store.Contact{RecipientID: recipient.ID, Channel: "telegram", Address: "1000001", VerifiedAt: &verified}); err != nil {
		t.Fatal(err)
	}
	id := h.do("POST", "/v1/messages", `{"to":["ali"],"text":"private progress"}`).body["id"].(string)
	path := "/v1/messages/" + id + "/text"
	if r := h.do("DELETE", path, ""); r.status != http.StatusConflict {
		t.Fatal(r.raw)
	}
	w := &core.Worker{Store: h.st, Channels: []channel.Channel{h.ch}, Logger: h.srvLogger(), Clock: time.Now}
	if _, err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, key, err := core.NewClients(h.st).Create(t.Context(), "other-app")
	if err != nil {
		t.Fatal(err)
	}
	if r := h.doKey(key, "DELETE", path, ""); r.status != http.StatusNotFound {
		t.Fatal(r.raw)
	}
	for range 2 {
		if r := h.do("DELETE", path, ""); r.status != http.StatusNoContent {
			t.Fatal(r.raw)
		}
	}
	if len(h.ch.Deleted()) != 2 || len(h.ch.Sent()) != 1 {
		t.Fatal("deletion sent a new message")
	}
	verified = time.Now().Add(time.Minute)
	if err := h.st.UpsertContact(t.Context(), store.Contact{RecipientID: recipient.ID, Channel: "telegram", Address: "2000002", VerifiedAt: &verified}); err != nil {
		t.Fatal(err)
	}
	if r := h.do("DELETE", path, ""); r.status != http.StatusConflict {
		t.Fatal(r.raw)
	}
	if len(h.ch.Deleted()) != 2 || strings.Contains(h.logs.String(), "private progress") {
		t.Fatal("destination guard or privacy failed")
	}
}

func TestDeleteTextRefusesQuestionsAndRedactedContent(t *testing.T) {
	h := newV1(t)
	recipient := h.linkedRecipient()
	verified := time.Now().Add(-time.Hour)
	if err := h.st.UpsertContact(t.Context(), store.Contact{RecipientID: recipient.ID, Channel: "telegram", Address: "1000001", VerifiedAt: &verified}); err != nil {
		t.Fatal(err)
	}
	id := h.do("POST", "/v1/messages", `{"to":["ali"],"blocks":[{"type":"question","text":"Pick?"}]}`).body["id"].(string)
	w := &core.Worker{Store: h.st, Channels: []channel.Channel{h.ch}, Logger: h.srvLogger(), Clock: time.Now}
	if _, err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r := h.do("DELETE", "/v1/messages/"+id+"/text", ""); r.status != http.StatusConflict {
		t.Fatal(r.raw)
	}
	id = h.do("POST", "/v1/messages", `{"to":["ali"],"text":"private progress"}`).body["id"].(string)
	if _, err := w.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	path := "/v1/messages/" + id + "/text"
	h.ch.FailNext(channel.Transient("private provider content"))
	r := h.do("DELETE", path, "")
	if r.status != http.StatusBadGateway || strings.Contains(r.raw+h.logs.String(), "private") {
		t.Fatal("provider failure leaked content")
	}
	if _, err := h.st.Purge(t.Context(), time.Now().Add(time.Hour), time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if r := h.do("DELETE", path, ""); r.status != http.StatusConflict {
		t.Fatal(r.raw)
	}
	if len(h.ch.Deleted()) != 0 {
		t.Fatal("protected messages deleted")
	}
}
