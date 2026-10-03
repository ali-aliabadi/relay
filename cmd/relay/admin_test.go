package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type cli struct {
	t   *testing.T
	env func(string) (string, bool)
}

func newCLI(t *testing.T) cli {
	t.Helper()
	return cli{t: t, env: lookup(map[string]string{
		"RELAY_ENCRYPTION_KEY": testKey, "RELAY_DB_PATH": filepath.Join(t.TempDir(), "relay.db"),
	})}
}

func (c cli) run(args ...string) (int, string, string) {
	c.t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(c.t.Context(), args, c.env, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

var keyRe = regexp.MustCompile(`rk_[A-Za-z0-9_-]{43}`)

func TestClientsCLI(t *testing.T) {
	c := newCLI(t)
	code, out, errOut := c.run("clients", "create", "backup-script")
	if code != 0 || !keyRe.MatchString(out) {
		t.Fatalf("create = %d %q %q", code, out, errOut)
	}
	if strings.Contains(errOut, keyRe.FindString(out)) {
		t.Error("key written to the log stream")
	}
	if code, _, errOut := c.run("clients", "create", "backup-script"); code != 1 || !strings.Contains(errOut, "already exists") {
		t.Errorf("duplicate = %d %q", code, errOut)
	}
	if code, _, errOut := c.run("clients", "create", "Bad Name"); code != 1 || !strings.Contains(errOut, "name may only use") {
		t.Errorf("invalid = %d %q", code, errOut)
	}
	code, out, _ = c.run("clients", "list")
	if code != 0 || !strings.Contains(out, "backup-script") || !strings.Contains(out, "active") || keyRe.MatchString(out) {
		t.Errorf("list = %d %q", code, out)
	}
	if code, _, _ := c.run("clients", "revoke", "backup-script"); code != 0 {
		t.Errorf("revoke = %d", code)
	}
	if code, _, errOut := c.run("clients", "revoke", "backup-script"); code != 1 || !strings.Contains(errOut, "not found") {
		t.Errorf("second revoke = %d %q", code, errOut)
	}
	if _, out, _ := c.run("clients", "list"); !strings.Contains(out, "revoked") {
		t.Errorf("list after revoke = %q", out)
	}
	for _, args := range [][]string{{"clients"}, {"clients", "create"}, {"clients", "bogus"}, {"clients", "list", "x"}} {
		if code, _, errOut := c.run(args...); code != 2 || !strings.Contains(errOut, "Usage:") {
			t.Errorf("%v = %d %q", args, code, errOut)
		}
	}
}

func TestRecipientsCLI(t *testing.T) {
	c := newCLI(t)
	if code, out, errOut := c.run("recipients", "add", "ali", "--name", "Ali", "--timezone", "Europe/Berlin"); code != 0 ||
		!strings.Contains(out, "Added recipient ali") {
		t.Fatalf("add = %d %q %q", code, out, errOut)
	}
	if code, _, errOut := c.run("recipients", "add", "sara", "--timezone", "Nowhere/Land", "--name", "Sara"); code != 1 ||
		!strings.Contains(errOut, "unknown timezone") {
		t.Errorf("bad tz = %d %q", code, errOut)
	}
	code, out, _ := c.run("recipients", "list")
	if code != 0 || !strings.Contains(out, "ali") || !strings.Contains(out, "Europe/Berlin") || !strings.Contains(out, "telegram") {
		t.Errorf("list = %d %q", code, out)
	}
	if code, _, _ := c.run("recipients", "remove", "ali"); code != 0 {
		t.Errorf("remove = %d", code)
	}
	if code, _, _ := c.run("recipients", "remove", "ali"); code != 1 {
		t.Errorf("second remove = %d", code)
	}
	for _, args := range [][]string{
		{"recipients"},
		{"recipients", "add"},
		{"recipients", "add", "--name", "x"},
		{"recipients", "add", "ali", "--bogus"},
		{"recipients", "add", "ali", "extra"},
		{"recipients", "list", "x"},
		{"recipients", "remove"},
		{"recipients", "nope"},
	} {
		if code, _, errOut := c.run(args...); code != 2 || !strings.Contains(errOut, "Usage:") {
			t.Errorf("%v = %d %q", args, code, errOut)
		}
	}
	if code, _, _ := (cli{t: t, env: lookup(nil)}).run("recipients", "list"); code != 1 {
		t.Errorf("without config = %d, want 1", code)
	}
}

// TestKeyFromCLIAuthenticatesAgainstServer is the Phase 3 integration path:
// a key printed by `relay clients create` opens /v1 on a running server.
func TestKeyFromCLIAuthenticatesAgainstServer(t *testing.T) {
	c := newCLI(t)
	_, out, _ := c.run("clients", "create", "app")
	key := keyRe.FindString(out)

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, c.env, &syncBuffer{}, ln) }()
	defer func() { cancel(); <-done }()

	status := func(auth string) int {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+"/v1/nothing-yet", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	deadline := time.Now().Add(5 * time.Second)
	for status("") != http.StatusUnauthorized && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := status("Bearer " + key); got != http.StatusNotFound {
		t.Errorf("with CLI key: %d, want 404 (authenticated, no such route)", got)
	}
	if code, _, _ := c.run("clients", "revoke", "app"); code != 0 {
		t.Fatal("revoke failed")
	}
	if got := status("Bearer " + key); got != http.StatusUnauthorized {
		t.Errorf("after revoke: %d, want 401", got)
	}
}
