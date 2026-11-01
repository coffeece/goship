package mcptools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	billingLater   = "2099-01-02T12:00:00Z"
	billingEarlier = "2020-01-02T12:00:00Z"
	laterLine      = "Billing starts when the trial ends on 2 Jan 2099."
	earlierLine    = "Billing starts shortly."

	appsPath      = "/api/v1/orgs/acme/projects/clave/environments/production/apps"
	instancesPath = "/api/v1/orgs/acme/projects/clave/environments/production/services/postgresql/instances"
)

// billingSession serves reply to the one request a tool makes, and fails the
// test when it is not the exact method and path expected.
func billingSession(t *testing.T, method, path, reply string) *mcp.ClientSession {
	t.Helper()
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) //nolint:errcheck
		if r.Method != method || r.URL.Path != path {
			t.Errorf("got %s %s, want %s %s", r.Method, r.URL.Path, method, path)
		}
		w.Write([]byte(reply)) //nolint:errcheck
	})
	return session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{Deploy: true})
}

// result is the JSON a tool returned.
func result(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatal(text(res))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(text(res)), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text(res))
	}
	return got
}

// A created app is returned as the portal sent it, so billing_starts_at is
// there while a trial holds it and absent otherwise.
func TestCreateAppReturnsBillingStartsAt(t *testing.T) {
	cs := billingSession(t, http.MethodPost, appsPath, `{"name":"api","billing_starts_at":"`+billingLater+`"}`)
	got := result(t, call(t, cs, "goship_create_app", map[string]any{"name": "api", "platform": "go", "plan": "app-free"}))
	if got["billing_starts_at"] != billingLater {
		t.Errorf("result = %v", got)
	}

	cs = billingSession(t, http.MethodPost, appsPath, `{"name":"api"}`)
	got = result(t, call(t, cs, "goship_create_app", map[string]any{"name": "api", "platform": "go", "plan": "app-free"}))
	if _, ok := got["billing_starts_at"]; ok {
		t.Errorf("result = %v, want no billing_starts_at", got)
	}
}

func TestSetAppPlanReturnsBillingStartsAt(t *testing.T) {
	for _, tc := range []struct {
		name, reply, message string
		starts               bool
	}{
		{"held", `{"status":"updated","billing_starts_at":"` + billingLater + `"}`, "App api moved to plan app-small. " + laterLine, true},
		{"trial already over", `{"status":"updated","billing_starts_at":"` + billingEarlier + `"}`, "App api moved to plan app-small. " + earlierLine, true},
		{"not held", `{"status":"updated"}`, "App api moved to plan app-small.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := billingSession(t, http.MethodPut, appsPath+"/api/plan", tc.reply)
			got := result(t, call(t, cs, "goship_set_app_plan", map[string]any{"app": "api", "plan": "app-small"}))
			if got["message"] != tc.message {
				t.Errorf("message = %q, want %q", got["message"], tc.message)
			}
			if _, ok := got["billing_starts_at"]; ok != tc.starts {
				t.Errorf("result = %v, want billing_starts_at: %v", got, tc.starts)
			}
		})
	}
}

func TestCreateDatabaseReturnsBillingStartsAt(t *testing.T) {
	args := map[string]any{"name": "shop", "plan": "db-micro"}

	cs := billingSession(t, http.MethodPost, instancesPath, `{"status":"created","billing_starts_at":"`+billingLater+`"}`)
	got := result(t, call(t, cs, "goship_create_database", args))
	if got["billing_starts_at"] != billingLater || got["message"] != "Database shop is being created. "+laterLine {
		t.Errorf("result = %v", got)
	}

	cs = billingSession(t, http.MethodPost, instancesPath, `{"status":"created"}`)
	got = result(t, call(t, cs, "goship_create_database", args))
	if _, ok := got["billing_starts_at"]; ok || got["message"] != "Database shop is being created." {
		t.Errorf("result = %v", got)
	}
}

// goship_deploy carries the date of the app after the release.
func TestDeployReturnsBillingStartsAt(t *testing.T) {
	saved := deploy.Build
	deploy.Build = func(context.Context, *portal.Client, deploy.BuildArgs, deploy.Reporter) error { return nil }
	t.Cleanup(func() { deploy.Build = saved })

	for _, tc := range []struct {
		name, held string
		starts     bool
	}{
		{"held", `,"billing_starts_at":"` + billingLater + `"`, true},
		{"not held", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := api(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != appsPath+"/web" {
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
				w.Write([]byte(`{"name":"web","platform":"go","addresses":["https://web.apps.goship.sh"]` + tc.held + `}`)) //nolint:errcheck
			})
			cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{Deploy: true})
			dir := t.TempDir()
			writeFile(t, dir+"/go.mod", "module web\n")
			got := result(t, call(t, cs, "goship_deploy", map[string]any{"path": dir, "app": "web", "environment": "production"}))
			if _, ok := got["billing_starts_at"]; ok != tc.starts || (tc.starts && got["billing_starts_at"] != billingLater) {
				t.Errorf("result = %v, want billing_starts_at: %v", got, tc.starts)
			}
			if got["url"] != "https://web.apps.goship.sh" {
				t.Errorf("url = %v", got["url"])
			}
		})
	}
}
