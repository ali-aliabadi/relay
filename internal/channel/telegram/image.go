package telegram

import (
	"encoding/binary"
	"net/http"
)

// image returns the largest photo, an inline animation, a file or a video.
// The downloaded bytes decide whether Relay supports the format.
func (m *UpdateMessage) image() *botFile {
	if n := len(m.Photo); n > 0 {
		return &m.Photo[n-1]
	}
	if m.Animation != nil {
		return m.Animation
	}
	if m.Document != nil {
		return m.Document
	}
	return m.Video
}

const minFileTypeBox = 16 // size, type, major brand and minor version

// imageType returns the supported scene format from the file's bytes.
// Telegram's mime_type can differ from what was downloaded.
func imageType(data []byte) string {
	switch t := http.DetectContentType(data); t {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return t
	}
	// Telegram also sends GIF animations as MP4. A bounded ftyp box
	// identifies the container; the receiving app validates its streams.
	if len(data) >= minFileTypeBox && string(data[4:8]) == "ftyp" {
		size := int64(binary.BigEndian.Uint32(data[:4]))
		if size >= minFileTypeBox && size <= int64(len(data)) {
			return "video/mp4"
		}
	}
	return ""
}
