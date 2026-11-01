package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

func TestChoosePlanPrefersTheDefault(t *testing.T) {
	var asked, plans string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RequestURI()
		w.Write([]byte(plans)) //nolint:errcheck
	}))
	defer srv.Close()
	client := portal.New(srv.URL, "t")
	staging := Target{Org: "acme", Project: "clave", Environment: "staging"}

	cases := []struct{ name, plans, want string }{
		// A customer cluster: nothing is billed and the cluster names its default.
		{"the cluster's default", `[{"slug":"sized","is_active":true},{"slug":"app-small","is_active":true,"default":true},{"slug":"app-large","is_active":true}]`, "app-small"},
		// No default: the first unbilled plan in the server's order, whatever sort_order says.
		{"first unbilled", `[{"slug":"app-small","billed":true,"is_active":true,"sort_order":1},{"slug":"app-free","is_active":true,"sort_order":9},{"slug":"app-micro","is_active":true,"sort_order":2}]`, "app-free"},
		// A default that costs money is still the user's call.
		{"a billed default is not chosen", `[{"slug":"app-small","billed":true,"default":true,"is_active":true},{"slug":"app-free","is_active":true}]`, "app-free"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plans = tc.plans
			got, err := ChoosePlan(context.Background(), client, staging)
			if err != nil || got.Slug != tc.want {
				t.Fatalf("ChoosePlan = %q, %v; want %q", got.Slug, err, tc.want)
			}
			if asked != "/api/v1/orgs/acme/projects/clave/environments/staging/available-plans?kind=app" {
				t.Errorf("asked %s", asked)
			}
		})
	}

	plans = `[{"slug":"app-small","billed":true,"is_active":true,"cpu_milli":500,"memory_mb":512,"price_cents":1990}]`
	if _, err := ChoosePlan(context.Background(), client, staging); err == nil || !strings.Contains(err.Error(), "app-small") || !strings.Contains(err.Error(), "--plan") {
		t.Errorf("only paid plans: err = %v, want the plans and how to pick one", err)
	}
}
