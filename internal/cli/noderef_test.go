package cli

import (
	"context"

	"github.com/coffeece/goship/internal/deploy"

	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/config"
	"github.com/coffeece/goship/internal/portal"
)

const twoNodes = `[{"id":"25b640dc-5b91-49e8-ac21-1409efd47375","name":"do-server1","status":"active"},
                   {"id":"9f3c0000-0000-0000-0000-000000000001","name":"homelab","status":"active"}]`

func TestResolveNode(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(twoNodes)) //nolint:errcheck
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client := portal.New(cfg.Endpoint(), "")

	for _, tc := range []struct{ ref, want string }{
		{"do-server1", "25b640dc-5b91-49e8-ac21-1409efd47375"},
		{"9f3c0000-0000-0000-0000-000000000001", "9f3c0000-0000-0000-0000-000000000001"},
	} {
		got, err := deploy.ResolveNode(context.Background(), client, "acme", tc.ref)
		if err != nil || got != tc.want {
			t.Errorf("deploy.ResolveNode(%q) = %q, %v; want %q", tc.ref, got, err, tc.want)
		}
	}

	_, err = deploy.ResolveNode(context.Background(), client, "acme", "nope")
	if err == nil || !strings.Contains(err.Error(), "do-server1") || !strings.Contains(err.Error(), "homelab") {
		t.Errorf("an unknown ref should list what exists, got %v", err)
	}
}

// The API keys on ids; the person typed a name. The id is what must be sent.
func TestDeployNodeByNameSendsTheID(t *testing.T) {
	var created map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(twoNodes)) //nolint:errcheck
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created) //nolint:errcheck
			w.Write([]byte(`{"name":"quake"}`))      //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	dir := filepath.Join(t.TempDir(), "quake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module quake\n")

	run(t, "", "deploy", dir, "--node", "do-server1") //nolint:errcheck
	if created["node_id"] != "25b640dc-5b91-49e8-ac21-1409efd47375" {
		t.Errorf("node_id = %v, want the id resolved from the name", created["node_id"])
	}
}

func TestResolvePlacementGoshipMeansSharedServers(t *testing.T) {
	nodes := twoNodes
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(nodes)) //nolint:errcheck
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client := portal.New(cfg.Endpoint(), "")
	ctx := context.Background()

	if got, err := deploy.ResolvePlacement(ctx, client, "acme", "goship"); err != nil || got != portal.GoShipPlacement {
		t.Errorf("ResolvePlacement(goship) = %q, %v; want %q", got, err, portal.GoShipPlacement)
	}
	if got, err := deploy.ResolvePlacement(ctx, client, "acme", "do-server1"); err != nil || got != "25b640dc-5b91-49e8-ac21-1409efd47375" {
		t.Errorf("ResolvePlacement(do-server1) = %q, %v; want the node's id", got, err)
	}
	if _, err := deploy.ResolveNode(ctx, client, "acme", "goship"); err == nil {
		t.Error("ResolveNode(goship) must not name GoShip's servers: node commands need a real node")
	}
	nodes = `[]`
	if got, err := deploy.ResolvePlacement(ctx, client, "acme", "goship"); err != nil || got != portal.GoShipPlacement {
		t.Errorf("ResolvePlacement(goship) without nodes = %q, %v; want %q", got, err, portal.GoShipPlacement)
	}
	nodes = `[{"id":"n-goship","name":"goship","status":"active"}]`
	if got, _ := deploy.ResolvePlacement(ctx, client, "acme", "goship"); got != "n-goship" {
		t.Errorf("ResolvePlacement(goship) = %q, want the node named goship", got)
	}
}
