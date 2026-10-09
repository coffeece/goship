package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeClients is a Clients over an httptest API: one token, one org.
type fakeClients struct {
	url   string
	token string
	org   string
}

func (f fakeClients) Client(context.Context) (*portal.Client, error) {
	if f.token == "" {
		return nil, errors.New(`not logged in: run "goship login" in a terminal, or set GOSHIP_TOKEN`)
	}
	return portal.New(f.url, f.token), nil
}

func (f fakeClients) Org(_ context.Context, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if f.org == "" {
		return "", errors.New(`no organization selected: pass org, or run "goship org use <slug>"; goship_list_orgs shows them`)
	}
	return f.org, nil
}

// session connects a client to a server over in-memory transports.
func session(t *testing.T, c Clients, o Options) *mcp.ClientSession {
	t.Helper()
	st, ct := mcp.NewInMemoryTransports()
	srv := New(c, "test", o)
	ss, err := srv.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() }) //nolint:errcheck
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() }) //nolint:errcheck
	return cs
}

func api(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestEveryToolIsPrefixedAndAnnotated(t *testing.T) {
	cs := session(t, fakeClients{}, Options{Deploy: true})
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) < 40 {
		t.Fatalf("only %d tools registered", len(tools.Tools))
	}
	for _, tl := range tools.Tools {
		if !strings.HasPrefix(tl.Name, "goship_") {
			t.Errorf("%s lacks the goship_ prefix", tl.Name)
		}
		if tl.Annotations == nil {
			t.Errorf("%s has no annotations", tl.Name)
			continue
		}
		readish := strings.HasPrefix(tl.Name, "goship_list_") || strings.HasPrefix(tl.Name, "goship_get_") ||
			tl.Name == "goship_whoami" || tl.Name == "goship_logs" || tl.Name == "goship_database_stats"
		if readish != tl.Annotations.ReadOnlyHint {
			t.Errorf("%s: ReadOnlyHint = %v", tl.Name, tl.Annotations.ReadOnlyHint)
		}
		destructiveish := strings.HasPrefix(tl.Name, "goship_delete_") || tl.Name == "goship_revoke_api_token"
		if destructiveish {
			if tl.Annotations.DestructiveHint != nil && !*tl.Annotations.DestructiveHint {
				t.Errorf("%s is marked non-destructive", tl.Name)
			}
			if !strings.Contains(tl.Description, "confirm with the user") {
				t.Errorf("%s does not ask for confirmation", tl.Name)
			}
		} else if !tl.Annotations.ReadOnlyHint && (tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint) {
			t.Errorf("%s is left destructive by default", tl.Name)
		}
		if strings.Contains(strings.ToLower(tl.Description), "tsuru") {
			t.Errorf("%s names the engine", tl.Name)
		}
	}
}

func TestNoTokenIsExplainedAsAToolError(t *testing.T) {
	cs := session(t, fakeClients{url: "http://127.0.0.1:1", org: "acme"}, Options{})
	res := call(t, cs, "goship_list_apps", nil)
	if !res.IsError || !strings.Contains(text(res), "goship login") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestNoOrgIsExplainedAsAToolError(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			t.Errorf("unexpected call %s", r.URL)
		}
		w.Write([]byte(`{"email":"me@example.com"}`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok"}, Options{})
	res := call(t, cs, "goship_list_apps", nil)
	if !res.IsError || !strings.Contains(text(res), "goship_list_orgs") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
	res = call(t, cs, "goship_whoami", nil)
	if res.IsError && strings.Contains(text(res), "organization") {
		t.Errorf("an account tool must not need an org: %q", text(res))
	}
}

func TestListAppsReturnsStructuredApps(t *testing.T) {
	var gotPath, gotAuth string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Write([]byte(`[{"name":"api","status":"running","plan_name":"Small","addresses":["api.apps.goship.sh"]}]`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_list_apps", map[string]any{"org": "other"})
	if res.IsError {
		t.Fatal(text(res))
	}
	if gotPath != "/api/v1/orgs/other/apps" || gotAuth != "Bearer tok" {
		t.Errorf("called %s with %q", gotPath, gotAuth)
	}
	var out AppList
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Apps) != 1 || out.Apps[0].Name != "api" {
		t.Errorf("structured content %s: %v", raw, err)
	}
}

func TestAPIErrorsBecomeToolErrors(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"app \"nope\" not found"}`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_get_app", map[string]any{"app": "nope"})
	if !res.IsError || !strings.Contains(text(res), "not found") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestDeleteAppCallsDelete(t *testing.T) {
	var method, path string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_delete_app", map[string]any{"app": "api"})
	if res.IsError || method != http.MethodDelete || path != "/api/v1/orgs/acme/apps/api" {
		t.Errorf("%s %s: %q", method, path, text(res))
	}
}

func TestLogsClampLinesAndNeverFollow(t *testing.T) {
	var query string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"type":"log","date":"2026-10-09T10:00:00Z","source":"app","unit":"u1","message":"hello"}` + "\n" + `{"type":"result","ok":true}` + "\n")) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_logs", map[string]any{"app": "api", "lines": 1000})
	if res.IsError {
		t.Fatal(text(res))
	}
	if !strings.Contains(query, "lines=1000") || strings.Contains(query, "follow") {
		t.Errorf("query %q", query)
	}
	var out LogList
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Entries) != 1 || out.Entries[0].Message != "hello" {
		t.Errorf("structured content %s: %v", raw, err)
	}
	res = call(t, cs, "goship_logs", map[string]any{"app": "api", "lines": 5000})
	if res.IsError || !strings.Contains(query, "lines=1000") {
		t.Errorf("lines above the maximum must be clamped: %q %q", query, text(res))
	}
}

func TestDeployWithNoMarkerListsWhatItLooksFor(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/apps/empty"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/orgs"):
			w.Write([]byte(`[{"slug":"acme"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{Deploy: true})
	res := call(t, cs, "goship_deploy", map[string]any{"path": t.TempDir(), "app": "empty"})
	if !res.IsError || !strings.Contains(text(res), "go.mod") || !strings.Contains(text(res), "Dockerfile") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestHostedServerPointsDeploysAtTheCLI(t *testing.T) {
	cs := session(t, fakeClients{url: "http://127.0.0.1:1", token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_deploy", map[string]any{"path": "."})
	if !res.IsError || !strings.Contains(text(res), "goship deploy") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestRollbackReportsTheFailingLog(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"type":"step","step":"release","state":"start"}` + "\n" + //nolint:errcheck
			`{"type":"output","step":"release","data":"unit crashed: exit 1\n"}` + "\n" +
			`{"type":"result","ok":false,"error":"deploy failed: release did not become healthy"}` + "\n"))
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_rollback", map[string]any{"app": "api", "version": "v3"})
	got := text(res)
	if !res.IsError || !strings.Contains(got, "release did not become healthy") || !strings.Contains(got, "unit crashed") {
		t.Errorf("got isError=%v %q", res.IsError, got)
	}
}
