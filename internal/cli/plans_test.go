package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

// plansAPI answers available-plans for any environment, recording what was
// asked, with the plans in the order the portal chose.
func plansAPI(t *testing.T, plans string) *string {
	t.Helper()
	var asked string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/available-plans") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		asked = r.URL.RequestURI()
		w.Write([]byte(plans)) //nolint:errcheck
	})
	return &asked
}

const sharedPlans = `[
  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":1990,"is_active":true,"sort_order":30,"billed":true},
  {"slug":"app-micro","kind":"app","display_name":"Micro","cpu_milli":200,"memory_mb":256,"price_cents":990,"is_active":true,"sort_order":20,"billed":true}
]`

const clusterPlans = `[
  {"slug":"byon-homolog-default","kind":"app","display_name":"Sized to the cluster","cpu_milli":1800,"memory_mb":3600,"is_active":true,"default":true,"billed":false},
  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":0,"is_active":true,"billed":false}
]`

// The plans are the target environment's: its cluster decides them.
func TestPlansUsesTargetEnvironment(t *testing.T) {
	asked := plansAPI(t, clusterPlans)

	out, err := run(t, "", "plans", "--environment", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if *asked != "/api/v1/orgs/acme/projects/clave/environments/staging/available-plans?kind=app" {
		t.Errorf("asked %s", *asked)
	}
	for _, want := range []string{"byon-homolog-default", "default", "included"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "GoShip (shared)") {
		t.Errorf("one environment is one list, not sections:\n%s", out)
	}
}

// The portal orders the plans; the CLI shows them as they came.
func TestPlansKeepsTheServersOrder(t *testing.T) {
	plansAPI(t, sharedPlans)

	out, err := run(t, "", "plans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "R$ 19.90/mo") {
		t.Errorf("missing the price:\n%s", out)
	}
	if strings.Index(out, "app-small") > strings.Index(out, "app-micro") {
		t.Errorf("plans were reordered:\n%s", out)
	}
}

// --kind db is what older scripts pass; the portal calls it database.
func TestPlansMapsDbToDatabase(t *testing.T) {
	for _, kind := range []string{"db", "database"} {
		asked := plansAPI(t, `[]`)
		if _, err := run(t, "", "plans", "--kind", kind); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(*asked, "/environments/production/available-plans?kind=database") {
			t.Errorf("--kind %s asked %s", kind, *asked)
		}
	}

	asked := plansAPI(t, `[]`)
	if _, err := run(t, "", "plans", "--kind", "volume"); err == nil || !strings.Contains(err.Error(), `"app" or "database"`) {
		t.Errorf("an unknown kind should be refused, got %v", err)
	}
	if *asked != "" {
		t.Errorf("an unknown kind reached the API: %s", *asked)
	}
}

func TestPlansJSONIsThePlans(t *testing.T) {
	plansAPI(t, clusterPlans)

	out, err := run(t, "", "plans", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not a JSON list: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0]["slug"] != "byon-homolog-default" || got[0]["default"] != true {
		t.Errorf("got %v", got)
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
