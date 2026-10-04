package core

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store"
)

// InviteLinkTTL is how long a one-time invite link works. Short, because
// whoever holds the link can claim it.
const InviteLinkTTL = 24 * time.Hour

// inviteTokenLen is the length of a token: 32 random bytes, base64url. It
// fits Telegram's 64-character ?start= limit and alphabet.
const inviteTokenLen = 43

// InviteLink returns a one-time token that links whoever opens it to the
// recipient called name (a username or alias) on Telegram. A missing
// recipient is added with displayName (name when empty) and timezone.
// Earlier links for that recipient stop working.
func (r *Recipients) InviteLink(ctx context.Context, name, displayName, timezone string, now time.Time) (store.Recipient, string, error) {
	found, err := r.store.RecipientsByNames(ctx, []string{name})
	if err != nil {
		return store.Recipient{}, "", err
	}
	rcp, ok := found[name]
	if !ok {
		if displayName == "" {
			displayName = name
		}
		rcp, err = r.Add(ctx, store.Recipient{Username: name, DisplayName: displayName, Timezone: timezone})
		if err != nil {
			return store.Recipient{}, "", err
		}
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return store.Recipient{}, "", fmt.Errorf("generating invite token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	err = r.store.ReplaceLinkToken(ctx, store.LinkToken{
		TokenHash: hashKey(token), RecipientID: rcp.ID, Channel: telegramChannel,
		CreatedAt: now, ExpiresAt: now.Add(InviteLinkTTL),
	})
	return rcp, token, err
}

// ClaimInviteLink links chatID to the recipient token was made for. ok is
// false when the token is malformed, unknown, used or expired. A pending
// invite by Telegram username for the same recipient is dropped.
func (r *Recipients) ClaimInviteLink(ctx context.Context, token, chatID string, now time.Time) (store.Recipient, bool, error) {
	if len(token) != inviteTokenLen {
		return store.Recipient{}, false, nil
	}
	rcpID, ch, err := r.store.ClaimLinkToken(ctx, hashKey(token), now)
	if errors.Is(err, store.ErrNotFound) || (err == nil && ch != telegramChannel) {
		return store.Recipient{}, false, nil
	}
	if err != nil {
		return store.Recipient{}, false, err
	}
	rcp, err := r.store.RecipientByID(ctx, rcpID)
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

// TelegramAdmin returns the recipient called name (a username or alias)
// and its linked Telegram chat. ok is false when either is missing.
func (r *Recipients) TelegramAdmin(ctx context.Context, name string) (rcp store.Recipient, chatID string, ok bool, err error) {
	if name == "" {
		return store.Recipient{}, "", false, nil
	}
	found, err := r.store.RecipientsByNames(ctx, []string{name})
	if err != nil {
		return store.Recipient{}, "", false, err
	}
	rcp, ok = found[name]
	if !ok {
		return store.Recipient{}, "", false, nil
	}
	c, err := r.store.Contact(ctx, rcp.ID, telegramChannel)
	if errors.Is(err, store.ErrNotFound) {
		return store.Recipient{}, "", false, nil
	}
	if err != nil {
		return store.Recipient{}, "", false, err
	}
	return rcp, c.Address, true, nil
}
