package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

func plansAPI(t *testing.T) {
	t.Helper()
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(`[{"id":"n1","name":"do-server1","status":"active"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/acme/available-plans" && r.URL.Query().Get("node") == "n1":
			w.Write([]byte(`[
			  {"slug":"byon-do-server1-default","kind":"app","display_name":"Sized to the node","cpu_milli":1800,"memory_mb":3600,"is_active":true,"default":true,"billed":false},
			  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":0,"is_active":true,"billed":false}
			]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/acme/available-plans":
			w.Write([]byte(`[
			  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":1990,"is_active":true,"sort_order":30,"billed":true},
			  {"slug":"app-micro","kind":"app","display_name":"Micro","cpu_milli":200,"memory_mb":256,"price_cents":990,"is_active":true,"sort_order":20,"billed":true}
			]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// One listing, one section per placement: the shared catalogue with prices,
// then each node with sizes and no prices, its pool's default marked.
func TestPlansListsEveryPlacement(t *testing.T) {
	plansAPI(t)

	out, err := run(t, "", "plans")
	if err != nil {
		t.Fatal(err)
	}
	shared, node := strings.Index(out, "GoShip (shared)"), strings.Index(out, "do-server1")
	if shared < 0 || node < 0 || shared > node {
		t.Fatalf("expected the shared section before the node section:\n%s", out)
	}
	for _, want := range []string{"R$ 19.90/mo", "not billed", "byon-do-server1-default", "default", "included"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// The shared section is sorted by the catalogue's order, not the API's.
	if strings.Index(out, "app-micro") > strings.Index(out, "app-small") {
		t.Errorf("shared plans should follow sort_order:\n%s", out)
	}
}

func TestPlansForOneNode(t *testing.T) {
	plansAPI(t)

	out, err := run(t, "", "plans", "--node", "n1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "GoShip (shared)") || strings.Contains(out, "R$") {
		t.Errorf("--node should show only that node's pool:\n%s", out)
	}
	// Pool order is meaningful and must survive: the default comes first.
	if strings.Index(out, "byon-do-server1-default") > strings.Index(out, "app-small") {
		t.Errorf("pool order lost:\n%s", out)
	}
}

func TestPlansJSONKeysEachPlacement(t *testing.T) {
	plansAPI(t)

	out, err := run(t, "", "plans", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got["shared"]) != 2 || len(got["do-server1"]) != 2 {
		t.Errorf("got keys %v", got)
	}
	if got["do-server1"][0]["default"] != true {
		t.Errorf("the node default should be flagged in JSON: %v", got["do-server1"][0])
	}
}

func TestPlanPriceWording(t *testing.T) {
	for _, tc := range []struct {
		p    portal.Plan
		want string
	}{
		{portal.Plan{IsFree: true}, "free"},
		{portal.Plan{Billed: false, PriceCents: 0}, "included"},
		{portal.Plan{Billed: true, PriceCents: 1990}, "R$ 19.90/mo"},
	} {
		if got := deploy.Price(tc.p); got != tc.want {
			t.Errorf("%+v → %q, want %q", tc.p, got, tc.want)
		}
	}
}
