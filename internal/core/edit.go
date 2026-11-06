package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

// ErrNotEditable means a message isn't delivered, is redacted, has rich content,
// or its destination was relinked. Nothing is sent as a fallback.
var ErrNotEditable = errors.New("message cannot be edited")

// MaxEditText bounds a replacement so the title and source fit in the same part.
const MaxEditText = 2000

type editTarget struct {
	editor     channel.TextEditor
	contact    channel.Contact
	providerID string
}

// EditText updates only the caller's delivered, one-text-block message. The
// original encrypted archive and retention deadline are unchanged; edits are not archived.
func (m *Messages) EditText(ctx context.Context, clientID, messageID, text string) error {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > MaxEditText {
		return &message.ValidationError{Problems: []string{"text: must be 1-2000 characters"}}
	}
	original, ds, err := m.Get(ctx, clientID, messageID)
	if err != nil {
		return err
	}
	var blocks []message.Block
	if original.RedactedAt != nil || original.Status != store.StatusDelivered || json.Unmarshal(original.Blocks, &blocks) != nil || len(blocks) != 1 || blocks[0].Type != message.BlockText {
		return ErrNotEditable
	}
	targets, err := m.editTargets(ctx, original, ds)
	if err != nil {
		return err
	}
	edited := message.Message{Urgency: original.Urgency, Title: original.Title, Source: original.Source, Blocks: []message.Block{{Type: message.BlockText, Text: text}}}
	for _, target := range targets {
		if err := target.editor.EditText(ctx, target.contact, target.providerID, edited); err != nil {
			return err
		}
	}
	return nil
}

func (m *Messages) editTargets(ctx context.Context, original store.Message, ds []store.Delivery) ([]editTarget, error) {
	if len(ds) == 0 {
		return nil, ErrNotEditable
	}
	targets := make([]editTarget, 0, len(ds))
	for _, d := range ds {
		editor := m.textEditor(d.Channel)
		if editor == nil || d.Status != store.StatusDelivered || d.ProviderMessageID == "" {
			return nil, ErrNotEditable
		}
		contact, err := m.store.Contact(ctx, d.RecipientID, d.Channel)
		if err != nil {
			return nil, err
		}
		// Telegram IDs are scoped to a chat. Never use an old ID in a new chat:
		// only contacts verified before this message was queued can be edited.
		if contact.VerifiedAt == nil || contact.VerifiedAt.After(original.CreatedAt) {
			return nil, ErrNotEditable
		}
		targets = append(targets, editTarget{editor: editor, contact: channel.Contact{Address: contact.Address}, providerID: d.ProviderMessageID})
	}
	return targets, nil
}

func (m *Messages) textEditor(name string) channel.TextEditor {
	for _, ch := range m.channels {
		if ch.Name() == name {
			editor, _ := ch.(channel.TextEditor)
			return editor
		}
	}
	return nil
}
