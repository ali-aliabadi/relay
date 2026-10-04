package telegram

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

// Name is the channel's identifier.
const Name = "telegram"

// Channel sends messages through a Telegram bot.
type Channel struct {
	client *Client
}

// New returns the Telegram channel using client.
func New(client *Client) *Channel { return &Channel{client: client} }

// Name implements channel.Channel.
func (c *Channel) Name() string { return Name }

// Preview implements channel.Channel.
func (c *Channel) Preview(msg message.Message) (channel.Preview, error) {
	return channel.Preview{Parts: Render(msg)}, nil
}

type sentMessage struct {
	MessageID int64 `json:"message_id"`
}

type inlineButton struct {
	Text         string `json:"text"`
	URL          string `json:"url,omitempty"`
	CallbackData string `json:"callback_data,omitempty"`
}

type replyMarkup struct {
	InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
}

// Send implements channel.Channel. It returns the last Telegram message ID:
// the part carrying the buttons and question, which replies point at.
func (c *Channel) Send(ctx context.Context, to channel.Contact, msg message.Message) (string, error) {
	var last string
	for _, part := range Render(msg) {
		id, err := c.sendPart(ctx, to.Address, part, msg)
		if err != nil {
			return "", classify(err)
		}
		last = strconv.FormatInt(id, 10)
	}
	return last, nil
}

func (c *Channel) sendPart(ctx context.Context, chatID string, p channel.Part, msg message.Message) (int64, error) {
	var out sentMessage
	params := map[string]any{"chat_id": chatID, "parse_mode": "HTML", "disable_notification": p.DisableNotification}
	if len(p.Buttons) > 0 {
		params["reply_markup"] = markup(p.Buttons)
	}
	if p.Kind == "text" {
		params["text"] = p.Text
		params["link_preview_options"] = map[string]bool{"is_disabled": true}
		err := c.client.call(ctx, "sendMessage", params, &out)
		return out.MessageID, err
	}
	if p.Text != "" {
		params["caption"] = p.Text
	}
	method, field := "sendPhoto", "photo"
	if p.Kind == "document" {
		method, field = "sendDocument", "document"
	} else if p.Photo != "inline" {
		params["photo"] = p.Photo
		err := c.client.call(ctx, method, params, &out)
		return out.MessageID, err
	}
	att, filename, ok := inlineAttachment(msg, p.Kind)
	if !ok {
		return 0, channel.Permanent("inline %s missing", p.Kind)
	}
	fields := map[string]string{}
	for k, v := range params {
		switch v := v.(type) {
		case string:
			fields[k] = v
		case bool:
			fields[k] = strconv.FormatBool(v)
		default:
			raw, _ := jsonString(v)
			fields[k] = raw
		}
	}
	file := upload{field: field, name: filename, contentType: att.ContentType, data: att.Bytes}
	err := c.client.callMultipart(ctx, method, fields, file, &out)
	return out.MessageID, err
}

func markup(buttons []channel.Button) replyMarkup {
	rows := make([][]inlineButton, len(buttons))
	for i, b := range buttons {
		rows[i] = []inlineButton{{Text: b.Text, URL: b.URL, CallbackData: b.Data}}
	}
	return replyMarkup{InlineKeyboard: rows}
}

// inlineAttachment returns the bytes and upload file name of the message's
// inline photo or document (each message has at most one of each).
func inlineAttachment(msg message.Message, kind string) (message.Attachment, string, bool) {
	for _, b := range msg.Blocks {
		if b.Attachment == nil || *b.Attachment >= len(msg.Attachments) {
			continue
		}
		att := msg.Attachments[*b.Attachment]
		switch {
		case kind == "document" && b.Type == message.BlockFile:
			return att, b.Filename, true
		case kind == "photo" && b.Type == message.BlockImage:
			name := "image.png"
			if att.ContentType == "image/jpeg" {
				name = "image.jpg"
			}
			return att, name, true
		}
	}
	return message.Attachment{}, "", false
}

// classify maps Bot API failures to channel errors. 400, 401, 403 and 404
// (bad chat, bot blocked, bad token, bad request) won't succeed on retry;
// 429, 5xx and network errors may.
func classify(err error) error {
	var ce *channel.Error
	if errors.As(err, &ce) {
		return ce
	}
	var ae *apiError
	if !errors.As(err, &ae) {
		return channel.Transient("telegram: %v", err)
	}
	code := ae.Code
	if code == 0 {
		code = ae.Status
	}
	switch code {
	case http.StatusTooManyRequests:
		e := channel.Transient("telegram: rate limited")
		e.RetryAfter = ae.RetryAfter
		return e
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return channel.Permanent("telegram: %d %s", code, ae.Description)
	default:
		return channel.Transient("telegram: %d %s", code, ae.Description)
	}
}

// Health implements channel.HealthChecker with getMe, which fails on a bad
// token or when Telegram is unreachable.
func (c *Channel) Health(ctx context.Context) error {
	if err := c.client.call(ctx, "getMe", struct{}{}, nil); err != nil {
		return classify(err)
	}
	return nil
}
