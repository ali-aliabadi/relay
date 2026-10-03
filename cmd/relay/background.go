package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/channel/telegram"
	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

// background is everything serve runs besides the public API: the delivery
// worker, the retention job, the Telegram poller and the internal metrics listener.
type background struct {
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	workerErr error
	metrics   *http.Server
	webhooks  *core.Webhooks
}

func startBackground(ctx context.Context, cfg config.Config, logger *slog.Logger, st *store.Store,
	channels []channel.Channel, metrics *obs.Metrics,
) (*background, error) {
	bctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	b := &background{cancel: cancel}

	if cfg.MetricsAddr != "" {
		var lc net.ListenConfig
		ln, err := lc.Listen(ctx, "tcp", cfg.MetricsAddr)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("listening on %s for metrics: %w", cfg.MetricsAddr, err)
		}
		b.metrics = &http.Server{
			Handler: metrics.Handler(cfg.Pprof), ReadHeaderTimeout: 5 * time.Second,
			ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		}
		go func() {
			if err := b.metrics.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics listener stopped", slog.String("error", err.Error()))
			}
		}()
	}

	worker := &core.Worker{
		Store: st, Channels: channels, Logger: logger, Clock: time.Now, Poll: cfg.WorkerPollInterval,
		OnOutcome: metrics.DeliveryOutcome,
	}
	retention := &core.Retention{Store: st, Days: cfg.RetentionDays, Logger: logger, Clock: time.Now}
	b.wg.Add(2)
	go func() { defer b.wg.Done(); b.workerErr = worker.Run(bctx) }()
	go func() { defer b.wg.Done(); retention.Run(bctx) }()
	b.webhooks = core.NewWebhooks(logger)
	if p := newPoller(cfg, logger, st, b.webhooks); p != nil {
		b.wg.Go(func() { p.Run(bctx) })
	}
	return b, nil
}

// newPoller returns the Telegram update reader, or nil without a bot token.
func newPoller(cfg config.Config, logger *slog.Logger, st *store.Store, webhooks *core.Webhooks) *telegram.Poller {
	if cfg.TelegramBotToken == "" {
		return nil
	}
	answers := &core.Answers{Store: st, Logger: logger, Notify: webhooks.Notify}
	recipients := core.NewRecipients(st)
	return &telegram.Poller{
		Client:   telegram.NewClient(cfg.TelegramAPIURL, cfg.TelegramBotToken, nil),
		OnAnswer: answers.Record,
		LinkCode: func(chatID string) (string, error) { return recipients.LinkCode(telegram.Name, chatID, time.Now()) },
		ClaimInvite: func(ctx context.Context, username, chatID string) (string, bool, error) {
			rcp, ok, err := recipients.ClaimTelegramInvite(ctx, username, chatID, time.Now())
			if ok {
				logger.InfoContext(ctx, "recipient linked by invite", slog.String("recipient_id", rcp.ID), slog.String("channel", telegram.Name))
			}
			return rcp.DisplayName, ok, err
		},
		Logger: logger,
	}
}

// stop cancels background work and waits for it to finish.
func (b *background) stop(ctx context.Context) error {
	b.cancel()
	b.wg.Wait()
	b.webhooks.Wait()
	if b.metrics != nil {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = b.metrics.Shutdown(sctx)
	}
	if b.workerErr != nil {
		return fmt.Errorf("delivery worker: %w", b.workerErr)
	}
	return nil
}
