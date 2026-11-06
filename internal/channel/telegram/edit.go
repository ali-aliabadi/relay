package telegram

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

// EditText only edits the single text part; photos, questions and files are refused.
func (c *Channel) EditText(ctx context.Context, to channel.Contact, providerID string, msg message.Message) error {
	parts := Render(msg)
	id, err := strconv.ParseInt(providerID, 10, 64)
	if err != nil || id <= 0 || len(parts) != 1 || parts[0].Kind != "text" || len(parts[0].Buttons) != 0 {
		return channel.Permanent("message cannot be edited as text")
	}
	params := map[string]any{
		"chat_id": to.Address, "message_id": id, "text": parts[0].Text, "parse_mode": "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	}
	err = c.client.call(ctx, "editMessageText", params, nil)
	if err == nil {
		return nil
	}
	var ae *apiError
	if errors.As(err, &ae) && ae.Code == http.StatusBadRequest && strings.HasPrefix(ae.Description, "Bad Request: message is not modified") {
		return nil // Repeating the same edit is idempotent, including after a lost reply.
	}
	// Provider descriptions can echo message content. Do not return them to logs.
	if errors.As(err, &ae) {
		if ae.Code == http.StatusTooManyRequests {
			return &channel.Error{Reason: "telegram edit rate limited", RetryAfter: ae.RetryAfter}
		}
		return channel.Permanent("telegram rejected message edit")
	}
	return channel.Transient("telegram edit unavailable")
}
