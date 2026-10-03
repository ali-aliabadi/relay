package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

// recipientsLink links a recipient to the chat a bot link code came from.
// `relay serve` reads the bot's messages and answers /start with the code,
// since Telegram allows only one reader per bot. The chat ID is never printed.
func recipientsLink(e env, username, code string) error {
	return e.withConfigStore(func(cfg config.Config, st *store.Store) error {
		svc := core.NewRecipients(st)
		rcp, err := svc.Get(e.ctx, username)
		if err != nil {
			return err
		}
		ch, addr, err := svc.LinkWithCode(e.ctx, username, code, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "Linked %s to %s.\n", rcp.Username, ch)
		if ch == telegram.Name && cfg.TelegramBotToken != "" {
			chatID, _ := strconv.ParseInt(addr, 10, 64)
			ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
			defer cancel()
			client := telegram.NewClient(cfg.TelegramAPIURL, cfg.TelegramBotToken, nil)
			_ = client.SendText(ctx, chatID, "Linked to Relay. Notifications for "+rcp.DisplayName+" will arrive here.", 0)
		}
		return nil
	})
}
