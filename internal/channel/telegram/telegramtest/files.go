package telegramtest

import (
	"net/http"
	"strconv"
	"strings"
)

// AddVoiceReply queues a private voice note of data, replying to message
// replyTo when it is not zero. The bot downloads it with getFile.
func (s *Server) AddVoiceReply(chatID, replyTo int64, data []byte) {
	id := s.addFile(data)
	s.addFileReply(chatID, replyTo, "voice",
		map[string]any{"file_id": id, "file_size": len(data), "duration": 1, "mime_type": "audio/ogg"})
}

// AddPhotoReply queues a private photo replying to replyTo. Telegram sends a
// photo in several sizes, smallest first: small is the thumbnail, data the
// largest.
func (s *Server) AddPhotoReply(chatID, replyTo int64, small, data []byte) {
	thumb, big := s.addFile(small), s.addFile(data)
	s.addFileReply(chatID, replyTo, "photo", []map[string]any{
		{"file_id": thumb, "file_size": len(small), "width": 90, "height": 135},
		{"file_id": big, "file_size": len(data), "width": 1024, "height": 1536},
	})
}

// AddDocumentReply queues a private document (a file sent without
// compression) of data and mimeType, replying to replyTo.
func (s *Server) AddDocumentReply(chatID, replyTo int64, data []byte, mimeType string) {
	id := s.addFile(data)
	s.addFileReply(chatID, replyTo, "document",
		map[string]any{"file_id": id, "file_size": len(data), "file_name": "picture", "mime_type": mimeType})
}

func (s *Server) addFile(data []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	id := "file" + strconv.Itoa(len(s.files)+1)
	s.files[id] = data
	return id
}

func (s *Server) addFileReply(chatID, replyTo int64, kind string, file any) {
	from := map[string]any{"id": chatID, "is_bot": false, "first_name": "Test"}
	m := map[string]any{
		"message_id": 9000 + len(s.Calls("")), "chat": privateChat(chatID), "from": from, kind: file,
	}
	if replyTo != 0 {
		m["reply_to_message"] = map[string]any{"message_id": replyTo}
	}
	s.addUpdate("message", m)
}

// getFile answers the Bot API's getFile for a file queued by an Add*Reply.
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
		"file_id": id, "file_size": len(data), "file_path": "files/" + id,
	})
}

// serveFile serves /file/bot<token>/files/<id>, the download URL.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	token, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/file/bot"), "/")
	id := strings.TrimPrefix(path, "files/")
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
