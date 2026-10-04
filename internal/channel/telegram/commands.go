package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// InviteReply is what the admin's /invite produced.
type InviteReply struct {
	Admin    bool          // false: not the admin's chat, so /invite is treated like any text
	Problem  string        // why no invite was made; shown to the admin
	Name     string        // the invitee's display name
	Token    string        // goes in the bot's ?start= link; never logged
	ValidFor time.Duration // how long the link works
}

// Claimed is the outcome of opening an invite link.
type Claimed struct {
	OK        bool   // false: the link is unknown, used or expired
	Name      string // the linked recipient's display name
	AdminChat string // told about the new link when set; private
}

const (
	inviteUsage = "Use: /invite <username> [display name], e.g. /invite sara Sara"
	linkGone    = "This invite link has expired or was already used. Ask for a new one."
)

// splitCommand returns a message's bot command ("/invite", without any
// "@botname") and the text after it.
func splitCommand(text string) (cmd, args string) {
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	cmd, args, _ = strings.Cut(text, " ")
	cmd, _, _ = strings.Cut(cmd, "@")
	return cmd, strings.TrimSpace(args)
}

// handleInvite answers the admin's /invite with a one-time link. handled is
// false for anyone else, who gets the usual reply instead.
func (p *Poller) handleInvite(ctx context.Context, chat int64, args string) (handled bool, err error) {
	if p.Invite == nil {
		return false, nil
	}
	res, err := p.Invite(ctx, strconv.FormatInt(chat, 10), args)
	if err != nil {
		_ = p.Client.SendText(ctx, chat, "Something went wrong, please try again.", 0)
		return true, err
	}
	switch {
	case !res.Admin:
		return false, nil
	case res.Problem != "":
		return true, p.Client.SendText(ctx, chat, "Couldn't make an invite: "+res.Problem+"\n\n"+inviteUsage, 0)
	}
	bot, err := p.botUsername(ctx)
	if err != nil {
		_ = p.Client.SendText(ctx, chat, "Something went wrong, please try again.", 0)
		return true, err
	}
	text := fmt.Sprintf("Invite for %s is ready. Send them this link; it works once, within %d hours:\n\n"+
		"https://t.me/%s?start=%s\n\nThey tap Start and are linked. I'll tell you here when they do.",
		res.Name, int(res.ValidFor.Hours()), bot, res.Token)
	return true, p.Client.SendText(ctx, chat, text, 0)
}

// handleInviteLink links the chat that opened an invite link.
func (p *Poller) handleInviteLink(ctx context.Context, chat int64, token string) error {
	res, err := p.ClaimLink(ctx, token, strconv.FormatInt(chat, 10))
	if err != nil {
		_ = p.Client.SendText(ctx, chat, "Something went wrong, please try again.", 0)
		return err
	}
	if !res.OK {
		return p.Client.SendText(ctx, chat, linkGone, 0)
	}
	if err := p.Client.SendText(ctx, chat, "Linked to Relay as "+res.Name+". Notifications will arrive here.", 0); err != nil {
		return err
	}
	if admin, err := strconv.ParseInt(res.AdminChat, 10, 64); err == nil && admin != chat {
		return p.Client.SendText(ctx, admin, res.Name+" opened your invite and is now linked to Relay.", 0)
	}
	return nil
}

// botUsername returns the bot's @username, asking Telegram once.
func (p *Poller) botUsername(ctx context.Context) (string, error) {
	if p.bot != "" {
		return p.bot, nil
	}
	name, err := p.Client.BotUsername(ctx)
	if err != nil {
		return "", err
	}
	p.bot = name
	return name, nil
}
