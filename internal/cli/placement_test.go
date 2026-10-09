package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

// deployNewApp runs `goship deploy` for a new Go app and returns what the
// create request carried and what was printed.
func deployNewApp(t *testing.T, placement string, args ...string) (created map[string]any, plansAsked bool, out string) {
	t.Helper()
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs/acme/placement":
			w.Write([]byte(placement)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(twoNodes)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			plansAsked = true
			w.Write([]byte(freePlanCatalog)) //nolint:errcheck
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created) //nolint:errcheck
			w.Write([]byte(`{"name":"widget"}`))     //nolint:errcheck
		}
	})
	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")
	out, _ = run(t, "", append([]string{"deploy", dir}, args...)...)
	if created == nil {
		t.Fatal("the app was never created")
	}
	return created, plansAsked, out
}

func TestDeployUsesTheOrgDefaultNode(t *testing.T) {
	created, plansAsked, out := deployNewApp(t, `{"mode":"auto","target":{"node_id":"n-prod","node_name":"prod"}}`)

	if created["node_id"] != "n-prod" {
		t.Errorf("node_id = %v, want the org default node", created["node_id"])
	}
	if plansAsked || created["plan"] != nil {
		t.Errorf("a node app must not pick from the paid catalogue (asked=%v, plan=%v)", plansAsked, created["plan"])
	}
	if !strings.Contains(out, "on your node prod (org default)") {
		t.Errorf("the output should say the app goes to the default node, got %q", out)
	}
}

func TestDeployOnSharedDefaultChoosesAPlan(t *testing.T) {
	created, plansAsked, _ := deployNewApp(t, `{"mode":"goship","target":null}`)

	if _, ok := created["node_id"]; ok {
		t.Errorf("node_id = %v, want none", created["node_id"])
	}
	if !plansAsked || created["plan"] != "app-free" {
		t.Errorf("a shared app needs a plan (asked=%v, plan=%v)", plansAsked, created["plan"])
	}
}

func TestDeployNodeGoshipOverridesTheDefault(t *testing.T) {
	created, plansAsked, _ := deployNewApp(t, `{"mode":"auto","target":{"node_id":"n-prod","node_name":"prod"}}`, "--node", "goship")

	if created["node_id"] != portal.GoShipPlacement {
		t.Errorf("node_id = %v, want %q", created["node_id"], portal.GoShipPlacement)
	}
	if !plansAsked {
		t.Error("GoShip's servers need a plan")
	}
}
