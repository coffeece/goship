package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestNodeCreateCloudSendsAccountRegionSize(t *testing.T) {
	var body map[string]any
	var gotMethod, gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"acme-do"}]`)) //nolint:errcheck
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/nodes":
			gotMethod, gotPath = r.Method, r.URL.Path
			json.NewDecoder(r.Body).Decode(&body)                              //nolint:errcheck
			w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	if _, err := run(t, "", "node", "create", "n1",
		"--cloud", "acme-do", "--region", "nyc3", "--size", "s-4vcpu-8gb", "--no-wait"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/orgs/acme/nodes" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if body["name"] != "n1" || body["cloud_account_id"] != "acc-1" || body["region"] != "nyc3" || body["size"] != "s-4vcpu-8gb" {
		t.Errorf("body = %v", body)
	}
}

func TestNodeCreateSSHSendsHostAndKey(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyFile, []byte("fake-key-material"), 0o600); err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		json.NewDecoder(r.Body).Decode(&body)                              //nolint:errcheck
		w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning"}`)) //nolint:errcheck
	})

	if _, err := run(t, "", "node", "create", "n1", "--host", "203.0.113.5", "--ssh-key", keyFile, "--no-wait"); err != nil {
		t.Fatal(err)
	}
	if body["type"] != "vps" || body["host"] != "203.0.113.5" || body["ssh_key"] != "fake-key-material" {
		t.Errorf("body = %v", body)
	}
}

func TestNodeCreateRejectsBothCloudAndHost(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})

	_, err := run(t, "", "node", "create", "n1", "--cloud", "acme-do", "--host", "203.0.113.5")
	if err == nil || !strings.Contains(err.Error(), "--cloud") || !strings.Contains(err.Error(), "--host") {
		t.Fatalf("got %v", err)
	}
}

func TestNodeCreateMissingSizeWithoutTTYErrors(t *testing.T) {
	orig := stdinIsTTY
	stdinIsTTY = func(*cobra.Command) bool { return false }
	t.Cleanup(func() { stdinIsTTY = orig })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"acme-do"}]`)) //nolint:errcheck
	})

	_, err := run(t, "", "node", "create", "n1", "--cloud", "acme-do", "--region", "nyc3")
	if err == nil || !strings.Contains(err.Error(), "--size") {
		t.Fatalf("got %v", err)
	}
}

// runWithStderr is run with a context and a stdin of the test's choosing,
// handing back stderr too — where prompts and follow notes go.
func runWithStderr(t *testing.T, ctx context.Context, in io.Reader, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut strings.Builder
	root := NewRoot("test")
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(in)

	err = root.ExecuteContext(ctx)
	return out.String(), errOut.String(), err
}

func TestNodeCreatePromptsForRegionAndSizeOnTTY(t *testing.T) {
	orig := stdinIsTTY
	stdinIsTTY = func(*cobra.Command) bool { return true }
	t.Cleanup(func() { stdinIsTTY = orig })

	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"acme-do"}]`)) //nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts/acc-1/regions":
			w.Write([]byte(`[{"slug":"nyc1","label":"New York 1","country":"US"},{"slug":"nyc3","label":"New York 3","country":"US"}]`)) //nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts/acc-1/sizes":
			w.Write([]byte(`[{"slug":"s-1vcpu-1gb","vcpu":1,"memory_mb":1024,"disk_gb":25,"price_monthly":48,"currency":"USD","band":{"slug":"b1","price_cents":9900,"currency":"BRL"}},` +
				`{"slug":"s-2vcpu-4gb","vcpu":2,"memory_mb":4096,"disk_gb":80}]`)) //nolint:errcheck
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/nodes":
			json.NewDecoder(r.Body).Decode(&body)                              //nolint:errcheck
			w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	_, stderr, err := runWithStderr(t, context.Background(), strings.NewReader("2\n1\n"), "node", "create", "n1", "--cloud", "acme-do", "--no-wait")
	if err != nil {
		t.Fatal(err)
	}
	if body["region"] != "nyc3" {
		t.Errorf("region = %v, want the second in the list (nyc3)", body["region"])
	}
	if body["size"] != "s-1vcpu-1gb" {
		t.Errorf("size = %v, want the first in the list (s-1vcpu-1gb)", body["size"])
	}
	if !strings.Contains(stderr, "s-1vcpu-1gb — 1 vCPU, 1 GB RAM, 25 GB disk, US$ 48/mo (GoShip R$ 99,00/mo)") {
		t.Errorf("priced size line missing from the prompt:\n%s", stderr)
	}
	if !strings.Contains(stderr, "s-2vcpu-4gb — 2 vCPU, 4 GB RAM, 80 GB disk\n") {
		t.Errorf("unpriced size should end at its disk:\n%s", stderr)
	}
	if strings.Contains(stderr, "/mo/mo") || strings.Contains(stderr, "-/mo") {
		t.Errorf("prompt doubles or dangles the /mo suffix:\n%s", stderr)
	}
}

// Regression: stdin used to be wrapped in a bufio.Reader before the TTY check,
// which then never saw an *os.File and refused to prompt on a real terminal.
func TestNodeCreateChecksTTYBeforeWrappingStdin(t *testing.T) {
	in := strings.NewReader("1\n1\n")
	orig := stdinIsTTY
	calls := 0
	stdinIsTTY = func(cmd *cobra.Command) bool {
		calls++
		if got := cmd.InOrStdin(); got != io.Reader(in) {
			t.Errorf("stdinIsTTY saw %T, want the reader the command was given", got)
		}
		return true
	}
	t.Cleanup(func() { stdinIsTTY = orig })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"acme-do"}]`)) //nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts/acc-1/regions":
			w.Write([]byte(`[{"slug":"nyc3","label":"New York 3","country":"US"}]`)) //nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts/acc-1/sizes":
			w.Write([]byte(`[{"slug":"s-1vcpu-1gb","vcpu":1,"memory_mb":1024,"disk_gb":25}]`)) //nolint:errcheck
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	if _, _, err := runWithStderr(t, context.Background(), in, "node", "create", "n1", "--cloud", "acme-do", "--no-wait"); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Error("stdinIsTTY was never asked")
	}
}

func TestNodeCreateFollowsUntilActive(t *testing.T) {
	origInterval := nodePollInterval
	nodePollInterval = time.Millisecond
	t.Cleanup(func() { nodePollInterval = origInterval })

	polls := 0
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"acme-do"}]`)) //nolint:errcheck
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning","stage":"creating_machine"}`)) //nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/nodes/n1":
			polls++
			if polls == 1 {
				w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning","stage":"installing_k3s"}`)) //nolint:errcheck
				return
			}
			w.Write([]byte(`{"id":"n1","name":"n1","status":"active","stage":"","pool_name":"pool-n1"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	out, err := run(t, "", "node", "create", "n1", "--cloud", "acme-do", "--region", "nyc3", "--size", "s-1vcpu-1gb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Install k3s") {
		t.Errorf("output should show the stage title: %q", out)
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("output should show a completed step: %q", out)
	}
	if !strings.Contains(out, "ready") {
		t.Errorf("output should say the node is ready: %q", out)
	}
}

func TestNodeCreateFollowStopsOnError(t *testing.T) {
	origInterval := nodePollInterval
	nodePollInterval = time.Millisecond
	t.Cleanup(func() { nodePollInterval = origInterval })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"acme-do"}]`)) //nolint:errcheck
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning","stage":"waiting_ssh"}`)) //nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/nodes/n1":
			w.Write([]byte(`{"id":"n1","name":"n1","status":"error","stage":"waiting_ssh","error_message":"ssh never answered"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	out, err := run(t, "", "node", "create", "n1", "--cloud", "acme-do", "--region", "nyc3", "--size", "s-1vcpu-1gb")
	if err == nil || !strings.Contains(err.Error(), "ssh never answered") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(out, "Wait for SSH") {
		t.Errorf("output should show the stage title: %q", out)
	}
	if !strings.Contains(out, "ssh never answered") {
		t.Errorf("output should show the error message: %q", out)
	}
}

func TestNodeCreateJSONSkipsFollow(t *testing.T) {
	gets := 0
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"acme-do"}]`)) //nolint:errcheck
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(`{"id":"n1","name":"n1","status":"provisioning"}`)) //nolint:errcheck
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/nodes/n1":
			gets++
			w.Write([]byte(`{"id":"n1","name":"n1","status":"active"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	out, err := run(t, "", "node", "create", "n1",
		"--cloud", "acme-do", "--region", "nyc3", "--size", "s-1vcpu-1gb", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if gets != 0 {
		t.Errorf("json output should not poll the node, got %d GETs", gets)
	}
	if !strings.Contains(out, `"id"`) {
		t.Errorf("output should be the created node's JSON: %q", out)
	}
}
