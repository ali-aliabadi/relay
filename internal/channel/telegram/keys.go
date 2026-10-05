package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"time"
)

// KeyReply is what the admin's /newkey, /keys or /revoke produced.
type KeyReply struct {
	Admin bool   // false: not the admin's chat, so the command is treated like any text
	Text  string // plain reply to the admin
	Key   string // a new API key, sent on its own and deleted after KeyTTL; never logged
}

// defaultKeyTTL is how long a new API key stays in the admin's chat.
const defaultKeyTTL = time.Minute

// handleKeys answers the admin's key commands. handled is false for anyone
// else, who gets the usual reply instead.
func (p *Poller) handleKeys(ctx context.Context, chat int64, cmd, args string) (handled bool, err error) {
	if p.Keys == nil {
		return false, nil
	}
	res, err := p.Keys(ctx, strconv.FormatInt(chat, 10), cmd, args)
	if err != nil {
		_ = p.Client.SendText(ctx, chat, "Something went wrong, please try again.", 0)
		return true, err
	}
	if !res.Admin {
		return false, nil
	}
	if err := p.Client.SendText(ctx, chat, res.Text, 0); err != nil || res.Key == "" {
		return true, err
	}
	ttl := p.keyTTL()
	text := fmt.Sprintf("<code>%s</code>\n\nTap the key to copy it. This message deletes itself in %s.",
		html.EscapeString(res.Key), ttlText(ttl))
	id, err := p.Client.sendHTML(ctx, chat, text)
	if err != nil {
		return true, err
	}
	p.deleteLater(ctx, chat, id, ttl)
	return true, nil
}

func (p *Poller) keyTTL() time.Duration {
	if p.KeyTTL > 0 {
		return p.KeyTTL
	}
	return defaultKeyTTL
}

func ttlText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		if d == time.Minute {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}

// deleteLater deletes a key message after ttl, or at once when ctx ends (Relay
// is stopping), so a restart doesn't leave the key in the chat. Run waits for
// these before returning.
func (p *Poller) deleteLater(ctx context.Context, chat, messageID int64, ttl time.Duration) {
	p.pending.Go(func() {
		t := time.NewTimer(ttl)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
		}
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := p.Client.deleteMessage(dctx, chat, messageID); err != nil {
			p.Logger.Warn("deleting api key message", slog.String("error", err.Error()))
			_ = p.Client.SendText(dctx, chat, "I couldn't delete the message with the new API key. Please delete it yourself.", 0)
		}
	})
}

// sendHTML sends an HTML-formatted message with no link preview and returns
// its message ID. Every caller value in text must already be escaped.
func (c *Client) sendHTML(ctx context.Context, chatID int64, text string) (int64, error) {
	var out struct {
		MessageID int64 `json:"message_id"`
	}
	err := c.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID, "text": text, "parse_mode": "HTML",
		"link_preview_options": map[string]any{"is_disabled": true},
	}, &out)
	return out.MessageID, err
}

// deleteMessage deletes a message the bot sent (allowed for 48 hours).
func (c *Client) deleteMessage(ctx context.Context, chatID, messageID int64) error {
	return c.call(ctx, "deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID}, nil)
}
