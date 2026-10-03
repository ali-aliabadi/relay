package obs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	b, _ := io.ReadAll(rec.Body)
	return rec.Code, string(b)
}

func TestMetrics(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	m := NewMetrics(t.Context(), func(context.Context) (int64, time.Time, error) {
		return 3, now.Add(-90 * time.Second), nil
	}, func() time.Time { return now })
	m.MessageAccepted("high")
	m.DeliveryOutcome("telegram", "delivered", 120*time.Millisecond)
	m.DeliveryOutcome("telegram", "retry", time.Second)

	code, body := scrape(t, m.Handler(false), "/metrics")
	for _, want := range []string{
		`relay_messages_total{urgency="high"} 1`,
		`relay_deliveries_total{channel="telegram",result="delivered"} 1`,
		`relay_delivery_duration_seconds_count{channel="telegram"} 2`,
		`relay_queue_depth 3`,
		`relay_oldest_queued_seconds 90`,
		`go_goroutines`,
	} {
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	if code, _ := scrape(t, m.Handler(false), "/debug/pprof/"); code != http.StatusNotFound {
		t.Errorf("pprof served while disabled: %d", code)
	}
	if code, _ := scrape(t, m.Handler(true), "/debug/pprof/"); code != http.StatusOK {
		t.Errorf("pprof not served when enabled: %d", code)
	}
}

func TestQueueMetricsOnError(t *testing.T) {
	m := NewMetrics(t.Context(), func(context.Context) (int64, time.Time, error) {
		return 0, time.Time{}, errors.New("db gone")
	}, time.Now)
	_, body := scrape(t, m.Handler(false), "/metrics")
	if !strings.Contains(body, "relay_queue_depth -1") || !strings.Contains(body, "relay_oldest_queued_seconds 0") {
		t.Errorf("error values wrong:\n%s", body)
	}
}
