package telegram

import "net/http"

// image returns the picture a message carries: the largest size of a photo
// (Telegram lists them smallest first), or a document, which keeps full
// quality. Whether a document is a picture is decided by its bytes.
func (m *UpdateMessage) image() *botFile {
	if n := len(m.Photo); n > 0 {
		return &m.Photo[n-1]
	}
	return m.Document
}

// imageType is data's type when it is a PNG, JPEG or WebP picture, and ""
// otherwise. It reads the bytes, never the sender's mime_type.
func imageType(data []byte) string {
	switch t := http.DetectContentType(data); t {
	case "image/png", "image/jpeg", "image/webp":
		return t
	}
	return ""
}
