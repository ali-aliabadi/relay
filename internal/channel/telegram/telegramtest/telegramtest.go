// Package telegramtest is a fake Telegram Bot API for tests and the e2e fake
// Telegram container. It records calls, serves queued updates and can be
// told to fail the next requests.
package telegramtest

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Call is one recorded Bot API request.
type Call struct {
	Token  string            `json:"token"`
	Method string            `json:"method"`
	Params map[string]any    `json:"params,omitempty"` // JSON requests
	Form   map[string]string `json:"form,omitempty"`   // multipart requests
	File   int               `json:"file_bytes,omitempty"`
	// MessageID is what a successful sendMessage/sendPhoto returned.
	MessageID int64 `json:"message_id,omitempty"`
}

// Failure is a canned error response.
type Failure struct {
	Status     int    `json:"status"`
	Desc       string `json:"description"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

// Server is the fake. Its zero value is not usable; call New.
type Server struct {
	Token string // the only token accepted; others get 401

	mu      sync.Mutex
	calls   []Call
	fails   []Failure
	updates []json.RawMessage
	nextID  int64
}

// New returns a fake accepting token.
func New(token string) *Server { return &Server{Token: token} }

// FailNext makes the next sendMessage/sendPhoto calls fail, in order.
func (s *Server) FailNext(f ...Failure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails = append(s.fails, f...)
}

// AddStart queues a private "/start" from a chat whose user has no username.
func (s *Server) AddStart(chatID int64) { s.AddStartFrom(chatID, "") }

// AddStartFrom queues a private "/start" from a user with this Telegram username.
func (s *Server) AddStartFrom(chatID int64, username string) {
	s.addMessage(chatID, 0, "/start", username)
}

// AddText queues a private text message (a command, say) from a user with
// this Telegram username ("" for none).
func (s *Server) AddText(chatID int64, text, username string) {
	s.addMessage(chatID, 0, text, username)
}

// AddReply queues a private text message, replying to message replyTo when
// it is not zero.
func (s *Server) AddReply(chatID, replyTo int64, text string) {
	s.addMessage(chatID, replyTo, text, "")
}

func (s *Server) addMessage(chatID, replyTo int64, text, username string) {
	from := map[string]any{"id": chatID, "is_bot": false, "first_name": "Test"}
	if username != "" {
		from["username"] = username
	}
	m := map[string]any{"message_id": 9000 + len(s.Calls("")), "text": text, "chat": privateChat(chatID), "from": from}
	if replyTo != 0 {
		m["reply_to_message"] = map[string]any{"message_id": replyTo}
	}
	s.addUpdate("message", m)
}

// AddTap queues a tap on a button with callback data on message messageID.
func (s *Server) AddTap(chatID, messageID int64, data string, markup any) {
	s.addUpdate("callback_query", map[string]any{"id": "cb" + strconv.Itoa(len(s.Calls(""))), "data": data, "message": map[string]any{
		"message_id": messageID, "chat": privateChat(chatID), "reply_markup": markup,
	}})
}

// AddGroupMessage queues a message in a group chat, which Relay ignores.
func (s *Server) AddGroupMessage(chatID int64, text string) {
	s.addUpdate("message", map[string]any{"message_id": 1, "text": text, "chat": map[string]any{"id": chatID, "type": "group"}})
}

func privateChat(id int64) map[string]any { return map[string]any{"id": id, "type": "private"} }

func (s *Server) addUpdate(kind string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, _ := json.Marshal(map[string]any{"update_id": len(s.updates) + 1, kind: v})
	s.updates = append(s.updates, u)
}

// Calls returns every recorded call, optionally only for method.
func (s *Server) Calls(method string) []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Call
	for _, c := range s.calls {
		if method == "" || c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// ServeHTTP implements the Bot API paths (/bot<token>/<method>) plus control
// endpoints for the e2e suite: GET /_calls, POST /_fail, POST /_reply, POST /_tap.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/_") {
		s.control(w, r)
		return
	}
	token, method, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
	if !ok {
		reply(w, http.StatusNotFound, Failure{Status: 404, Desc: "Not Found"}, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	call := decodeCall(r, token, method)
	s.mu.Lock()
	s.calls = append(s.calls, call)
	idx := len(s.calls) - 1
	s.mu.Unlock()
	if token != s.Token {
		reply(w, http.StatusUnauthorized, Failure{Status: 401, Desc: "Unauthorized"}, nil)
		return
	}
	switch method {
	case "getMe":
		reply(w, http.StatusOK, Failure{}, map[string]any{"id": 1, "is_bot": true, "username": "relay_test_bot"})
	case "getUpdates":
		ups := s.pendingUpdates(call.Params)
		if timeout, _ := call.Params["timeout"].(float64); len(ups) == 0 && timeout > 0 {
			time.Sleep(20 * time.Millisecond) // a short stand-in for long polling
		}
		reply(w, http.StatusOK, Failure{}, ups)
	case "sendMessage", "sendPhoto":
		s.send(w, idx)
	case "answerCallbackQuery", "editMessageReplyMarkup":
		reply(w, http.StatusOK, Failure{}, true)
	default:
		reply(w, http.StatusNotFound, Failure{Status: 404, Desc: "Not Found: method not found"}, nil)
	}
}

func (s *Server) send(w http.ResponseWriter, idx int) {
	s.mu.Lock()
	if len(s.fails) > 0 {
		f := s.fails[0]
		s.fails = s.fails[1:]
		s.mu.Unlock()
		reply(w, f.Status, f, nil)
		return
	}
	s.nextID++
	id := s.nextID
	s.calls[idx].MessageID = id
	s.mu.Unlock()
	reply(w, http.StatusOK, Failure{}, map[string]any{"message_id": id})
}

func (s *Server) pendingUpdates(params map[string]any) []json.RawMessage {
	offset, _ := params["offset"].(float64)
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < 0 {
		if len(s.updates) == 0 {
			return []json.RawMessage{}
		}
		return s.updates[len(s.updates)-1:]
	}
	start := max(int(offset)-1, 0)
	if start >= len(s.updates) {
		return []json.RawMessage{}
	}
	return s.updates[start:]
}

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_calls":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Calls(r.URL.Query().Get("method")))
	case "/_fail":
		var f []Failure
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		s.FailNext(f...)
	case "/_reply":
		q := r.URL.Query()
		chat, _ := strconv.ParseInt(q.Get("chat_id"), 10, 64)
		to, _ := strconv.ParseInt(q.Get("reply_to"), 10, 64)
		s.addMessage(chat, to, q.Get("text"), q.Get("username"))
	case "/_tap":
		q := r.URL.Query()
		chat, _ := strconv.ParseInt(q.Get("chat_id"), 10, 64)
		msg, _ := strconv.ParseInt(q.Get("message_id"), 10, 64)
		s.AddTap(chat, msg, q.Get("data"), nil)
	case "/_health":
	default:
		http.NotFound(w, r)
	}
}

func decodeCall(r *http.Request, token, method string) Call {
	c := Call{Token: token, Method: method}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(nil, r.Body, 16<<20)
		if err := r.ParseMultipartForm(16 << 20); err == nil { //nolint:gosec // test fake; body capped just above

			c.Form = map[string]string{}
			for k, v := range r.MultipartForm.Value {
				c.Form[k] = v[0]
			}
			if f, _, err := r.FormFile("photo"); err == nil {
				b, _ := io.ReadAll(f)
				c.File = len(b)
			}
		}
		return c
	}
	_ = json.NewDecoder(r.Body).Decode(&c.Params)
	return c
}

func reply(w http.ResponseWriter, status int, f Failure, result any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"ok": status == http.StatusOK}
	if status == http.StatusOK {
		body["result"] = result
	} else {
		body["error_code"] = f.Status
		body["description"] = f.Desc
		if f.RetryAfter > 0 {
			body["parameters"] = map[string]any{"retry_after": f.RetryAfter}
		}
	}
	_ = json.NewEncoder(w).Encode(body)
}
