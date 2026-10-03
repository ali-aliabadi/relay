package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/message"
	"github.com/ali-aliabadi/relay/internal/store"
)

const sendUsage = `Usage:
  relay send --to <username>[,<username>...] [--urgency normal] [--title T] [--wait 30s] <text>

Queues a message as the built-in "relay-cli" client. A running "relay serve"
delivers it; send waits up to --wait and prints the outcome.
`

// cliClientName is the client that `relay send` messages belong to.
const cliClientName = "relay-cli"

func sendCmd(e env, args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	to := fs.String("to", "", "comma-separated usernames")
	urgency := fs.String("urgency", "", "low, normal, high or critical")
	title := fs.String("title", "", "optional title")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for delivery (0 to not wait)")
	if err := fs.Parse(args); err != nil || *to == "" || fs.NArg() == 0 {
		return errUsage
	}
	req := message.Request{
		To: strings.Split(*to, ","), Urgency: *urgency, Title: *title, Text: strings.Join(fs.Args(), " "),
	}
	return e.withConfigStore(func(cfg config.Config, st *store.Store) error {
		clientID, err := cliClient(e.ctx, st)
		if err != nil {
			return err
		}
		msgs := core.NewMessages(st, buildChannels(cfg))
		m, _, err := msgs.Create(e.ctx, clientID, "", req)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "Queued %s.\n", m.ID)
		if *wait <= 0 {
			return nil
		}
		return waitForOutcome(e, msgs, clientID, m.ID, *wait)
	})
}

// cliClient returns the relay-cli client, creating it on first use with an
// unusable key: it exists only so CLI messages have an owner.
func cliClient(ctx context.Context, st *store.Store) (string, error) {
	clients, err := st.ListClients(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range clients {
		if c.Name == cliClientName {
			return c.ID, nil
		}
	}
	unusable := make([]byte, 32)
	if _, err := rand.Read(unusable); err != nil {
		return "", fmt.Errorf("creating cli client: %w", err)
	}
	c, err := st.CreateClient(ctx, cliClientName, unusable)
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

func waitForOutcome(e env, msgs *core.Messages, clientID, id string, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(e.ctx, wait)
	defer cancel()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		m, ds, err := msgs.Get(ctx, clientID, id)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if err == nil && m.Status != store.StatusQueued && m.Status != store.StatusSending {
			fmt.Fprintf(e.stdout, "Status: %s\n", m.Status)
			for _, d := range ds {
				if d.LastError != "" {
					fmt.Fprintf(e.stdout, "  %s via %s: %s (%s)\n", d.ID, d.Channel, d.Status, d.LastError)
				}
			}
			if m.Status == store.StatusFailed {
				return errors.New("delivery failed")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			fmt.Fprintf(e.stdout, "Still %s after %s. Is relay serve running?\n", m.Status, wait)
			return nil
		case <-tick.C:
		}
	}
}
