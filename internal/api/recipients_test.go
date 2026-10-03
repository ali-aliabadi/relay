package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestRecipientsCRUD(t *testing.T) {
	h := newV1(t)
	res := h.do("POST", "/v1/recipients", `{"username":"ali","display_name":"Ali","timezone":"Europe/Berlin"}`)
	if res.status != http.StatusCreated || res.body["username"] != "ali" || res.body["timezone"] != "Europe/Berlin" {
		t.Fatalf("create = %d %s", res.status, res.raw)
	}
	if pref, _ := res.body["channel_preference"].([]any); len(pref) != 1 || pref[0] != "telegram" {
		t.Errorf("default preference = %v", res.body["channel_preference"])
	}
	if res := h.do("POST", "/v1/recipients", `{"username":"ali","display_name":"Again"}`); res.status != http.StatusConflict {
		t.Errorf("duplicate = %d %s", res.status, res.raw)
	}
	if res := h.do("POST", "/v1/recipients", `{"username":"Bad Name","display_name":"x"}`); res.status != http.StatusUnprocessableEntity {
		t.Errorf("invalid = %d %s", res.status, res.raw)
	}

	res = h.do("PUT", "/v1/recipients/ali", `{"display_name":"Ali A","timezone":"UTC","channel_preference":["telegram"]}`)
	if res.status != http.StatusOK || res.body["display_name"] != "Ali A" {
		t.Errorf("update = %d %s", res.status, res.raw)
	}
	if res := h.do("PUT", "/v1/recipients/ali", `{"username":"other","display_name":"x","timezone":"UTC","channel_preference":["telegram"]}`); res.status != http.StatusUnprocessableEntity {
		t.Errorf("rename = %d %s", res.status, res.raw)
	}
	if res := h.do("PUT", "/v1/recipients/nobody", `{"display_name":"x","timezone":"UTC","channel_preference":["telegram"]}`); res.status != http.StatusNotFound {
		t.Errorf("update missing = %d", res.status)
	}

	res = h.do("GET", "/v1/recipients/ali", "")
	if res.status != http.StatusOK || res.body["display_name"] != "Ali A" {
		t.Errorf("get = %d %s", res.status, res.raw)
	}
	res = h.do("GET", "/v1/recipients", "")
	if list, _ := res.body["recipients"].([]any); res.status != http.StatusOK || len(list) != 1 {
		t.Errorf("list = %d %s", res.status, res.raw)
	}
	if res := h.do("DELETE", "/v1/recipients/ali", ""); res.status != http.StatusNoContent {
		t.Errorf("delete = %d %s", res.status, res.raw)
	}
	if res := h.do("GET", "/v1/recipients/ali", ""); res.status != http.StatusNotFound || res.errCode() != "not_found" {
		t.Errorf("get deleted = %d %s", res.status, res.raw)
	}
}

func TestRecipientLinkedChannelsHideAddress(t *testing.T) {
	h := newV1(t)
	h.linkedRecipient()
	res := h.do("GET", "/v1/recipients/ali", "")
	if linked, _ := res.body["linked_channels"].([]any); len(linked) != 1 || linked[0] != "telegram" {
		t.Errorf("linked_channels = %v", res.body["linked_channels"])
	}
	if strings.Contains(res.raw, "1000001") {
		t.Errorf("contact address exposed: %s", res.raw)
	}
}

func TestStrictJSON(t *testing.T) {
	h := newV1(t)
	tests := []struct {
		name, body string
		status     int
		code       string
	}{
		{"unknown field", `{"username":"ali","display_name":"A","admin":true}`, 400, "invalid_json"},
		{"wrong type", `{"username":5}`, 400, "invalid_json"},
		{"not json", `username=ali`, 400, "invalid_json"},
		{"two objects", `{"username":"a","display_name":"A"}{}`, 400, "invalid_json"},
		{"empty", ``, 400, "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := h.do("POST", "/v1/recipients", tt.body)
			if res.status != tt.status || res.errCode() != tt.code {
				t.Errorf("= %d %s", res.status, res.raw)
			}
			if strings.Contains(res.raw, "admin") {
				t.Errorf("unknown field name echoed: %s", res.raw)
			}
		})
	}
}

func TestRecipientsNeedAuth(t *testing.T) {
	h := newV1(t)
	for _, path := range []string{"/v1/recipients", "/v1/recipients/ali", "/v1/messages", "/v1/messages/msg_x"} {
		if res := h.doKey("", "GET", path, ""); res.status != http.StatusUnauthorized {
			t.Errorf("GET %s without key = %d", path, res.status)
		}
	}
}
