package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const twoClusters = `[{"id":"c-goship","name":"goship","type":"kubernetes","status":"active","shared":true,"namespaced":true},
                      {"id":"n1","name":"homolog","type":"vps","status":"active","host":"203.0.113.5"}]`

// An organization runs on its own clusters and on GoShip's shared one; the
// listing shows both and says which is which.
func TestClustersListsSharedAndOwn(t *testing.T) {
	var gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(twoClusters)) //nolint:errcheck
	})
	out, err := run(t, "", "clusters")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/clusters" {
		t.Errorf("path = %q", gotPath)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "ID NAME TYPE STATUS HOST SHARED" {
		t.Fatalf("output = %q", out)
	}
	for _, tc := range []struct{ name, shared string }{{"goship", "true"}, {"homolog", "false"}} {
		found := false
		for _, l := range lines[1:] {
			f := strings.Fields(l)
			if len(f) == 6 && f[1] == tc.name {
				found = true
				if f[5] != tc.shared {
					t.Errorf("%s: SHARED = %q, want %q", tc.name, f[5], tc.shared)
				}
			}
		}
		if !found {
			t.Errorf("missing %s:\n%s", tc.name, out)
		}
	}
}

// Every way of spelling a node command says where it went and exits 2,
// without running anything.
func TestNodeCommandPointsToCluster(t *testing.T) {
	for _, args := range [][]string{
		{"node"},
		{"nodes"},
		{"node", "create", "n1", "--cloud", "acme-do", "--region", "nyc3"},
		{"node", "rm", "n1", "--destroy-server", "--yes"},
		{"--org", "acme", "nodes", "--all"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			})
			out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), args...)
			var code ExitCode
			if !errors.As(err, &code) || code != 2 {
				t.Fatalf("err = %v, want exit code 2", err)
			}
			if strings.TrimSpace(stderr) != `node commands are now "goship cluster …"` {
				t.Errorf("stderr = %q", stderr)
			}
			if out != "" {
				t.Errorf("stdout = %q, want nothing", out)
			}
		})
	}
}

func TestClusterInfoResolvesTheName(t *testing.T) {
	var calls []string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs/acme/clusters":
			w.Write([]byte(twoClusters)) //nolint:errcheck
		case "/api/v1/orgs/acme/clusters/n1":
			w.Write([]byte(`{"id":"n1","name":"homolog","type":"vps","status":"resizing","stage":"stopping","resize_message":"Back in a few minutes."}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := run(t, "", "cluster", "info", "homolog")
	if err != nil {
		t.Fatal(err)
	}
	if calls[len(calls)-1] != "/api/v1/orgs/acme/clusters/n1" {
		t.Errorf("calls = %q", calls)
	}
	for _, want := range []string{"homolog", "Resize stage: stopping", "Back in a few minutes."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// Org-level: a cluster needs no project, even when the org has several and
// none is selected.
func TestClusterCommandsNeedNoProject(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/clusters":
			w.Write([]byte(twoClusters)) //nolint:errcheck
		case "/api/v1/orgs/acme/clusters/n1":
			w.Write([]byte(`{"id":"n1","name":"homolog"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	t.Setenv("GOSHIP_PROJECT", "")
	t.Setenv("GOSHIP_ENVIRONMENT", "")
	for _, args := range [][]string{{"clusters"}, {"cluster", "info", "n1"}} {
		if _, err := run(t, "", args...); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestClusterRmDestroySendsFlag(t *testing.T) {
	var body map[string]any
	var deleted string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(twoClusters)) //nolint:errcheck
			return
		}
		deleted = r.Method + " " + r.URL.Path
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	})

	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader("y\n"), "cluster", "rm", "homolog", "--destroy-server")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != "DELETE /api/v1/orgs/acme/clusters/n1" || body["destroy_server"] != true {
		t.Errorf("request = %s %v", deleted, body)
	}
	if !strings.Contains(stderr, `Disconnect cluster "homolog" and destroy its machine at the cloud provider?`) {
		t.Errorf("confirmation should name the destroy: %q", stderr)
	}
	if !strings.Contains(out, "Cluster homolog is being removed; its machine will be destroyed — `goship clusters` shows when it is gone.") {
		t.Errorf("result should say the destroy is underway, not done: %q", out)
	}
}

func TestClusterRmDestroySurfacesAPIMessage(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(twoClusters)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"destroying the machine needs the digitalocean account connected"}`)) //nolint:errcheck
	})

	_, err := run(t, "y\n", "cluster", "rm", "n1", "--destroy-server")
	if err == nil || err.Error() != "destroying the machine needs the digitalocean account connected" {
		t.Errorf("err = %v", err)
	}
}

func TestClusterResizeYesSkipsPrompt(t *testing.T) {
	var body map[string]any
	var path string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(twoClusters)) //nolint:errcheck
			return
		}
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"id":"n1","name":"homolog","status":"resizing"}`)) //nolint:errcheck
	})

	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), "--yes", "cluster", "resize", "homolog", "--size", "e2-standard-4")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Errorf("--yes should not prompt: %q", stderr)
	}
	if path != "/api/v1/orgs/acme/clusters/n1/resize" || body["size"] != "e2-standard-4" {
		t.Errorf("request = %s %v", path, body)
	}
	if !strings.Contains(out, "Cluster homolog is resizing to e2-standard-4. Follow it with `goship cluster info homolog`.") {
		t.Errorf("output = %q", out)
	}
}

func TestClusterResizeShowsReconnectMessage(t *testing.T) {
	const msg = "Reconnect your Google Cloud account to let GoShip resize machines."
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(twoClusters)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"` + msg + `","code":"reconnect_required"}`)) //nolint:errcheck
	})
	_, err := run(t, "", "--yes", "cluster", "resize", "homolog", "--size", "x")
	want := strings.TrimSuffix(msg, ".") + ". Reconnect it from the dashboard: "
	if err == nil || !strings.HasPrefix(err.Error(), want) || !strings.HasSuffix(err.Error(), "/orgs/acme/clusters/n1/resize") {
		t.Errorf("error = %v, want prefix %q and the cluster's resize page", err, want)
	}
}

func TestClusterSizesMarksCurrent(t *testing.T) {
	var sizesPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sizes") {
			sizesPath = r.URL.Path
			w.Write([]byte(`[{"slug":"e2-small","vcpu":2,"memory_mb":2048,"disk_gb":20,"current":true},{"slug":"e2-standard-4","vcpu":4,"memory_mb":16384,"disk_gb":20}]`)) //nolint:errcheck
			return
		}
		w.Write([]byte(twoClusters)) //nolint:errcheck
	})
	out, err := run(t, "", "cluster", "sizes", "homolog")
	if err != nil {
		t.Fatal(err)
	}
	if sizesPath != "/api/v1/orgs/acme/clusters/n1/sizes" {
		t.Errorf("path = %q", sizesPath)
	}
	if !strings.Contains(out, "CURRENT") || !strings.Contains(out, "e2-standard-4") {
		t.Errorf("output = %q", out)
	}
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, "e2-small") && !strings.Contains(l, "current") {
			t.Errorf("current size is not marked: %q", l)
		}
	}
}
