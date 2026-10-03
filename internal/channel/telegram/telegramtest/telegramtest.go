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

// AddStart queues a private "/start" message from a user for getUpdates.
func (s *Server) AddStart(chatID int64, firstName, username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := len(s.updates) + 1
	u, _ := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{
		"text": "/start", "chat": map[string]any{"id": chatID, "type": "private"},
		"from": map[string]any{"first_name": firstName, "username": username},
	}})
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
// endpoints for the e2e suite: GET /_calls, POST /_fail, POST /_start.
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
		s.send(w)
	default:
		reply(w, http.StatusNotFound, Failure{Status: 404, Desc: "Not Found: method not found"}, nil)
	}
}

func (s *Server) send(w http.ResponseWriter) {
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
	case "/_start":
		id, _ := strconv.ParseInt(r.URL.Query().Get("chat_id"), 10, 64)
		s.AddStart(id, r.URL.Query().Get("name"), r.URL.Query().Get("username"))
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
