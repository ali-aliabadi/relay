package core

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

// ErrNotDeletable means a message cannot safely be removed from its destination.
var ErrNotDeletable = errors.New("message cannot be deleted")

type deleteTarget struct {
	deleter    channel.TextDeleter
	contact    channel.Contact
	providerID string
}

// DeleteText only removes the caller's delivered, unredacted single-part plain
// text message from the provider. The encrypted archive and retention stay intact.
// Preflight every recipient before touching any chat; retries never resend.
func (m *Messages) DeleteText(ctx context.Context, clientID, messageID string) error {
	original, ds, err := m.Get(ctx, clientID, messageID)
	if err != nil {
		return err
	}
	var blocks []message.Block
	if original.RedactedAt != nil || original.Status != store.StatusDelivered || json.Unmarshal(original.Blocks, &blocks) != nil || len(blocks) != 1 || blocks[0].Type != message.BlockText {
		return ErrNotDeletable
	}
	editable, err := m.editTargets(ctx, original, ds)
	if err != nil {
		if errors.Is(err, ErrNotEditable) {
			return ErrNotDeletable
		}
		return err
	}
	targets, err := m.deleteTargets(original, blocks, editable)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := target.deleter.DeleteText(ctx, target.contact, target.providerID); err != nil {
			return err
		}
	}
	return nil
}

func (m *Messages) deleteTargets(original store.Message, blocks []message.Block, editable []editTarget) ([]deleteTarget, error) {
	targets := make([]deleteTarget, 0, len(editable))
	msg := message.Message{Urgency: original.Urgency, Title: original.Title, Source: original.Source, Blocks: blocks}
	for _, target := range editable {
		deleter, ok := target.editor.(channel.TextDeleter)
		previewer, canPreview := target.editor.(channel.Channel)
		if !ok || !canPreview {
			return nil, ErrNotDeletable
		}
		p, err := previewer.Preview(msg)
		if err != nil || len(p.Parts) != 1 || p.Parts[0].Kind != "text" || len(p.Parts[0].Buttons) != 0 {
			return nil, ErrNotDeletable
		}
		targets = append(targets, deleteTarget{deleter: deleter, contact: target.contact, providerID: target.providerID})
	}
	return targets, nil
}
