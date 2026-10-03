package telegram

import "context"

// Update is the subset of a Bot API update used to link chats.
type Update struct {
	UpdateID int64          `json:"update_id"`
	Message  *UpdateMessage `json:"message"`
}

// UpdateMessage is an incoming message. Chat.ID is private.
type UpdateMessage struct {
	Text string `json:"text"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From struct {
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"from"`
}

// GetUpdates long-polls for updates after offset, waiting up to timeoutSec.
// An offset of -1 returns only the latest update.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	var out []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeoutSec, "allowed_updates": []string{"message"},
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

// SendText sends a plain message (no markup) to a chat.
func (c *Client) SendText(ctx context.Context, chatID int64, text string) error {
	return c.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil)
}
