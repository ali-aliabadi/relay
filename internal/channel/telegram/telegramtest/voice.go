package telegramtest

import (
	"net/http"
	"strconv"
	"strings"
)

// AddVoiceReply queues a private voice note of data, replying to message
// replyTo when it is not zero. The bot downloads it with getFile.
func (s *Server) AddVoiceReply(chatID, replyTo int64, data []byte) {
	s.mu.Lock()
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	id := "voice" + strconv.Itoa(len(s.files)+1)
	s.files[id] = data
	s.mu.Unlock()
	from := map[string]any{"id": chatID, "is_bot": false, "first_name": "Test"}
	m := map[string]any{
		"message_id": 9000 + len(s.Calls("")), "chat": privateChat(chatID), "from": from,
		"voice": map[string]any{"file_id": id, "file_size": len(data), "duration": 1, "mime_type": "audio/ogg"},
	}
	if replyTo != 0 {
		m["reply_to_message"] = map[string]any{"message_id": replyTo}
	}
	s.addUpdate("message", m)
}

// getFile answers the Bot API's getFile for a file queued by AddVoiceReply.
func (s *Server) getFile(w http.ResponseWriter, params map[string]any) {
	id, _ := params["file_id"].(string)
	s.mu.Lock()
	data, ok := s.files[id]
	s.mu.Unlock()
	if !ok {
		reply(w, http.StatusBadRequest, Failure{Status: 400, Desc: "Bad Request: invalid file_id"}, nil)
		return
	}
	reply(w, http.StatusOK, Failure{}, map[string]any{
		"file_id": id, "file_size": len(data), "file_path": "voice/" + id + ".oga",
	})
}

// serveFile serves /file/bot<token>/voice/<id>.oga, the download URL.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	token, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/file/bot"), "/")
	id := strings.TrimSuffix(strings.TrimPrefix(path, "voice/"), ".oga")
	s.mu.Lock()
	s.calls = append(s.calls, Call{Token: token, Method: "file"})
	data, ok := s.files[id]
	s.mu.Unlock()
	if token != s.Token || !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}
