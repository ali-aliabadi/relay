package core

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/store"
)

// InviteTTL is how long an invite by Telegram username waits for /start.
// It bounds the window in which a released username could be taken over.
const InviteTTL = 7 * 24 * time.Hour

const telegramChannel = "telegram"

// Telegram usernames: 4-32 of a-z, 0-9 and _, starting with a letter, case-insensitive.
var telegramUsername = regexp.MustCompile(`^[a-z][a-z0-9_]{3,31}$`)

// NormalizeTelegramUsername strips "@", lowercases and validates a username.
func NormalizeTelegramUsername(s string) (string, error) {
	u := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@"))
	if !telegramUsername.MatchString(u) {
		return "", fmt.Errorf("%w: a Telegram username is 4-32 letters, digits or underscores, starting with a letter", ErrInvalid)
	}
	return u, nil
}

// InviteTelegram lets the Telegram account @tgUsername link itself to
// username by sending /start to the bot within InviteTTL. An existing link
// keeps working until the invite is claimed.
func (r *Recipients) InviteTelegram(ctx context.Context, username, tgUsername string, now time.Time) (store.Recipient, error) {
	handle, err := NormalizeTelegramUsername(tgUsername)
	if err != nil {
		return store.Recipient{}, err
	}
	rcp, err := r.store.RecipientByUsername(ctx, username)
	if err != nil {
		return store.Recipient{}, err
	}
	invites, err := r.store.Invites(ctx, telegramChannel, now.Add(-InviteTTL))
	if err != nil {
		return store.Recipient{}, err
	}
	for _, inv := range invites {
		if inv.Handle == handle && inv.RecipientID != rcp.ID {
			return store.Recipient{}, fmt.Errorf("%w: another recipient is already invited as this Telegram user", ErrInvalid)
		}
	}
	err = r.store.UpsertInvite(ctx, store.Invite{RecipientID: rcp.ID, Channel: telegramChannel, Handle: handle, CreatedAt: now})
	return rcp, err
}

// ClaimTelegramInvite links chatID to the recipient invited as tgUsername.
// ok is false when no unexpired invite names that username. tgUsername must
// come from Telegram (the message's sender), never from user input.
func (r *Recipients) ClaimTelegramInvite(ctx context.Context, tgUsername, chatID string, now time.Time) (store.Recipient, bool, error) {
	handle := strings.ToLower(tgUsername)
	// ponytail: decrypts every pending invite per /start; fine for a handful of people.
	invites, err := r.store.Invites(ctx, telegramChannel, now.Add(-InviteTTL))
	if err != nil || handle == "" {
		return store.Recipient{}, false, err
	}
	for _, inv := range invites {
		if inv.Handle != handle {
			continue
		}
		rcp, err := r.store.RecipientByID(ctx, inv.RecipientID)
		if err != nil {
			return store.Recipient{}, false, err
		}
		if err := r.store.UpsertContact(ctx, store.Contact{
			RecipientID: rcp.ID, Channel: telegramChannel, Address: chatID, VerifiedAt: &now,
		}); err != nil {
			return store.Recipient{}, false, err
		}
		return rcp, true, r.store.DeleteInvite(ctx, rcp.ID, telegramChannel)
	}
	return store.Recipient{}, false, nil
}

// InvitedRecipients returns the IDs of recipients with an unexpired invite.
func (r *Recipients) InvitedRecipients(ctx context.Context, now time.Time) (map[string]bool, error) {
	invites, err := r.store.Invites(ctx, telegramChannel, now.Add(-InviteTTL))
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(invites))
	for _, inv := range invites {
		out[inv.RecipientID] = true
	}
	return out, nil
}
