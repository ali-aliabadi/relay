package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LinkCodeTTL is how long a link code from the bot stays valid.
const LinkCodeTTL = time.Hour

const linkPurpose = "link-code"

// ErrBadLinkCode means a link code is malformed, forged or expired.
var ErrBadLinkCode = errors.New("link code is invalid or expired; send /start to the bot for a new one")

// LinkCode returns a code proving address on channel asked to be linked.
// It holds the address encrypted, so showing it to its owner leaks nothing.
func (r *Recipients) LinkCode(channelName, address string, now time.Time) (string, error) {
	exp := strconv.FormatInt(now.Add(LinkCodeTTL).Unix(), 10)
	return r.store.SealToken(linkPurpose, []byte(channelName+"|"+address+"|"+exp))
}

// LinkWithCode links username to the contact in code and returns the
// channel and address it linked.
func (r *Recipients) LinkWithCode(ctx context.Context, username, code string, now time.Time) (channelName, address string, err error) {
	plain, err := r.store.OpenToken(linkPurpose, strings.TrimSpace(code))
	if err != nil {
		return "", "", ErrBadLinkCode
	}
	parts := strings.Split(string(plain), "|")
	if len(parts) != 3 {
		return "", "", ErrBadLinkCode
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || now.Unix() > exp {
		return "", "", ErrBadLinkCode
	}
	if err := r.Link(ctx, username, parts[0], parts[1], now); err != nil {
		return "", "", fmt.Errorf("linking: %w", err)
	}
	return parts[0], parts[1], nil
}
