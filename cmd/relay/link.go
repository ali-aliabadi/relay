package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

// linkTimeout is how long `recipients link` waits for /start.
const linkTimeout = 5 * time.Minute

// recipientsLink waits for someone to send /start to the bot, asks the
// operator to confirm who it is, and stores that chat as the recipient's
// Telegram contact. The chat ID is never printed.
func recipientsLink(e env, username string) error {
	return e.withConfigStore(func(cfg config.Config, st *store.Store) error {
		if cfg.TelegramBotToken == "" {
			return errors.New("RELAY_TELEGRAM_BOT_TOKEN is not set")
		}
		svc := core.NewRecipients(st)
		rcp, err := svc.Get(e.ctx, username)
		if err != nil {
			return err
		}
		client := telegram.NewClient(cfg.TelegramAPIURL, cfg.TelegramBotToken, nil)
		ctx, cancel := context.WithTimeout(e.ctx, linkTimeout)
		defer cancel()

		bot, err := client.BotUsername(ctx)
		if err != nil {
			return fmt.Errorf("checking the bot token: %w", err)
		}
		offset, err := latestOffset(ctx, client)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "From %s's Telegram, open @%s and send /start. Waiting up to %s...\n", rcp.DisplayName, bot, linkTimeout)

		return confirmLoop(ctx, e, client, svc, rcp, offset)
	})
}

// confirmLoop offers each /start to the operator until one is accepted.
func confirmLoop(ctx context.Context, e env, client *telegram.Client, svc *core.Recipients, rcp store.Recipient, offset int64) error {
	in := bufio.NewReader(e.stdin)
	for {
		msg, next, err := waitForStart(ctx, client, offset)
		if err != nil {
			return err
		}
		offset = next
		who := strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName)
		if msg.From.Username != "" {
			who += " (@" + msg.From.Username + ")"
		}
		fmt.Fprintf(e.stdout, "Got /start from %s. Link this chat to %s? [y/N] ", who, rcp.Username)
		answer, _ := in.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Fprintln(e.stdout, "Skipped. Waiting for another /start...")
			continue
		}
		if err := svc.Link(ctx, rcp.Username, telegram.Name, strconv.FormatInt(msg.Chat.ID, 10), time.Now()); err != nil {
			return err
		}
		_ = client.SendText(ctx, msg.Chat.ID, "Linked to Relay. Notifications for "+rcp.DisplayName+" will arrive here.")
		fmt.Fprintf(e.stdout, "Linked %s to Telegram.\n", rcp.Username)
		return nil
	}
}

// latestOffset skips updates that arrived before the command started.
func latestOffset(ctx context.Context, c *telegram.Client) (int64, error) {
	ups, err := c.GetUpdates(ctx, -1, 0)
	if err != nil {
		return 0, fmt.Errorf("reading bot updates: %w", err)
	}
	if len(ups) == 0 {
		return 0, nil
	}
	return ups[len(ups)-1].UpdateID + 1, nil
}

// waitForStart long-polls until a private chat sends /start.
func waitForStart(ctx context.Context, c *telegram.Client, offset int64) (*telegram.UpdateMessage, int64, error) {
	for {
		ups, err := c.GetUpdates(ctx, offset, 25)
		if ctx.Err() != nil {
			return nil, offset, errors.New("timed out waiting for /start")
		}
		if err != nil {
			return nil, offset, fmt.Errorf("reading bot updates: %w", err)
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			if m := u.Message; m != nil && m.Chat.Type == "private" && strings.HasPrefix(m.Text, "/start") {
				return m, offset, nil
			}
		}
	}
}
