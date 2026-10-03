package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/store/db"
)

// PurgeResult counts what one retention pass removed.
type PurgeResult struct {
	Redacted           int64 // messages whose content was dropped
	AttachmentsDeleted int64
	MessagesDeleted    int64 // messages (with deliveries) deleted entirely
}

// Purge drops content of messages created before contentCutoff and deletes
// messages created before metadataCutoff, in one transaction. secure_delete
// is on, so freed pages are zeroed rather than left in the file.
func (s *Store) Purge(ctx context.Context, contentCutoff, metadataCutoff time.Time) (PurgeResult, error) {
	var res PurgeResult
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if res.MessagesDeleted, err = q.DeleteMessagesBefore(ctx, formatTime(metadataCutoff)); err != nil {
			return fmt.Errorf("deleting old messages: %w", err)
		}
		if res.AttachmentsDeleted, err = q.DeleteAttachmentsBefore(ctx, formatTime(contentCutoff)); err != nil {
			return fmt.Errorf("deleting attachments: %w", err)
		}
		if res.Redacted, err = q.RedactMessagesBefore(ctx, db.RedactMessagesBeforeParams{
			Now: nullString(formatTime(s.clock())), Cutoff: formatTime(contentCutoff),
		}); err != nil {
			return fmt.Errorf("redacting messages: %w", err)
		}
		return nil
	})
	return res, err
}
