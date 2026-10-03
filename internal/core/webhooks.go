package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"syscall"
	"time"
)

// Webhooks tells apps an answer arrived. The body is only the message ID;
// the app then fetches the answer through the authenticated API, so a leaked
// or wrong webhook URL exposes nothing.
type Webhooks struct {
	Client *http.Client
	Logger *slog.Logger
	Delays []time.Duration // between attempts

	wg sync.WaitGroup
}

// NewWebhooks returns a notifier that only reaches public addresses and
// doesn't follow redirects, so callers can't aim Relay at internal services.
func NewWebhooks(logger *slog.Logger) *Webhooks {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: publicOnly}
	return &Webhooks{
		Client: &http.Client{
			Timeout:       10 * time.Second,
			Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		Logger: logger,
		Delays: []time.Duration{5 * time.Second, 30 * time.Second},
	}
}

var errNotPublic = errors.New("webhook address is not public")

// publicOnly runs after DNS resolution, so a hostname resolving to a private
// address is refused too.
func publicOnly(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return errNotPublic
	}
	ip := ap.Addr().Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return errNotPublic
	}
	return nil
}

// Notify posts {"message_id"} to url in the background, retrying a few
// times. The app can always poll, so a lost notification is not fatal.
// ponytail: in-memory retries, lost on restart; persist them if apps rely on webhooks alone.
func (w *Webhooks) Notify(ctx context.Context, url, messageID string) {
	w.wg.Go(func() {
		for i := 0; ; i++ {
			err := w.post(ctx, url, messageID)
			if err == nil || ctx.Err() != nil {
				return
			}
			if i >= len(w.Delays) || errors.Is(err, errNotPublic) { // retrying can't make an address public
				// The URL may hold a secret, so only the message ID is logged.
				w.Logger.Warn("answer webhook failed", slog.String("message_id", messageID), slog.String("error", err.Error()))
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.Delays[i]):
			}
		}
	})
}

func (w *Webhooks) post(ctx context.Context, url, messageID string) error {
	body, _ := json.Marshal(map[string]string{"event": "answer", "message_id": messageID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return errors.New("building request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		if errors.Is(err, errNotPublic) {
			return errNotPublic
		}
		return errors.New("request failed") // net errors include the URL
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// Wait blocks until notifications in flight finish (their ctx bounds them).
func (w *Webhooks) Wait() { w.wg.Wait() }
