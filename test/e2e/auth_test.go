//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/testcontainers/testcontainers-go/exec"
)

// TestAdminCLIKeyOpensAPI creates a client with the admin CLI inside the
// running container and uses the printed key against the public API.
func TestAdminCLIKeyOpensAPI(t *testing.T) {
	ctr := startRelay(t)
	code, reader, err := ctr.Exec(t.Context(), []string{"/usr/local/bin/relay", "clients", "create", "e2e"}, exec.Multiplexed())
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(reader)
	key := regexp.MustCompile(`rk_[A-Za-z0-9_-]{43}`).FindString(string(out))
	if code != 0 || key == "" {
		t.Fatalf("clients create = %d: %s", code, out)
	}

	endpoint, err := ctr.PortEndpoint(t.Context(), "8080/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	status := func(auth string) int {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint+"/v1/nothing-yet", nil)
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
	if got := status(""); got != http.StatusUnauthorized {
		t.Errorf("without key: %d, want 401", got)
	}
	if got := status("Bearer " + key); got != http.StatusNotFound {
		t.Errorf("with key: %d, want 404 (authenticated, no such route yet)", got)
	}
}
