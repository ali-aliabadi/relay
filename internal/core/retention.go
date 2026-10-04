package core

import (
	"context"
	"log/slog"
	"time"

	"github.com/ali-aliabadi/relay/internal/store"
)

// MetadataRetention is how long message and delivery metadata are kept.
const MetadataRetention = 180 * 24 * time.Hour

// Retention purges message content after Days and metadata after 180 days.
type Retention struct {
	Store    *store.Store
	Days     int
	Logger   *slog.Logger
	Clock    func() time.Time
	Interval time.Duration // between passes; 24h when zero
}

// Run purges once at start and then every Interval until ctx is cancelled.
func (r *Retention) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		r.Pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Pass runs one purge and logs a line only when something was removed.
func (r *Retention) Pass(ctx context.Context) {
	now := r.Clock()
	res, err := r.Store.Purge(ctx, now.Add(-time.Duration(r.Days)*24*time.Hour), now.Add(-MetadataRetention))
	if err == nil {
		res.InvitesDeleted, err = r.Store.DeleteInvitesBefore(ctx, now.Add(-InviteTTL))
	}
	if err == nil {
		var links int64
		links, err = r.Store.DeleteLinkTokensBefore(ctx, now)
		res.InvitesDeleted += links
	}
	if err != nil {
		if ctx.Err() == nil {
			r.Logger.Error("retention pass failed", slog.String("error", err.Error()))
		}
		return
	}
	if res.Redacted+res.AttachmentsDeleted+res.AnswersDeleted+res.MessagesDeleted+res.InvitesDeleted > 0 {
		r.Logger.Info("retention purge",
			slog.Int64("messages_redacted", res.Redacted),
			slog.Int64("attachments_deleted", res.AttachmentsDeleted),
			slog.Int64("answers_deleted", res.AnswersDeleted),
			slog.Int64("invites_deleted", res.InvitesDeleted),
			slog.Int64("messages_deleted", res.MessagesDeleted))
	}
}
