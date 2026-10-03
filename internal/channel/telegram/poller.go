package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
)

// Poller reads the bot's incoming updates: answers to questions and /start
// from people asking to be linked. Telegram allows one reader per bot, so
// only `relay serve` runs it.
type Poller struct {
	Client   *Client
	OnAnswer channel.AnswerFunc
	// LinkCode returns the code an admin passes to `relay recipients link`.
	LinkCode func(chatID string) (string, error)
	Logger   *slog.Logger
	Backoff  time.Duration // after a failed poll; 5s when zero
}

// Feedback shown to the person who replied, by outcome.
var feedback = map[string]string{
	channel.AnswerSaved:      "✅ Answer saved.",
	channel.AnswerFinal:      "Your answer was already picked up, so it can't be changed now.",
	channel.AnswerExpired:    "This question has expired.",
	channel.AnswerUnknown:    "That message isn't a question waiting for your answer.",
	channel.AnswerNeedOption: "Please answer with one of the buttons.",
	channel.AnswerNeedText:   "Please answer with text.",
}

const replyHint = "To answer a question, reply to its message. To link this chat to Relay, send /start."

// Run polls until ctx is cancelled. Unconfirmed updates stay on Telegram's
// side for 24h, so a restart picks up where it stopped.
func (p *Poller) Run(ctx context.Context) {
	backoff := p.Backoff
	if backoff <= 0 {
		backoff = 5 * time.Second
	}
	var offset int64
	failing := false
	for {
		ups, err := p.Client.GetUpdates(ctx, offset, 25)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if !failing { // log once per outage, not every retry
				p.Logger.Warn("telegram polling failed", slog.String("error", err.Error()))
			}
			failing = true
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		failing = false
		for _, u := range ups {
			offset = u.UpdateID + 1
			p.handle(ctx, u)
		}
	}
}

func (p *Poller) handle(ctx context.Context, u Update) {
	var err error
	switch {
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		err = p.handleTap(ctx, u.CallbackQuery)
	case u.Message != nil && u.Message.Chat.Type == "private":
		err = p.handleMessage(ctx, u.Message)
	}
	if err != nil {
		p.Logger.Warn("handling telegram update", slog.Int64("update_id", u.UpdateID), slog.String("error", err.Error()))
	}
}

func (p *Poller) handleTap(ctx context.Context, q *CallbackQuery) error {
	deliveryID, option, ok := parseAnswerData(q.Data)
	if !ok {
		return p.Client.answerCallback(ctx, q.ID, feedback[channel.AnswerUnknown])
	}
	chat := q.Message.Chat.ID
	res, err := p.OnAnswer(ctx, channel.Answer{
		Channel: Name, Address: strconv.FormatInt(chat, 10), DeliveryID: deliveryID, Option: option,
	})
	if err != nil {
		_ = p.Client.answerCallback(ctx, q.ID, "Something went wrong, please try again.")
		return err
	}
	if err := p.Client.answerCallback(ctx, q.ID, feedback[res.Outcome]); err != nil {
		return err
	}
	if res.Outcome != channel.AnswerSaved || q.Message.ReplyMarkup == nil {
		return nil
	}
	// Tick the chosen option, keeping the other buttons (links) as they are.
	m := *q.Message.ReplyMarkup
	for _, row := range m.InlineKeyboard {
		for i, b := range row {
			if id, n, ok := parseAnswerData(b.CallbackData); ok && id == deliveryID && n < len(res.Options) {
				row[i].Text = res.Options[n]
				if n == option {
					row[i].Text = "✅ " + res.Options[n]
				}
			}
		}
	}
	return p.Client.editMarkup(ctx, chat, q.Message.MessageID, m)
}

func (p *Poller) handleMessage(ctx context.Context, m *UpdateMessage) error {
	chat := m.Chat.ID
	if strings.HasPrefix(m.Text, "/start") {
		code, err := p.LinkCode(strconv.FormatInt(chat, 10))
		if err != nil {
			return err
		}
		return p.Client.SendText(ctx, chat, "To get Relay notifications here, ask the admin to run:\n\n"+
			"relay recipients link <your username> "+code+"\n\nThe code works for 1 hour.", 0)
	}
	if m.ReplyToMessage == nil {
		return p.Client.SendText(ctx, chat, replyHint, m.MessageID)
	}
	res, err := p.OnAnswer(ctx, channel.Answer{
		Channel: Name, Address: strconv.FormatInt(chat, 10), Option: -1, Text: m.Text,
		ProviderMessageID: strconv.FormatInt(m.ReplyToMessage.MessageID, 10),
	})
	if err != nil {
		_ = p.Client.SendText(ctx, chat, "Something went wrong, please try again.", m.MessageID)
		return err
	}
	return p.Client.SendText(ctx, chat, feedback[res.Outcome], m.MessageID)
}
