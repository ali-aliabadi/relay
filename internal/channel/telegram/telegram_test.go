package telegram

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/obs"
)

const testToken = "123456:fake-test-token"

// botAPI mimics the parts of the Bot API Relay uses.
type botAPI struct {
	t        *testing.T
	mu       sync.Mutex
	calls    []call
	respond  func(method string) (int, string)
	nextID   int
	gotToken string
}

type call struct {
	Method      string
	JSON        map[string]any
	Form        map[string]string
	FileBytes   []byte
	FileName    string // uploaded file's name and part Content-Type
	FileType    string
	ContentType string
}

func newBotAPI(t *testing.T) (*botAPI, *Channel) {
	t.Helper()
	b := &botAPI{t: t}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return b, New(NewClient(srv.URL, obs.Secret(testToken), srv.Client()))
}

func (b *botAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/bot"), "/", 2)
	c := call{Method: parts[1], ContentType: r.Header.Get("Content-Type")}
	if strings.HasPrefix(c.ContentType, "multipart/") {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			b.t.Errorf("bad multipart: %v", err)
		}
		c.Form = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			c.Form[k] = v[0]
		}
		for _, field := range []string{"photo", "document"} {
			if f, h, err := r.FormFile(field); err == nil {
				c.FileBytes, _ = io.ReadAll(f)
				c.FileName, c.FileType = h.Filename, h.Header.Get("Content-Type")
			}
		}
	} else {
		_ = json.NewDecoder(r.Body).Decode(&c.JSON)
	}
	b.mu.Lock()
	b.gotToken = parts[0]
	b.calls = append(b.calls, c)
	b.nextID++
	id := b.nextID
	respond := b.respond
	b.mu.Unlock()
	status, body := http.StatusOK, ""
	if respond != nil {
		status, body = respond(c.Method)
	}
	if body == "" {
		body = `{"ok":true,"result":{"message_id":` + strconv.Itoa(10+id) + `}}`
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestSendText(t *testing.T) {
	api, ch := newBotAPI(t)
	id, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{
		Urgency: "low", Title: "T", Blocks: []message.Block{
			{Type: "text", Text: "hello"},
			{Type: "link", Text: "Open", URL: "https://example.com"},
		},
	})
	if err != nil || id != "11" {
		t.Fatalf("Send = %q, %v", id, err)
	}
	if api.gotToken != testToken {
		t.Errorf("token in path = %q", api.gotToken)
	}
	c := api.calls[0]
	if c.Method != "sendMessage" || c.JSON["chat_id"] != "42" || c.JSON["parse_mode"] != "HTML" ||
		c.JSON["disable_notification"] != true || c.JSON["text"] != "<b>T</b>\n\nhello" {
		t.Errorf("call = %+v", c)
	}
	kb := c.JSON["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	if btn := kb[0].([]any)[0].(map[string]any); btn["url"] != "https://example.com" || btn["text"] != "Open" {
		t.Errorf("keyboard = %v", kb)
	}
}

func TestSendPhotoURLAndInline(t *testing.T) {
	api, ch := newBotAPI(t)
	if _, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{Urgency: "normal", Blocks: []message.Block{
		{Type: "image", URL: "https://example.com/a.png", Caption: "cap"},
	}}); err != nil {
		t.Fatal(err)
	}
	if c := api.calls[0]; c.Method != "sendPhoto" || c.JSON["photo"] != "https://example.com/a.png" || c.JSON["caption"] != "<i>cap</i>" {
		t.Errorf("url photo call = %+v", c)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), 1, 2, 3)
	if _, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{
		Urgency: "normal", Title: "Chart",
		Blocks:      []message.Block{{Type: "image", Attachment: idx(0)}, {Type: "link", Text: "Go", URL: "https://e.x"}},
		Attachments: []message.Attachment{{ContentType: "image/png", Bytes: png}},
	}); err != nil {
		t.Fatal(err)
	}
	c := api.calls[1]
	if c.Method != "sendPhoto" || string(c.FileBytes) != string(png) || c.Form["chat_id"] != "42" ||
		c.Form["caption"] != "<b>Chart</b>" || !strings.Contains(c.Form["reply_markup"], "https://e.x") {
		t.Errorf("inline photo call = %+v", c)
	}
}

func TestSendClassifiesErrors(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		permanent  bool
		retryAfter time.Duration
	}{
		{"chat not found", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, true, 0},
		{"blocked", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, true, 0},
		{"bad token", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, true, 0},
		{"rate limited", 429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`, false, 7 * time.Second},
		{"server error", 502, `{"ok":false,"error_code":502,"description":"Bad Gateway"}`, false, 0},
		{"garbage", 500, `<html>oops</html>`, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, ch := newBotAPI(t)
			api.respond = func(string) (int, string) { return tt.status, tt.body }
			_, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{Blocks: []message.Block{{Type: "text", Text: "x"}}})
			var ce *channel.Error
			if !errors.As(err, &ce) || ce.Permanent != tt.permanent || ce.RetryAfter != tt.retryAfter {
				t.Fatalf("err = %#v", err)
			}
			if strings.Contains(ce.Reason, testToken) {
				t.Errorf("token in reason: %s", ce.Reason)
			}
		})
	}
}

func TestNetworkErrorHidesToken(t *testing.T) {
	ch := New(NewClient("http://127.0.0.1:1", obs.Secret(testToken), &http.Client{Timeout: time.Second}))
	_, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{Blocks: []message.Block{{Type: "text", Text: "x"}}})
	var ce *channel.Error
	if !errors.As(err, &ce) || ce.Permanent {
		t.Fatalf("err = %v, want transient", err)
	}
	if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "fake-test-token") {
		t.Errorf("token leaked: %v", err)
	}
}

func TestMissingInlineImageIsPermanent(t *testing.T) {
	_, ch := newBotAPI(t)
	_, err := ch.Send(t.Context(), channel.Contact{Address: "42"}, message.Message{Blocks: []message.Block{{Type: "image", Attachment: idx(3)}}})
	var ce *channel.Error
	if !errors.As(err, &ce) || !ce.Permanent {
		t.Fatalf("err = %v", err)
	}
}

func TestPreviewMatchesRender(t *testing.T) {
	_, ch := newBotAPI(t)
	msg := goldenCases["links"]
	p, err := ch.Preview(msg)
	if err != nil || len(p.Parts) != 1 || ch.Name() != "telegram" {
		t.Fatalf("Preview = %+v, %v", p, err)
	}
}

func TestHealth(t *testing.T) {
	api, ch := newBotAPI(t)
	if err := ch.Health(t.Context()); err != nil || api.calls[0].Method != "getMe" {
		t.Fatalf("Health = %v, calls %+v", err, api.calls)
	}
	api.respond = func(string) (int, string) { return 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}` }
	if err := ch.Health(t.Context()); err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("Health with bad token = %v", err)
	}
}
