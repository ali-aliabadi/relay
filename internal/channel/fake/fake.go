// Package fake is an in-memory Channel for tests.
package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/ali-aliabadi/relay/internal/channel"
	"github.com/ali-aliabadi/relay/internal/message"
)

// Sent is one recorded send.
type Sent struct {
	To  channel.Contact
	Msg message.Message
}

// Channel records sends and can be told to fail.
type Channel struct {
	name string

	mu    sync.Mutex
	sent  []Sent
	fails []error // returned by the next sends, in order
}

// New returns a fake channel called name.
func New(name string) *Channel { return &Channel{name: name} }

// Name implements channel.Channel.
func (c *Channel) Name() string { return c.name }

// FailNext makes the next len(errs) sends return these errors in order.
func (c *Channel) FailNext(errs ...error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fails = append(c.fails, errs...)
}

// Send implements channel.Channel.
func (c *Channel) Send(_ context.Context, to channel.Contact, msg message.Message) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.fails) > 0 {
		err := c.fails[0]
		c.fails = c.fails[1:]
		return "", err
	}
	c.sent = append(c.sent, Sent{To: to, Msg: msg})
	return fmt.Sprintf("fake-%d", len(c.sent)), nil
}

// Preview implements channel.Channel with one plain text part per block.
func (c *Channel) Preview(msg message.Message) (channel.Preview, error) {
	p := channel.Preview{}
	for _, b := range msg.Blocks {
		p.Parts = append(p.Parts, channel.Part{Kind: "text", Text: b.Type + ": " + b.Text})
	}
	return p, nil
}

// Sent returns a copy of every successful send so far.
func (c *Channel) Sent() []Sent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Sent(nil), c.sent...)
}
