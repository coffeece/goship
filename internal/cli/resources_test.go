package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDatabaseCommandsUseTheServiceInstanceEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		args               []string
	}{
		{"create", http.MethodPost, "/api/v1/orgs/acme/services/postgresql/instances",
			[]string{"db", "create", "main", "--plan", "starter"}},
		{"delete", http.MethodDelete, "/api/v1/orgs/acme/services/postgresql/instances/main",
			[]string{"db", "rm", "main", "--yes"}},
		{"bind", http.MethodPost, "/api/v1/orgs/acme/services/postgresql/instances/main/bind",
			[]string{"db", "bind", "main", "-a", "api"}},
		{"unbind", http.MethodDelete, "/api/v1/orgs/acme/services/postgresql/instances/main/bind/api",
			[]string{"db", "unbind", "main", "-a", "api"}},
		{"list", http.MethodGet, "/api/v1/orgs/acme/databases",
			[]string{"dbs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotMethod string
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				w.Write([]byte(`[]`)) //nolint:errcheck
			})

			if _, err := run(t, "", tc.args...); err != nil {
				t.Fatal(err)
			}
			if gotPath != tc.path {
				t.Errorf("path = %q, want %q", gotPath, tc.path)
			}
			if gotMethod != tc.method {
				t.Errorf("method = %q, want %q", gotMethod, tc.method)
			}
		})
	}
}

func TestVolumeBindSendsTheMountPoint(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	})

	if _, err := run(t, "", "volume", "bind", "uploads", "-a", "api", "--mount", "/data", "--read-only"); err != nil {
		t.Fatal(err)
	}
	if body["app"] != "api" || body["mount_point"] != "/data" || body["read_only"] != true {
		t.Errorf("body = %v", body)
	}
}

// Unbind is a POST, not a DELETE — the portal restarts the app as part of it.
func TestVolumeUnbindIsAPost(t *testing.T) {
	var gotMethod, gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
	})

	if _, err := run(t, "", "volume", "unbind", "uploads", "-a", "api", "--mount", "/data"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/orgs/acme/volumes/uploads/unbind" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
}

func TestDomainAddTargetsTheApp(t *testing.T) {
	var gotPath string
	var body map[string]string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	})

	if _, err := run(t, "", "domain", "add", "shop.example.com", "-a", "api"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/apps/api/domains" {
		t.Errorf("path = %q", gotPath)
	}
	if body["domain"] != "shop.example.com" {
		t.Errorf("body = %v", body)
	}
}

func TestNodeRmDestroySendsFlag(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`[{"id":"n1","name":"n1"}]`)) //nolint:errcheck
			return
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	})

	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader("y\n"), "node", "rm", "n1", "--destroy-server")
	if err != nil {
		t.Fatal(err)
	}
	if body["destroy_server"] != true {
		t.Errorf("destroy_server = %v, want true", body["destroy_server"])
	}
	if !strings.Contains(stderr, `Disconnect node "n1" and destroy its machine at the cloud provider?`) {
		t.Errorf("confirmation should name the destroy: %q", stderr)
	}
	if !strings.Contains(out, "Node n1 is being removed; its machine will be destroyed — `goship nodes` shows when it is gone.") {
		t.Errorf("result should say the destroy is underway, not done: %q", out)
	}
}

func TestNodeRmDestroySurfacesAPIMessage(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`[{"id":"n1","name":"n1"}]`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"destroying the machine needs the digitalocean account connected"}`)) //nolint:errcheck
	})

	_, err := run(t, "y\n", "node", "rm", "n1", "--destroy-server")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "destroying the machine needs the digitalocean account connected") {
		t.Errorf("error message = %q, want to contain %q", err.Error(), "destroying the machine needs the digitalocean account connected")
	}
}

func TestDestructiveCommandsAllConfirm(t *testing.T) {
	for _, args := range [][]string{
		{"app", "rm", "api"},
		{"db", "rm", "main"},
		{"volume", "rm", "uploads"},
		{"node", "rm", "n1"},
		{"cloud", "disconnect", "acme-do"},
	} {
		t.Run(args[0], func(t *testing.T) {
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				// Resolving a node's name is a read; it may happen before the
				// question. Anything else must not.
				if r.Method == http.MethodGet {
					w.Write([]byte(`[{"id":"n1","name":"n1"}]`)) //nolint:errcheck
					return
				}
				t.Errorf("%v reached the API without confirmation", args)
			})
			if _, err := run(t, "n\n", args...); err == nil {
				t.Errorf("%v should abort when the answer is no", args)
			}
		})
	}
}
