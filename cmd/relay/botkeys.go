package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

const keysUsage = "Use: /newkey <app>, /keys, or /revoke <app>. App names use a-z, 0-9, '-' and '_'."

// botKeys answers /newkey <app>, /keys, /revoke <app> and /recipients, but
// only in the admin's own linked chat. Returns nil (the commands are off) when no admin
// is configured.
func botKeys(admin string, recipients *core.Recipients, clients *core.Clients, logger *slog.Logger,
) func(ctx context.Context, chatID, cmd, args string) (telegram.KeyReply, error) {
	if admin == "" {
		return nil
	}
	return func(ctx context.Context, chatID, cmd, args string) (telegram.KeyReply, error) {
		_, adminChat, ok, err := recipients.TelegramAdmin(ctx, admin)
		if err != nil || !ok || adminChat != chatID {
			return telegram.KeyReply{}, err
		}
		app := strings.ToLower(args)
		switch {
		case cmd == "/keys" && args == "":
			return keysList(ctx, clients)
		case cmd == "/recipients":
			return recipientsReply(ctx, recipients)
		case cmd == "/newkey" && app != "":
			return newKey(ctx, clients, logger, app)
		case cmd == "/revoke" && app != "":
			return revokeKey(ctx, clients, logger, app)
		}
		return telegram.KeyReply{Admin: true, Text: keysUsage}, nil
	}
}

func newKey(ctx context.Context, clients *core.Clients, logger *slog.Logger, app string) (telegram.KeyReply, error) {
	c, key, err := clients.Create(ctx, app)
	switch {
	case errors.Is(err, core.ErrInvalid):
		return telegram.KeyReply{Admin: true, Text: "Couldn't make a key: " + problem(err) + "\n\n" + keysUsage}, nil
	case errors.Is(err, store.ErrConflict):
		return telegram.KeyReply{Admin: true, Text: fmt.Sprintf(
			"%s already has a key. To replace it, send /revoke %s and then /newkey %s.", app, app, app)}, nil
	case err != nil:
		return telegram.KeyReply{}, err
	}
	logger.InfoContext(ctx, "api key created", slog.String("client_id", c.ID), slog.String("channel", telegram.Name))
	return telegram.KeyReply{Admin: true, Key: key, Text: fmt.Sprintf(
		"New API key for %s below. Copy it now: Relay keeps only its hash, so it can't be shown again.\n\n"+
			"In the app set RELAY_API_KEY to it and RELAY_APP=%s.", c.Name, c.Name)}, nil
}

func revokeKey(ctx context.Context, clients *core.Clients, logger *slog.Logger, app string) (telegram.KeyReply, error) {
	err := clients.Revoke(ctx, app)
	if errors.Is(err, store.ErrNotFound) {
		return telegram.KeyReply{Admin: true, Text: "No app called " + app + " has an active key. Send /keys to see them."}, nil
	}
	if err != nil {
		return telegram.KeyReply{}, err
	}
	logger.InfoContext(ctx, "api key revoked", slog.String("channel", telegram.Name))
	return telegram.KeyReply{Admin: true, Text: app + "'s key no longer works. Send /newkey " + app + " to give it a new one."}, nil
}

func keysList(ctx context.Context, clients *core.Clients) (telegram.KeyReply, error) {
	all, err := clients.List(ctx)
	if err != nil {
		return telegram.KeyReply{}, err
	}
	var active, revoked []string
	for _, c := range all {
		if c.RevokedAt == nil {
			active = append(active, c.Name)
		} else {
			revoked = append(revoked, c.Name)
		}
	}
	if len(all) == 0 {
		return telegram.KeyReply{Admin: true, Text: "No apps have keys yet. Send /newkey <app> to make one."}, nil
	}
	text := "Apps with a working key:\n" + bullets(active)
	if len(revoked) > 0 {
		text += "\n\nRevoked:\n" + bullets(revoked)
	}
	return telegram.KeyReply{Admin: true, Text: text}, nil
}

func bullets(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return "• " + strings.Join(names, "\n• ")
}

// problem is a core.ErrInvalid error's message without the "invalid: " prefix.
func problem(err error) string {
	return strings.TrimPrefix(err.Error(), core.ErrInvalid.Error()+": ")
}
