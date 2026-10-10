package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/core"
)

// recipientsReply answers the admin's /recipients: every recipient with the
// names apps can send to and whether they can receive yet. Chat IDs and
// other addresses are never shown.
func recipientsReply(ctx context.Context, recipients *core.Recipients) (telegram.KeyReply, error) {
	all, err := recipients.List(ctx)
	if err != nil {
		return telegram.KeyReply{}, err
	}
	if len(all) == 0 {
		return telegram.KeyReply{Admin: true, Text: "No recipients yet. Send /invite <username> [name] to add someone."}, nil
	}
	lines := make([]string, 0, len(all))
	for _, rcp := range all {
		aliases, err := recipients.Aliases(ctx, rcp.ID)
		if err != nil {
			return telegram.KeyReply{}, err
		}
		linked, err := recipients.Channels(ctx, rcp.ID)
		if err != nil {
			return telegram.KeyReply{}, err
		}
		line := fmt.Sprintf("%s (%s)", rcp.Username, rcp.DisplayName)
		if len(aliases) > 0 {
			line += ", also " + strings.Join(aliases, ", ")
		}
		if len(linked) > 0 {
			line += ": linked on " + strings.Join(linked, ", ")
		} else {
			line += ": not linked yet"
		}
		lines = append(lines, line)
	}
	return telegram.KeyReply{Admin: true, Text: "Recipients apps can send to:\n" + bullets(lines)}, nil
}
