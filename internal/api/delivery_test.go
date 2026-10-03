package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/core"
)

// TestAPIToWorkerToChannel: POST /v1/messages, one worker pass over the same
// store, and GET /v1/messages/{id} reports the delivery.
func TestAPIToWorkerToChannel(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	id := h.do("POST", "/v1/messages", `{"to":["ali"],"urgency":"low","title":"T","text":"hello"}`).body["id"].(string)

	w := &core.Worker{Store: h.st, Channels: []channel.Channel{h.ch}, Logger: h.srvLogger(), Clock: time.Now, Poll: time.Second}
	if n, err := w.Tick(t.Context()); n != 1 || err != nil {
		t.Fatalf("Tick = %d, %v", n, err)
	}
	res := h.do("GET", "/v1/messages/"+id, "")
	d := res.body["deliveries"].([]any)[0].(map[string]any)
	if res.status != http.StatusOK || res.body["status"] != "delivered" || d["status"] != "delivered" || d["attempts"] != float64(1) {
		t.Fatalf("after delivery = %s", res.raw)
	}
	sent := h.ch.Sent()
	if len(sent) != 1 || sent[0].Msg.Title != "T" || sent[0].Msg.Urgency != "low" || sent[0].To.Address != "1000001" {
		t.Errorf("sent = %+v", sent)
	}
	if res := h.do("GET", "/v1/messages?status=delivered", ""); len(res.body["messages"].([]any)) != 1 {
		t.Errorf("status filter after delivery = %s", res.raw)
	}
}
