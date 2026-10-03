package api

import (
	"net/http"
	"testing"
)

func TestPreview(t *testing.T) {
	h := newV1(t)
	res := h.do("POST", "/v1/preview", `{"to":["anyone"],"title":"T","text":"hello"}`)
	chans, _ := res.body["channels"].(map[string]any)
	tg, _ := chans["telegram"].(map[string]any)
	if res.status != http.StatusOK || tg["parts"] == nil {
		t.Fatalf("preview = %d %s", res.status, res.raw)
	}
	if res := h.do("GET", "/v1/messages", ""); len(res.body["messages"].([]any)) != 0 {
		t.Error("preview stored a message")
	}
	if res := h.do("POST", "/v1/preview", `{"to":["a"],"text":"x","channels":["sms"]}`); res.status != http.StatusUnprocessableEntity {
		t.Errorf("unconfigured channel = %d", res.status)
	}
	if res := h.do("POST", "/v1/preview", `{"to":["a"]}`); res.status != http.StatusUnprocessableEntity {
		t.Errorf("no content = %d", res.status)
	}
}

func TestChannels(t *testing.T) {
	h := newV1(t)
	res := h.do("GET", "/v1/channels", "")
	list, _ := res.body["channels"].([]any)
	if res.status != http.StatusOK || len(list) != 1 {
		t.Fatalf("channels = %d %s", res.status, res.raw)
	}
	if c := list[0].(map[string]any); c["name"] != "telegram" || c["healthy"] != true {
		t.Errorf("channel = %v", c)
	}
}
