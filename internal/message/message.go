// Package message defines the content blocks callers send, their validation
// and limits. It is shared by the API and every channel layout.
package message

import "log/slog"

// Urgency levels.
const (
	UrgencyLow      = "low"
	UrgencyNormal   = "normal"
	UrgencyHigh     = "high"
	UrgencyCritical = "critical"
)

// Block types.
const (
	BlockText   = "text"
	BlockFields = "fields"
	BlockTable  = "table"
	BlockImage  = "image"
	BlockCode   = "code"
	BlockLink   = "link"
)

// Request is the body of POST /v1/messages and POST /v1/preview.
type Request struct {
	To             []string `json:"to"`
	Urgency        string   `json:"urgency,omitempty"`
	Source         string   `json:"source,omitempty"`
	Title          string   `json:"title,omitempty"`
	Text           string   `json:"text,omitempty"` // shorthand for one text block
	Blocks         []Block  `json:"blocks,omitempty"`
	Channels       []string `json:"channels,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

// LogValue keeps content out of logs.
func (r Request) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("recipients", len(r.To)),
		slog.String("urgency", r.Urgency),
		slog.Int("blocks", len(r.Blocks)),
	)
}

// Block is one piece of content. Which fields apply depends on Type.
type Block struct {
	Type        string     `json:"type"`
	Text        string     `json:"text,omitempty"`         // text, code, link
	Items       []Field    `json:"items,omitempty"`        // fields
	Columns     []string   `json:"columns,omitempty"`      // table
	Rows        [][]string `json:"rows,omitempty"`         // table
	URL         string     `json:"url,omitempty"`          // image, link
	Base64      string     `json:"base64,omitempty"`       // image (inline)
	ContentType string     `json:"content_type,omitempty"` // image (inline)
	Caption     string     `json:"caption,omitempty"`      // image

	// Attachment is set by Relay, never by callers: the index into the
	// message's stored attachments that holds this inline image's bytes.
	Attachment *int `json:"attachment,omitempty"`
}

// Field is one label/value line of a fields block.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Message is what a channel layout renders: a validated request's content,
// with inline images resolved to bytes.
type Message struct {
	Urgency string
	Title   string
	Source  string
	Blocks  []Block
	Images  []Image // inline image bytes, indexed by Block.Attachment
}

// Image is an inline image's bytes.
type Image struct {
	ContentType string
	Bytes       []byte
}

// LogValue keeps content out of logs.
func (m Message) LogValue() slog.Value {
	return slog.GroupValue(slog.String("urgency", m.Urgency), slog.Int("blocks", len(m.Blocks)))
}
