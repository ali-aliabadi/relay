package telegram

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/ali-aliabadi/relay/internal/channel"
)

// DeleteText removes an outgoing message ID preflighted by core. Telegram may
// refuse messages older than 48 hours. An already absent message is success.
func (c *Channel) DeleteText(ctx context.Context, to channel.Contact, providerID string) error {
	id, err := strconv.ParseInt(providerID, 10, 64)
	if err != nil || id <= 0 {
		return channel.Permanent("message cannot be deleted as text")
	}
	err = c.client.call(ctx, "deleteMessage", map[string]any{"chat_id": to.Address, "message_id": id}, nil)
	if err == nil {
		return nil
	}
	var ae *apiError
	if errors.As(err, &ae) {
		if ae.Code == http.StatusBadRequest && ae.Description == "Bad Request: message to delete not found" {
			return nil
		}
		if ae.Code == http.StatusTooManyRequests {
			return &channel.Error{Reason: "telegram delete rate limited", RetryAfter: ae.RetryAfter}
		}
		return channel.Permanent("telegram rejected message deletion")
	}
	return channel.Transient("telegram delete unavailable")
}
