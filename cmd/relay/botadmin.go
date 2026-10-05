package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/store"
)

// botInvite answers /invite <username> [display name] with a one-time
// invite link, but only in the admin's own linked chat. Returns nil (the
// command is off) when no admin is configured.
func botInvite(admin string, recipients *core.Recipients, logger *slog.Logger,
) func(ctx context.Context, chatID, args string) (telegram.InviteReply, error) {
	if admin == "" {
		return nil
	}
	return func(ctx context.Context, chatID, args string) (telegram.InviteReply, error) {
		adm, adminChat, ok, err := recipients.TelegramAdmin(ctx, admin)
		if err != nil || !ok || adminChat != chatID {
			return telegram.InviteReply{}, err
		}
		username, displayName, _ := strings.Cut(args, " ")
		if username == "" {
			return telegram.InviteReply{Admin: true, Problem: "say who to invite."}, nil
		}
		rcp, token, err := recipients.InviteLink(ctx, strings.ToLower(username), strings.TrimSpace(displayName), adm.Timezone, time.Now())
		if errors.Is(err, core.ErrInvalid) || errors.Is(err, store.ErrConflict) {
			return telegram.InviteReply{Admin: true, Problem: strings.TrimPrefix(err.Error(), core.ErrInvalid.Error()+": ")}, nil
		}
		if err != nil {
			return telegram.InviteReply{}, err
		}
		logger.InfoContext(ctx, "invite link created", slog.String("recipient_id", rcp.ID), slog.String("channel", telegram.Name))
		return telegram.InviteReply{Admin: true, Name: rcp.DisplayName, Token: token, ValidFor: core.InviteLinkTTL}, nil
	}
}

// botClaimLink links the chat that opened an invite link and finds the
// admin's chat to tell them.
func botClaimLink(admin string, recipients *core.Recipients, logger *slog.Logger,
) func(ctx context.Context, token, chatID string) (telegram.Claimed, error) {
	return func(ctx context.Context, token, chatID string) (telegram.Claimed, error) {
		rcp, ok, err := recipients.ClaimInviteLink(ctx, token, chatID, time.Now())
		if err != nil || !ok {
			return telegram.Claimed{}, err
		}
		logger.InfoContext(ctx, "recipient linked by invite link", slog.String("recipient_id", rcp.ID), slog.String("channel", telegram.Name))
		_, adminChat, _, err := recipients.TelegramAdmin(ctx, admin)
		if err != nil {
			logger.WarnContext(ctx, "finding admin chat", slog.String("error", err.Error()))
		}
		return telegram.Claimed{OK: true, Name: rcp.DisplayName, AdminChat: adminChat}, nil
	}
}

// botAdminChat finds the admin's linked chat for the bot's command menu.
// Returns nil (no admin menu) when no admin is configured.
func botAdminChat(admin string, recipients *core.Recipients) func(ctx context.Context) (string, error) {
	if admin == "" {
		return nil
	}
	return func(ctx context.Context) (string, error) {
		_, chat, _, err := recipients.TelegramAdmin(ctx, admin)
		return chat, err
	}
}
