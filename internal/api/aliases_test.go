package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
)

func TestAliasesInAPI(t *testing.T) {
	h := newV1(t)
	ali := h.linkedRecipient()
	svc := core.NewRecipients(h.st)
	if err := svc.AddAlias(t.Context(), "ali", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}

	res := h.do("GET", "/v1/recipients/ali", "")
	if res.status != http.StatusOK || !strings.Contains(res.raw, `"aliases":["admin"]`) {
		t.Errorf("get = %d %s", res.status, res.raw)
	}
	h.do("POST", "/v1/recipients", `{"username":"tara","display_name":"Tara"}`)
	res = h.do("GET", "/v1/recipients", "")
	if !strings.Contains(res.raw, `"username":"tara","aliases":[]`) {
		t.Errorf("a recipient without aliases should list an empty array: %s", res.raw)
	}

	// The alias is taken: no recipient can be created with that name.
	if res := h.do("POST", "/v1/recipients", `{"username":"admin","display_name":"X"}`); res.status != http.StatusConflict || res.errCode() != "conflict" {
		t.Errorf("recipient named like an alias = %d %s", res.status, res.raw)
	}
	// Aliases are names for sending, not for managing recipients.
	if res := h.do("GET", "/v1/recipients/admin", ""); res.status != http.StatusNotFound {
		t.Errorf("GET by alias = %d", res.status)
	}

	res = h.do("POST", "/v1/messages", `{"to":["admin","ali"],"text":"hi"}`)
	if res.status != http.StatusAccepted {
		t.Fatalf("send to alias = %d %s", res.status, res.raw)
	}
	res = h.do("GET", "/v1/messages/"+res.body["id"].(string), "")
	ds, _ := res.body["deliveries"].([]any)
	if len(ds) != 1 || ds[0].(map[string]any)["recipient_id"] != ali.ID {
		t.Errorf("deliveries = %v; want one, to ali", ds)
	}
	res = h.do("POST", "/v1/messages", `{"to":["boss"],"text":"hi"}`)
	if res.status != http.StatusUnprocessableEntity || !strings.Contains(res.raw, "to[0]: unknown recipient") || strings.Contains(res.raw, "boss") {
		t.Errorf("unknown alias = %d %s", res.status, res.raw)
	}
}
