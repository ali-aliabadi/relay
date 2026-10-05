package telegram

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
)

// command is one entry in the bot's "/" menu.
type command struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// Everyone's menu, and the admin's (only set when the admin commands are on).
var (
	userCommands = []command{
		{"start", "Link this chat to Relay"},
		{"help", "What this bot does"},
	}
	adminCommands = append(append([]command(nil), userCommands...),
		command{"invite", "<username> [name]: one-time link to add someone"},
		command{"newkey", "<app>: new API key for an app"},
		command{"keys", "List apps with API keys"},
		command{"revoke", "<app>: turn an app's API key off"},
	)
)

const userHelp = "Relay sends you notifications here.\n\n" +
	"To answer a question, reply to its message or tap a button.\n" +
	"/start links this chat to Relay."

// syncMenu sets the "/" menu: the user commands for everyone and, in the
// admin's chat only, the admin commands too. The menu only hides commands;
// each admin command still checks the chat itself. Failures are logged and
// otherwise ignored.
func (p *Poller) syncMenu(ctx context.Context) {
	if err := p.Client.setCommands(ctx, userCommands, nil); err != nil {
		p.Logger.WarnContext(ctx, "setting telegram command menu", slog.String("error", err.Error()))
		return
	}
	chat, ok := p.adminChat(ctx)
	if !ok {
		return
	}
	scope := map[string]any{"type": "chat", "chat_id": chat}
	if err := p.Client.setCommands(ctx, adminCommands, scope); err != nil {
		p.Logger.WarnContext(ctx, "setting telegram admin command menu", slog.String("error", err.Error()))
	}
}

// adminChat returns the admin's linked chat when admin commands are on.
func (p *Poller) adminChat(ctx context.Context) (int64, bool) {
	if p.AdminChat == nil {
		return 0, false
	}
	s, err := p.AdminChat(ctx)
	if err != nil {
		p.Logger.WarnContext(ctx, "finding admin chat", slog.String("error", err.Error()))
		return 0, false
	}
	chat, err := strconv.ParseInt(s, 10, 64)
	return chat, err == nil
}

// handleHelp lists the commands this chat can use, refreshing the menu so a
// newly linked admin sees theirs.
func (p *Poller) handleHelp(ctx context.Context, chat int64) error {
	p.syncMenu(ctx)
	if admin, ok := p.adminChat(ctx); !ok || admin != chat {
		return p.Client.SendText(ctx, chat, userHelp, 0)
	}
	lines := make([]string, len(adminCommands))
	for i, c := range adminCommands {
		lines[i] = "/" + c.Command + " " + c.Description
	}
	return p.Client.SendText(ctx, chat, "Your commands (only you see the admin ones):\n\n"+strings.Join(lines, "\n"), 0)
}

// setCommands replaces the bot's command menu for scope (nil: everyone).
func (c *Client) setCommands(ctx context.Context, cmds []command, scope map[string]any) error {
	params := map[string]any{"commands": cmds}
	if scope != nil {
		params["scope"] = scope
	}
	return c.call(ctx, "setMyCommands", params, nil)
}
