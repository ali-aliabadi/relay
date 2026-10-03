package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

// recipientsLink invites a Telegram @username, or links the chat a bot link
// code came from. `relay serve` reads the bot's messages (Telegram allows one
// reader per bot): it claims invites on /start, or answers with a code. The
// chat ID is never printed.
func recipientsLink(e env, username, codeOrHandle string) error {
	if strings.HasPrefix(codeOrHandle, "@") {
		return recipientsInvite(e, username, codeOrHandle)
	}
	code := codeOrHandle
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

func recipientsInvite(e env, username, handle string) error {
	return e.withConfigStore(func(cfg config.Config, st *store.Store) error {
		rcp, err := core.NewRecipients(st).InviteTelegram(e.ctx, username, handle, time.Now())
		if err != nil {
			return err
		}
		bot := "the Relay bot"
		if cfg.TelegramBotToken != "" {
			ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
			defer cancel()
			if name, err := telegram.NewClient(cfg.TelegramAPIURL, cfg.TelegramBotToken, nil).BotUsername(ctx); err == nil {
				bot = "https://t.me/" + name
			}
		}
		fmt.Fprintf(e.stdout, "Invited %s as %s. Ask %s to open %s and tap Start within %d days.\n",
			rcp.Username, handle, rcp.DisplayName, bot, int(core.InviteTTL.Hours()/24))
		return nil
	})
}
