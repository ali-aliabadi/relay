package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/crypto"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

// v1 is an API test harness over a real SQLite store.
type v1 struct {
	t    *testing.T
	srv  *httptest.Server
	st   *store.Store
	key  string
	logs *bytes.Buffer
}

func newV1(t *testing.T) *v1 {
	t.Helper()
	conn, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := store.Migrate(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	c, _ := crypto.New(bytes.Repeat([]byte{5}, crypto.KeySize))
	st := store.New(conn, c, time.Now)
	clients := core.NewClients(st)
	_, key, err := clients.Create(t.Context(), "test-app")
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	srv := httptest.NewServer(NewHandler(Deps{
		Logger: obs.NewLogger(logs, slog.LevelDebug), Clock: time.Now, Health: st.Ping, Auth: clients,
		Recipients: core.NewRecipients(st), Messages: core.NewMessages(st, []string{"telegram"}),
		MaxBodyBytes: 7 << 20,
	}))
	t.Cleanup(srv.Close)
	return &v1{t: t, srv: srv, st: st, key: key, logs: logs}
}

// linkedRecipient creates recipient "ali" with a telegram contact.
func (h *v1) linkedRecipient() store.Recipient {
	const username = "ali"
	h.t.Helper()
	r, err := h.st.CreateRecipient(h.t.Context(), store.Recipient{Username: username, DisplayName: username})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.st.UpsertContact(h.t.Context(), store.Contact{RecipientID: r.ID, Channel: "telegram", Address: "1000001"}); err != nil {
		h.t.Fatal(err)
	}
	return r
}

type result struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (h *v1) do(method, path, body string) result {
	return h.doKey(h.key, method, path, body)
}

func (h *v1) doKey(key, method, path, body string) result {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.srv.URL+path, rd)
	if err != nil {
		h.t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	res := result{status: resp.StatusCode, header: resp.Header, raw: string(raw)}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &res.body); err != nil {
			h.t.Fatalf("%s %s: response is not JSON: %s", method, path, raw)
		}
	}
	return res
}

func (r result) errCode() string {
	e, _ := r.body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func storeRecipient(username string) store.Recipient {
	return store.Recipient{Username: username, DisplayName: username}
}
