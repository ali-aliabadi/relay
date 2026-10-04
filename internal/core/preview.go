package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

// Preview validates req like Create and returns what each channel would send
// (the request's channels, or every configured one). Nothing is stored and
// recipients are not resolved.
func (m *Messages) Preview(req message.Request) (map[string]channel.Preview, error) {
	req, atts, err := message.Normalize(req)
	if err != nil {
		return nil, err
	}
	if err := m.checkChannels(req.Channels); err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, ch := range req.Channels {
		want[ch] = true
	}
	msg := message.Message{Urgency: req.Urgency, Title: req.Title, Source: req.Source, Blocks: req.Blocks, Attachments: atts}
	out := map[string]channel.Preview{}
	for _, ch := range m.channels {
		if len(want) > 0 && !want[ch.Name()] {
			continue
		}
		p, err := ch.Preview(msg)
		if err != nil {
			return nil, fmt.Errorf("previewing %s: %w", ch.Name(), err)
		}
		out[ch.Name()] = p
	}
	return out, nil
}

// ChannelStatus is one configured channel and whether it can send.
type ChannelStatus struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

// healthTimeout bounds each channel's health check.
const healthTimeout = 5 * time.Second

// Channels reports every configured channel's health.
func (m *Messages) Channels(ctx context.Context) []ChannelStatus {
	out := make([]ChannelStatus, 0, len(m.channels))
	for _, ch := range m.channels {
		st := ChannelStatus{Name: ch.Name(), Healthy: true}
		if hc, ok := ch.(channel.HealthChecker); ok {
			hctx, cancel := context.WithTimeout(ctx, healthTimeout)
			err := hc.Health(hctx)
			cancel()
			if err != nil {
				st.Healthy = false
				st.Error = "unavailable"
				var ce *channel.Error
				if errors.As(err, &ce) {
					st.Error = ce.Reason
				}
			}
		}
		out = append(out, st)
	}
	return out
}
