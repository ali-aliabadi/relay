//go:build e2e

// Package e2e drives the real Docker image through its public API.
// Run with `make test-e2e`, which builds the image first.
package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startRelay runs the image under test with a throwaway key and a read-only
// root filesystem. opts add to (and may override) those defaults.
func startRelay(t *testing.T, opts ...testcontainers.ContainerCustomizer) testcontainers.Container {
	t.Helper()
	image := os.Getenv("RELAY_E2E_IMAGE")
	if image == "" {
		t.Fatal("RELAY_E2E_IMAGE is not set; run `make test-e2e`")
	}
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	base := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithEnv(map[string]string{"RELAY_ENCRYPTION_KEY": key}),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.ReadonlyRootfs = true
		}),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/healthz").WithPort("8080/tcp").
			WithStartupTimeout(30 * time.Second)),
	}
	ctr, err := testcontainers.Run(t.Context(), image, append(base, opts...)...)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("starting relay: %v", err)
	}
	return ctr
}

func TestHealthzAndGracefulStop(t *testing.T) {
	ctr := startRelay(t)
	endpoint, err := ctr.PortEndpoint(t.Context(), "8080/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+"/healthz", nil)
	req.Header.Set("X-Request-ID", "e2e-check-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("healthz = %d %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Request-ID"); got != "e2e-check-1" {
		t.Errorf("X-Request-ID = %q", got)
	}

	timeout := 10 * time.Second
	if err := ctr.Stop(context.WithoutCancel(t.Context()), &timeout); err != nil {
		t.Fatalf("stopping: %v", err)
	}
	state, err := ctr.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.ExitCode != 0 {
		t.Errorf("exit code after SIGTERM = %d, want 0 (graceful shutdown)", state.ExitCode)
	}

	logs, err := ctr.Logs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(logs)
	for _, want := range []string{"relay starting", `"route":"GET /healthz"`, `"request_id":"e2e-check-1"`, "relay stopped"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("container logs missing %q:\n%s", want, out)
		}
	}
}
