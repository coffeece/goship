package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeClients is a Clients over an httptest API: one token, one org, one
// project.
type fakeClients struct {
	url     string
	token   string
	org     string
	project string
	// saved are the environments `goship environment use` chose, by
	// "org/project"; any other project is in production.
	saved map[string]string
	// envErr is why the organization's default environment cannot be read.
	envErr error
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

func (f fakeClients) Project(_ context.Context, _ *portal.Client, _, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if f.project == "" {
		return "", errors.New(`no project selected: pass project, or run "goship project use <slug>"; goship_list_projects shows them`)
	}
	return f.project, nil
}

func (f fakeClients) Environment(_ context.Context, _ *portal.Client, org, project, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if env := f.selected(deploy.Target{Org: org, Project: project}); env != "" {
		return env, nil
	}
	return deploy.FallbackEnvironment, f.envErr
}

func (f fakeClients) selected(t deploy.Target) string { return f.saved[t.Org+"/"+t.Project] }

func (f fakeClients) DeploySelection(c *portal.Client) (func(deploy.Target) string, deploy.OrgLister) {
	return f.selected, c.Orgs
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
		destructiveish := strings.HasPrefix(tl.Name, "goship_delete_") || tl.Name == "goship_revoke_api_token" || tl.Name == "goship_resize_cluster" ||
			tl.Name == "goship_remove_project_domain" || tl.Name == "goship_enable_database_server_pitr" || tl.Name == "goship_resize_database_server"
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
		if node.MatchString(tl.Name) || node.MatchString(tl.Description) || node.MatchString(string(schema(t, tl))) {
			t.Errorf("%s still speaks of nodes: %s", tl.Name, tl.Description)
		}
	}
}

// node is the word clusters replaced; nodejs is a platform.
var node = regexp.MustCompile(`(?i)\bnodes?\b|_nodes?\b`)

func schema(t *testing.T, tl *mcp.Tool) []byte {
	t.Helper()
	raw, err := json.Marshal(tl.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// properties are the arguments a tool takes, with their descriptions.
func properties(t *testing.T, tl *mcp.Tool) map[string]string {
	t.Helper()
	var s struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema(t, tl), &s); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range s.Properties {
		out[k] = v.Description
	}
	return out
}

func toolsByName(t *testing.T, o Options) map[string]*mcp.Tool {
	t.Helper()
	cs := session(t, fakeClients{}, o)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		out[tl.Name] = tl
	}
	return out
}

func TestEnvironmentAndClusterToolsAreRegistered(t *testing.T) {
	all := toolsByName(t, Options{Deploy: true})
	for name, k := range map[string]kind{
		"goship_list_environments":   read,
		"goship_create_environment":  write,
		"goship_delete_environment":  destructive,
		"goship_enable_environment":  write,
		"goship_disable_environment": write,
		"goship_list_clusters":       read,
		"goship_get_cluster":         read,
		"goship_list_cluster_sizes":  read,
		"goship_create_cluster":      write,
		"goship_resize_cluster":      destructive,
		"goship_delete_cluster":      destructive,
	} {
		tl, ok := all[name]
		if !ok {
			t.Errorf("%s is not registered", name)
			continue
		}
		a := tl.Annotations
		switch k {
		case read:
			if !a.ReadOnlyHint {
				t.Errorf("%s must be read-only", name)
			}
		case write:
			if a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint {
				t.Errorf("%s must be a non-destructive write", name)
			}
		case destructive:
			if a.ReadOnlyHint || (a.DestructiveHint != nil && !*a.DestructiveHint) || !strings.Contains(tl.Description, "confirm with the user") {
				t.Errorf("%s must be destructive and ask for confirmation", name)
			}
		}
	}
	for _, gone := range []string{"goship_move_cloud_account", "goship_list_nodes", "goship_resize_node"} {
		if _, ok := all[gone]; ok {
			t.Errorf("%s is still registered", gone)
		}
	}
}

// Every tool that takes a project takes an environment beside it, and the
// tools of the organization (clusters, environments, cloud accounts,
// database servers) take neither.
var projectDomainTools = map[string]bool{
	"goship_list_project_domains":             true,
	"goship_add_project_domain":               true,
	"goship_remove_project_domain":            true,
	"goship_set_project_domain_environment":   true,
	"goship_unset_project_domain_environment": true,
	"goship_apply_project_domain":             true,
}

func TestEveryProjectToolTakesAnEnvironment(t *testing.T) {
	orgLevel := regexp.MustCompile(`_(environments?|clusters?|cluster_sizes|cloud_\w+|database_servers?|database_server_\w+)$`)
	for _, o := range []Options{{Deploy: true}, {}} {
		want := "environment slug; defaults to the organization's default environment (development unless changed)"
		if o.Deploy {
			want = localEnvironmentDesc
		}
		for name, tl := range toolsByName(t, o) {
			props := properties(t, tl)
			_, hasProject := props["project"]
			switch {
			case projectDomainTools[name]:
				// Acts on the project as a whole; a rule names its environment itself.
			case orgLevel.MatchString(name):
				if hasProject {
					t.Errorf("%s belongs to the organization but takes a project", name)
				}
			case hasProject && props["environment"] != want:
				t.Errorf("%s takes a project but its environment is %q", name, props["environment"])
			}
		}
	}
}

func TestLimitToolsAreRegisteredAsWrites(t *testing.T) {
	cs := session(t, fakeClients{}, Options{})
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tl := range tools.Tools {
		if tl.Name == "goship_set_database_size_limit" || tl.Name == "goship_set_database_server_disk_limit" {
			found[tl.Name] = true
			if tl.Annotations == nil || tl.Annotations.ReadOnlyHint || (tl.Annotations.DestructiveHint != nil && *tl.Annotations.DestructiveHint) {
				t.Errorf("%s must be a non-destructive write", tl.Name)
			}
		}
	}
	if len(found) != 2 {
		t.Errorf("registered %v", found)
	}
}

func TestNoTokenIsExplainedAsAToolError(t *testing.T) {
	cs := session(t, fakeClients{url: "http://127.0.0.1:1", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_list_apps", nil)
	if !res.IsError || !strings.Contains(text(res), "goship login") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestNoOrgIsExplainedAsAToolError(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/me":
			w.Write([]byte(`{"email":"me@example.com","groups":["acme-atlas"]}`)) //nolint:errcheck
		case "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"acme"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected call %s", r.URL)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok"}, Options{})
	res := call(t, cs, "goship_list_apps", nil)
	if !res.IsError || !strings.Contains(text(res), "goship_list_orgs") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
	res = call(t, cs, "goship_whoami", nil)
	if res.IsError {
		t.Fatalf("an account tool must not need an org: %q", text(res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"orgs":["acme"]`) || strings.Contains(string(raw), "acme-atlas") {
		t.Errorf("whoami must list organizations, not platform groups: %s", raw)
	}
}

func TestListAppsReturnsStructuredApps(t *testing.T) {
	var gotPath, gotAuth string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Write([]byte(`[{"name":"api","status":"running","plan_name":"Small","addresses":["api.apps.goship.sh"]}]`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_list_apps", map[string]any{"org": "other", "project": "comet"})
	if res.IsError {
		t.Fatal(text(res))
	}
	if gotPath != "/api/v1/orgs/other/projects/comet/environments/development/apps" || gotAuth != "Bearer tok" {
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_delete_app", map[string]any{"app": "api"})
	if res.IsError || method != http.MethodDelete || path != "/api/v1/orgs/acme/projects/atlas/environments/development/apps/api" {
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
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
		case strings.HasSuffix(r.URL.Path, "/projects"):
			w.Write([]byte(`[{"slug":"atlas"}]`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/environments"):
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{Deploy: true})
	res := call(t, cs, "goship_deploy", map[string]any{"path": t.TempDir(), "app": "empty"})
	if !res.IsError || !strings.Contains(text(res), "go.mod") || !strings.Contains(text(res), "Dockerfile") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestHostedServerPointsDeploysAtTheCLI(t *testing.T) {
	cs := session(t, fakeClients{url: "http://127.0.0.1:1", token: "tok", org: "acme", project: "atlas"}, Options{})
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_rollback", map[string]any{"app": "api", "version": "v3"})
	got := text(res)
	if !res.IsError || !strings.Contains(got, "release did not become healthy") || !strings.Contains(got, "unit crashed") {
		t.Errorf("got isError=%v %q", res.IsError, got)
	}
}

func TestNoProjectIsExplainedAsAToolError(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected call %s", r.URL)
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_list_apps", nil)
	if !res.IsError || !strings.Contains(text(res), "goship_list_projects") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestListProjectsNeedsNoProject(t *testing.T) {
	var gotPath string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"slug":"atlas","name":"Atlas","role":"developer"}]`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_list_projects", nil)
	if res.IsError || gotPath != "/api/v1/orgs/acme/projects" {
		t.Errorf("%s: %q", gotPath, text(res))
	}
}

// deployPaths drives goship_deploy in an org with two projects, each in
// development and staging, and records the app paths it touches; holder is
// the "project/environment" that already has the app. The deploy itself is
// refused, so the call ends there.
func deployPaths(t *testing.T, fc fakeClients, dir string, args map[string]any, holder string) ([]string, *mcp.CallToolResult) {
	t.Helper()
	var paths []string
	project, env, _ := strings.Cut(holder, "/")
	fc.url = api(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"acme","default_environment":"development"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"atlas"},{"slug":"comet"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/acme/environments":
			w.Write([]byte(`[{"slug":"development"},{"slug":"staging"}]`)) //nolint:errcheck
		case strings.Contains(r.URL.Path, "/apps"):
			paths = append(paths, r.Method+" "+r.URL.Path)
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/projects/"+project+"/environments/"+env+"/apps/web":
				w.Write([]byte(`{"name":"web","platform":"go"}`)) //nolint:errcheck
			case r.Method == http.MethodPost:
				w.WriteHeader(http.StatusForbidden)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	fc.token = "tok"
	cs := session(t, fc, Options{Deploy: true})
	args["path"], args["app"] = dir, "web"
	return paths, call(t, cs, "goship_deploy", args)
}

// With several projects and none passed, the deploy is not refused up front:
// the project that already holds the app decides.
func TestDeployFindsTheProjectHoldingTheApp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module web\n")
	paths, _ := deployPaths(t, fakeClients{org: "acme"}, dir, map[string]any{}, "comet/staging")
	if !slices.Contains(paths, "POST /api/v1/orgs/acme/projects/comet/environments/staging/apps/web/deploys") {
		t.Errorf("paths = %q, want the deploy to go to comet/staging", paths)
	}
}

// A goship.yml in the deployed directory names the project.
func TestDeployReadsTheProjectFromGoshipYml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module web\n")
	writeFile(t, dir+"/goship.yml", "project: atlas\n")
	paths, _ := deployPaths(t, fakeClients{org: "acme"}, dir, map[string]any{}, "atlas/development")
	if !slices.Contains(paths, "POST /api/v1/orgs/acme/projects/atlas/environments/development/apps/web/deploys") {
		t.Errorf("paths = %q, want the deploy to go to atlas", paths)
	}
}

// With nothing to go on, a new app asks for a project in the tool's words.
func TestDeployAsksForAProjectWhenNothingDecides(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module web\n")
	_, res := deployPaths(t, fakeClients{org: "acme"}, dir, map[string]any{}, "none/none")
	if !res.IsError || !strings.Contains(text(res), "goship_list_projects") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// goship.yml in the deployed directory pins the org; that beats the one the
// caller has selected.
func TestDeployUsesTheOrgGoshipYmlPins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module web\n")
	writeFile(t, dir+"/goship.yml", "org: other\nproject: comet\n")
	var paths []string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"acme"},{"slug":"other"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/other/projects":
			w.Write([]byte(`[{"slug":"comet"}]`)) //nolint:errcheck
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{Deploy: true})
	call(t, cs, "goship_deploy", map[string]any{"path": dir, "app": "web", "plan": "app-free"})
	if !slices.Contains(paths, "POST /api/v1/orgs/other/projects/comet/environments/development/apps") {
		t.Errorf("paths = %q, want the app created in other/comet", paths)
	}
	for _, p := range paths {
		if strings.Contains(p, "/orgs/acme/projects/atlas") {
			t.Errorf("touched the selected org's project: %s", p)
		}
	}
}

func TestPrivateAppTools(t *testing.T) {
	base := "/api/v1/orgs/acme/projects/atlas/environments/development/apps/api"
	cases := []struct {
		tool, method, path, query, body string
		args                            map[string]any
	}{
		{"goship_set_app_visibility", http.MethodPut, base + "/visibility", "", `"visibility":"private"`, map[string]any{"visibility": "private"}},
		{"goship_list_app_access", http.MethodGet, base + "/access/users", "", "", nil},
		{"goship_add_app_access", http.MethodPost, base + "/access/users", "", `"username":"ana"`, map[string]any{"username": "ana"}},
		{"goship_reset_app_access", http.MethodPut, base + "/access/users/ana", "", "", map[string]any{"username": "ana"}},
		{"goship_remove_app_access", http.MethodDelete, base + "/access/users/ana", "", "", map[string]any{"username": "ana"}},
		{"goship_get_app_access_token", http.MethodGet, base + "/access/token", "reveal=1", "", map[string]any{"reveal": true}},
		{"goship_rotate_app_access_token", http.MethodPost, base + "/access/token/rotate", "", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			var method, path, query, body string
			url := api(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				method, path, query, body = r.Method, r.URL.Path, r.URL.RawQuery, string(b)
				switch {
				case r.Method == http.MethodDelete || strings.HasSuffix(path, "/visibility"):
					w.WriteHeader(http.StatusNoContent)
				case strings.HasSuffix(path, "/access/users") && r.Method == http.MethodGet:
					w.Write([]byte(`{"users":[{"username":"ana","created_at":"2026-10-20T10:00:00Z"}]}`)) //nolint:errcheck
				default:
					w.Write([]byte(`{"username":"ana","password":"pw","token":"tk"}`)) //nolint:errcheck
				}
			})
			cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
			args := map[string]any{"app": "api"}
			maps.Copy(args, tc.args)
			res := call(t, cs, tc.tool, args)
			if res.IsError {
				t.Fatal(text(res))
			}
			if method != tc.method || path != tc.path || query != tc.query || !strings.Contains(body, tc.body) {
				t.Errorf("%s %s?%s %s", method, path, query, body)
			}
		})
	}
}

func TestHostedDeployRunsAnImage(t *testing.T) {
	var paths []string
	var body string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"acme"}]`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/deploys/image"):
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Write([]byte(`{"type":"result","ok":true}` + "\n")) //nolint:errcheck
		case r.Method == http.MethodPost:
			w.Write([]byte(`{"name":"web"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_deploy", map[string]any{"app": "web", "image": "ghcr.io/a/b:1", "plan": "app-free"})
	if res.IsError {
		t.Fatalf("isError: %s", text(res))
	}
	const deployed = "POST /api/v1/orgs/acme/projects/atlas/environments/development/apps/web/deploys/image"
	if !slices.Contains(paths, deployed) {
		t.Errorf("paths = %q, want %q", paths, deployed)
	}
	if !strings.Contains(body, "ghcr.io/a/b:1") {
		t.Errorf("body = %q, want the image", body)
	}
}

func TestHostedDeployStillRefusesADirectory(t *testing.T) {
	cs := session(t, fakeClients{url: "http://127.0.0.1:1", token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_deploy", map[string]any{"path": "."})
	const want = "deploys need the project directory, which this hosted server cannot read: run `goship deploy <dir>` in a terminal, or add the local server with `goship mcp`"
	if !res.IsError || !strings.Contains(text(res), want) {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

func TestLocalDeployNeedsPathOrImage(t *testing.T) {
	cs := session(t, fakeClients{url: "http://127.0.0.1:1", token: "tok", org: "acme", project: "atlas"}, Options{Deploy: true})
	res := call(t, cs, "goship_deploy", map[string]any{"app": "web"})
	if !res.IsError || !strings.Contains(text(res), "path") || !strings.Contains(text(res), "image") {
		t.Errorf("got isError=%v %q", res.IsError, text(res))
	}
}

// The hosted server never reads the path a caller names, even with an image.
func TestHostedDeployIgnoresThePath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/goship.yml", "org: other\nproject: comet\n")
	var paths []string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"acme"},{"slug":"other"}]`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/deploys/image"):
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.Write([]byte(`{"type":"result","ok":true}` + "\n")) //nolint:errcheck
		case r.Method == http.MethodPost:
			w.Write([]byte(`{"name":"web"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_deploy", map[string]any{"path": dir, "app": "web", "image": "ghcr.io/a/b:1", "plan": "app-free"})
	if res.IsError {
		t.Fatalf("isError: %s", text(res))
	}
	const want = "POST /api/v1/orgs/acme/projects/atlas/environments/development/apps/web/deploys/image"
	if !slices.Contains(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
	for _, p := range paths {
		if strings.Contains(p, "/orgs/other") {
			t.Errorf("read goship.yml from the path: %s", p)
		}
	}
}
