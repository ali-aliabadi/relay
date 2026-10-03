package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var testKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))

func lookup(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestRunCommands(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"no args", nil, 2, "", "Usage: relay"},
		{"unknown", []string{"bogus"}, 2, "", `unknown command "bogus"`},
		{"help", []string{"help"}, 0, "Usage: relay", ""},
		{"version", []string{"version"}, 0, "dev", ""},
		{"serve without key", []string{"serve"}, 1, "", "RELAY_ENCRYPTION_KEY is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(t.Context(), tt.args, lookup(nil), strings.NewReader(""), &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantOut) || !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("stdout %q stderr %q", stdout.String(), stderr.String())
			}
		})
	}
}

// syncBuffer lets the test read logs while the server goroutine writes them.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestServeHealthzAndGracefulShutdown(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	logs := &syncBuffer{}
	dbPath := filepath.Join(t.TempDir(), "relay.db")
	env := lookup(map[string]string{
		"RELAY_ENCRYPTION_KEY": testKey, "RELAY_TELEGRAM_BOT_TOKEN": "fake:token", "RELAY_DB_PATH": dbPath,
	})
	done := make(chan error, 1)
	go func() { done <- serve(ctx, env, logs, ln) }()

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+ln.Addr().String()+"/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel() // what SIGTERM does via signal.NotifyContext
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down")
	}
	out := logs.String()
	for _, want := range []string{"migrations applied", "relay starting", `"route":"GET /healthz"`, "relay stopped"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "fake:token") || strings.Contains(out, testKey) {
		t.Errorf("secret in logs: %s", out)
	}
}

func TestServeListenError(t *testing.T) {
	env := lookup(map[string]string{
		"RELAY_ENCRYPTION_KEY": testKey, "RELAY_ADDR": "256.0.0.1:1",
		"RELAY_DB_PATH": filepath.Join(t.TempDir(), "relay.db"),
	})
	if err := serve(t.Context(), env, &bytes.Buffer{}, nil); err == nil {
		t.Fatal("want a listen error")
	}
}

func TestServeBadDBPath(t *testing.T) {
	env := lookup(map[string]string{
		"RELAY_ENCRYPTION_KEY": testKey,
		"RELAY_DB_PATH":        filepath.Join(t.TempDir(), "missing", "relay.db"),
	})
	if err := serve(t.Context(), env, &bytes.Buffer{}, nil); err == nil {
		t.Fatal("want a database error")
	}
}

func TestMigrateCommand(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "relay.db")
	env := lookup(map[string]string{"RELAY_ENCRYPTION_KEY": testKey, "RELAY_DB_PATH": dbPath})
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"migrate"}, env, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "migrations applied") {
		t.Errorf("first run should apply migrations: %s", stdout.String())
	}
	stdout.Reset()
	if code := run(t.Context(), []string{"migrate"}, env, strings.NewReader(""), &stdout, &stderr); code != 0 || stdout.Len() != 0 {
		t.Errorf("second run = %d, logged %q; want 0 and quiet", code, stdout.String())
	}
	if code := run(t.Context(), []string{"migrate"}, lookup(nil), strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Errorf("migrate without key = %d, want 1", code)
	}
}
