package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ali-aliabadi/relay/internal/config"
	"github.com/ali-aliabadi/relay/internal/core"
	"github.com/ali-aliabadi/relay/internal/obs"
	"github.com/ali-aliabadi/relay/internal/store"
)

// errUsage makes run print the command's usage and exit 2.
var errUsage = errors.New("usage")

// env is what every admin command gets.
type env struct {
	ctx    context.Context
	lookup config.LookupFunc
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// withStore opens the database (applying migrations) for one admin command.
// Logs go to stderr so stdout carries only the command's output.
func (e env) withStore(fn func(*store.Store) error) error {
	return e.withConfigStore(func(_ config.Config, st *store.Store) error { return fn(st) })
}

// withConfigStore is withStore for commands that also need the config.
func (e env) withConfigStore(fn func(config.Config, *store.Store) error) error {
	cfg, err := config.Load(e.lookup)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	st, conn, err := openStore(e.ctx, cfg, obs.NewLogger(e.stderr, cfg.LogLevel))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	return fn(cfg, st)
}

const clientsUsage = `Usage:
  relay clients create <name>   create a client and print its API key once
  relay clients list            list clients
  relay clients revoke <name>   revoke a client's key
`

func clientsCmd(e env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	return e.withStore(func(st *store.Store) error {
		svc := core.NewClients(st)
		switch {
		case args[0] == "create" && len(args) == 2:
			c, key, err := svc.Create(e.ctx, args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(e.stdout, "Created client %s (%s).\nAPI key (shown only once, store it now):\n\n  %s\n", c.Name, c.ID, key)
			return nil
		case args[0] == "list" && len(args) == 1:
			return clientsList(e, svc)
		case args[0] == "revoke" && len(args) == 2:
			if err := svc.Revoke(e.ctx, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(e.stdout, "Revoked client %s.\n", args[1])
			return nil
		default:
			return errUsage
		}
	})
}

func clientsList(e env, svc *core.Clients) error {
	clients, err := svc.List(e.ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tID\tCREATED\tSTATUS")
	for _, c := range clients {
		status := "active"
		if c.RevokedAt != nil {
			status = "revoked " + c.RevokedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.Name, c.ID, c.CreatedAt.Format(time.RFC3339), status)
	}
	return tw.Flush()
}

const recipientsUsage = `Usage:
  relay recipients add <username> --name "Display Name" [--timezone Europe/Berlin]
  relay recipients list
  relay recipients remove <username>
  relay recipients link <username>     link Telegram: then send /start to the bot from their phone
`

func recipientsCmd(e env, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "add":
		return recipientsAdd(e, args[1:])
	case "list":
		if len(args) != 1 {
			return errUsage
		}
		return e.withStore(func(st *store.Store) error { return recipientsList(e, core.NewRecipients(st)) })
	case "link":
		if len(args) != 2 {
			return errUsage
		}
		return recipientsLink(e, args[1])
	case "remove":
		if len(args) != 2 {
			return errUsage
		}
		return e.withStore(func(st *store.Store) error {
			if err := core.NewRecipients(st).Remove(e.ctx, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(e.stdout, "Removed recipient %s.\n", args[1])
			return nil
		})
	default:
		return errUsage
	}
}

func recipientsAdd(e env, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return errUsage
	}
	username := args[0]
	fs := flag.NewFlagSet("recipients add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	name := fs.String("name", "", "display name")
	tz := fs.String("timezone", "UTC", "IANA timezone")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	return e.withStore(func(st *store.Store) error {
		r, err := core.NewRecipients(st).Add(e.ctx, store.Recipient{Username: username, DisplayName: *name, Timezone: *tz})
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "Added recipient %s (%s).\n", r.Username, r.ID)
		return nil
	})
}

func recipientsList(e env, svc *core.Recipients) error {
	rs, err := svc.List(e.ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "USERNAME\tNAME\tTIMEZONE\tCHANNELS\tLINKED")
	for _, r := range rs {
		linked, err := svc.Channels(e.ctx, r.ID)
		if err != nil {
			return err
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Username, r.DisplayName, r.Timezone,
			strings.Join(r.ChannelPreference, ","), orDash(strings.Join(linked, ",")))
	}
	return tw.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
