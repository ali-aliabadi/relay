// Command relay is Relay's single binary: the HTTP service and its admin CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata" // the distroless image has no zoneinfo; recipients have timezones
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage: relay <command>

Commands:
  serve        run the HTTP API and delivery worker
  migrate      apply database migrations and exit (serve also does this)
  clients      create, list and revoke API clients
  recipients   add, list, remove and link recipients
  send         queue a test message and wait for its delivery
  version      print the version
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.LookupEnv, os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches a subcommand and returns the process exit code.
func run(ctx context.Context, args []string, lookup func(string) (string, bool), stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		if err := serve(ctx, lookup, stdout, nil); err != nil {
			fmt.Fprintf(stderr, "relay serve: %v\n", err)
			return 1
		}
		return 0
	case "migrate":
		if err := migrate(ctx, lookup, stdout); err != nil {
			fmt.Fprintf(stderr, "relay migrate: %v\n", err)
			return 1
		}
		return 0
	case "clients":
		return admin(env{ctx, lookup, stdin, stdout, stderr}, args[1:], clientsCmd, clientsUsage)
	case "recipients":
		return admin(env{ctx, lookup, stdin, stdout, stderr}, args[1:], recipientsCmd, recipientsUsage)
	case "send":
		return admin(env{ctx, lookup, stdin, stdout, stderr}, args[1:], sendCmd, sendUsage)
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "relay: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

// admin runs an admin subcommand and maps its error to an exit code.
func admin(e env, args []string, cmd func(env, []string) error, usage string) int {
	err := cmd(e, args)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprint(e.stderr, usage)
		return 2
	default:
		fmt.Fprintf(e.stderr, "relay: %v\n", err)
		return 1
	}
}
