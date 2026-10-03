package core

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store"
)

// AddAlias makes alias another name for username, usable anywhere a
// username is accepted in `to`. Aliases and usernames share one namespace.
func (r *Recipients) AddAlias(ctx context.Context, username, alias string, now time.Time) error {
	if err := validateName(alias); err != nil {
		return err
	}
	rcp, err := r.store.RecipientByUsername(ctx, username)
	if err != nil {
		return err
	}
	if err := r.nameIsFree(ctx, alias); err != nil {
		return err
	}
	return r.store.CreateAlias(ctx, alias, rcp.ID, now)
}

// RemoveAlias deletes an alias. The recipient is untouched.
func (r *Recipients) RemoveAlias(ctx context.Context, alias string) error {
	return r.store.DeleteAlias(ctx, alias)
}

// Aliases returns a recipient's aliases, sorted.
func (r *Recipients) Aliases(ctx context.Context, recipientID string) ([]string, error) {
	return r.store.Aliases(ctx, recipientID)
}

// nameIsFree reports ErrConflict when name is already a username or alias.
func (r *Recipients) nameIsFree(ctx context.Context, name string) error {
	found, err := r.store.RecipientsByNames(ctx, []string{name})
	if err != nil {
		return err
	}
	if _, taken := found[name]; taken {
		return fmt.Errorf("%q is already a recipient's username or alias: %w", name, store.ErrConflict)
	}
	return nil
}
