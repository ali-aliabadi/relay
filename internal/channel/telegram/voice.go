package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/ali-aliabadi/relay/internal/message"
)

// botFile is a file someone sent the bot: a voice note, audio file, photo or document.
type botFile struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	MimeType string `json:"mime_type"`
}

// errTooLarge means a file is over the size Relay accepts for an answer.
var errTooLarge = errors.New("file too large")

// voice returns the message's voice note or audio file, if it has one.
func (m *UpdateMessage) voice() *botFile {
	if m.Voice != nil {
		return m.Voice
	}
	return m.Audio
}

// contentType is the file's audio MIME type without parameters. The sender
// sets it for audio files, so anything else falls back to Ogg Opus, which
// Telegram voice notes are.
func (v *botFile) contentType() string {
	mt, _, err := mime.ParseMediaType(v.MimeType)
	if err != nil || !strings.HasPrefix(mt, "audio/") || len(mt) > message.MaxContentTypeLen {
		return "audio/ogg"
	}
	return mt
}

// download fetches a file's bytes, refusing anything over
// message.MaxAnswerFileBytes before and while downloading.
func (c *Client) download(ctx context.Context, v *botFile) ([]byte, error) {
	if v.FileSize > message.MaxAnswerFileBytes {
		return nil, errTooLarge
	}
	var f struct {
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	}
	if err := c.call(ctx, "getFile", map[string]any{"file_id": v.FileID}, &f); err != nil {
		return nil, err
	}
	if f.FileSize > message.MaxAnswerFileBytes {
		return nil, errTooLarge
	}
	if f.FilePath == "" || strings.Contains(f.FilePath, "..") {
		return nil, errors.New("getFile returned no usable file path")
	}
	endpoint := c.baseURL + "/file/bot" + c.token.Reveal() + "/" + f.FilePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building file request: %w", scrub(err))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading file: %w", scrub(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading file: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, message.MaxAnswerFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("downloading file: %w", scrub(err))
	}
	if len(data) > message.MaxAnswerFileBytes {
		return nil, errTooLarge
	}
	return data, nil
}
