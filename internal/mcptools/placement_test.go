package mcptools

import (
	"encoding/json"
	"net/http"
	"testing"
)

// goship_create_app follows the organization's default placement, and a
// shared one still needs a plan picked.
func TestCreateAppFollowsTheDefaultPlacement(t *testing.T) {
	for _, tc := range []struct {
		name, placement, wantNode, wantPlan string
		args                                map[string]any
	}{
		{"default node", `{"mode":"auto","target":{"node_id":"n-prod","node_name":"prod"}}`, "n-prod", "", map[string]any{"name": "web", "platform": "go"}},
		{"shared default", `{"mode":"shared","target":null}`, "", "app-free", map[string]any{"name": "web", "platform": "go"}},
		{"node goship", `{"mode":"auto","target":{"node_id":"n-prod","node_name":"prod"}}`, "shared", "app-free", map[string]any{"name": "web", "platform": "go", "node": "goship"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var created map[string]any
			url := api(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/orgs/acme/placement":
					w.Write([]byte(tc.placement)) //nolint:errcheck
				case "/api/v1/orgs/acme/nodes":
					w.Write([]byte(`[]`)) //nolint:errcheck
				case "/api/v1/orgs/acme/available-plans":
					w.Write([]byte(`[{"slug":"app-free","kind":"app","display_name":"Free","is_active":true,"billed":false}]`)) //nolint:errcheck
				case "/api/v1/orgs/acme/apps":
					json.NewDecoder(r.Body).Decode(&created) //nolint:errcheck
					w.Write([]byte(`{"name":"web"}`))        //nolint:errcheck
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			cs := session(t, fakeClients{url: url, token: "t", org: "acme"}, Options{})

			if res := call(t, cs, "goship_create_app", tc.args); res.IsError {
				t.Fatalf("goship_create_app: %s", text(res))
			}
			if got, _ := created["node_id"].(string); got != tc.wantNode {
				t.Errorf("node_id = %q, want %q", got, tc.wantNode)
			}
			if got, _ := created["plan"].(string); got != tc.wantPlan {
				t.Errorf("plan = %q, want %q", got, tc.wantPlan)
			}
		})
	}
}
