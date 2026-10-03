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
	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

// background is everything serve runs besides the public API: the delivery
// worker, the retention job and the internal metrics listener.
type background struct {
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	workerErr error
	metrics   *http.Server
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
	return b, nil
}

// stop cancels background work and waits for it to finish.
func (b *background) stop(ctx context.Context) error {
	b.cancel()
	b.wg.Wait()
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
