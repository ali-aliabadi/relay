package obs

import (
	"context"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// QueueStatsFunc reports queued deliveries and the oldest due time.
type QueueStatsFunc func(ctx context.Context) (depth int64, oldest time.Time, err error)

// Metrics holds Relay's Prometheus metrics. Labels are low-cardinality only:
// channel, urgency, result. Never recipients, clients or message IDs.
type Metrics struct {
	registry   *prometheus.Registry
	messages   *prometheus.CounterVec
	deliveries *prometheus.CounterVec
	duration   *prometheus.HistogramVec
}

// NewMetrics registers Relay's metrics plus Go runtime and process metrics.
// queue is read at scrape time, under ctx's values but not its cancellation.
func NewMetrics(ctx context.Context, queue QueueStatsFunc, clock func() time.Time) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		messages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_messages_total", Help: "Messages accepted, by urgency.",
		}, []string{"urgency"}),
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "relay_deliveries_total", Help: "Delivery attempts, by channel and result (delivered, retry, failed).",
		}, []string{"channel", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "relay_delivery_duration_seconds", Help: "Time spent on one delivery attempt.",
			Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 15},
		}, []string{"channel"}),
	}
	stats := func() (int64, time.Time) {
		qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		depth, oldest, err := queue(qctx)
		if err != nil {
			return -1, time.Time{}
		}
		return depth, oldest
	}
	m.registry.MustRegister(
		m.messages, m.deliveries, m.duration,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "relay_queue_depth", Help: "Queued deliveries (-1 if unreadable).",
		}, func() float64 { d, _ := stats(); return float64(d) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "relay_oldest_queued_seconds", Help: "Age of the oldest due queued delivery, 0 when none.",
		}, func() float64 {
			_, oldest := stats()
			if oldest.IsZero() {
				return 0
			}
			return max(clock().Sub(oldest).Seconds(), 0)
		}),
	)
	return m
}

// MessageAccepted counts one new message.
func (m *Metrics) MessageAccepted(urgency string) { m.messages.WithLabelValues(urgency).Inc() }

// DeliveryOutcome counts one delivery attempt and its duration.
func (m *Metrics) DeliveryOutcome(channel, result string, took time.Duration) {
	m.deliveries.WithLabelValues(channel, result).Inc()
	m.duration.WithLabelValues(channel).Observe(took.Seconds())
}

// Handler serves /metrics and, when pprof is true, /debug/pprof/. Mount it
// only on the internal listener.
func (m *Metrics) Handler(withPprof bool) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	if withPprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	return mux
}
