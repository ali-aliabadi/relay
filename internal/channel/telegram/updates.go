package telegram

import (
	"context"
	"strconv"
	"strings"
)

// Update is the subset of a Bot API update Relay reads.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *UpdateMessage `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// UpdateMessage is an incoming message. Chat.ID and Text are private.
type UpdateMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	// From is set by Telegram, so a sender can't claim someone else's username.
	From struct {
		Username string `json:"username"`
	} `json:"from"`
	ReplyToMessage *struct {
		MessageID int64 `json:"message_id"`
	} `json:"reply_to_message"`
	ReplyMarkup *replyMarkup `json:"reply_markup"`
}

// CallbackQuery is a tap on an inline button with callback data.
type CallbackQuery struct {
	ID      string         `json:"id"`
	Data    string         `json:"data"`
	Message *UpdateMessage `json:"message"`
}

// GetUpdates long-polls for messages and button taps after offset, waiting
// up to timeoutSec.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	var out []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSec, "allowed_updates": []string{"message", "callback_query"},
	}, &out)
	return out, err
}

// BotUsername returns the bot's @username via getMe.
func (c *Client) BotUsername(ctx context.Context) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	err := c.call(ctx, "getMe", struct{}{}, &me)
	return me.Username, err
}

// SendText sends a plain message (no markup) to a chat, as a reply when
// replyTo is not zero.
func (c *Client) SendText(ctx context.Context, chatID int64, text string, replyTo int64) error {
	params := map[string]any{"chat_id": chatID, "text": text}
	if replyTo != 0 {
		params["reply_parameters"] = map[string]any{"message_id": replyTo, "allow_sending_without_reply": true}
	}
	return c.call(ctx, "sendMessage", params, nil)
}

// answerCallback shows a short toast for a button tap.
func (c *Client) answerCallback(ctx context.Context, id, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

// editMarkup replaces a sent message's buttons.
func (c *Client) editMarkup(ctx context.Context, chatID, messageID int64, m replyMarkup) error {
	return c.call(ctx, "editMessageReplyMarkup", map[string]any{
		"chat_id": chatID, "message_id": messageID, "reply_markup": m,
	}, nil)
}

// AnswerData is the callback data of option i's button on a delivery. It
// stays well under Telegram's 64-byte limit (delivery IDs are 30 bytes).
func AnswerData(deliveryID string, option int) string {
	return "a:" + deliveryID + ":" + strconv.Itoa(option)
}

func parseAnswerData(data string) (deliveryID string, option int, ok bool) {
	rest, ok := strings.CutPrefix(data, "a:")
	if !ok {
		return "", 0, false
	}
	deliveryID, idx, ok := strings.Cut(rest, ":")
	option, err := strconv.Atoi(idx)
	if !ok || err != nil || option < 0 || deliveryID == "" {
		return "", 0, false
	}
	return deliveryID, option, true
}
