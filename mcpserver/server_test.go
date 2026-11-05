package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeAPI answers like the GoShip API for the token "the user's token" and
// records what it was asked.
type fakeAPI struct {
	mu       sync.Mutex
	orgs     string
	projects string
	users    map[string]string // Authorization header → email
	calls    []string
	auth     []string
}

func (f *fakeAPI) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/auth/me":
			// Whoever the token says, so a test can tell callers apart.
			json.NewEncoder(w).Encode(map[string]string{"email": f.users[r.Header.Get("Authorization")]})
		case "/api/v1/orgs":
			w.Write([]byte(f.orgs))
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(f.projects))
		case "/api/v1/orgs/acme/projects/atlas/environments/development/apps", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps":
			w.Write([]byte(`[{"name":"api","status":"running"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestOrgAndProjectNeedChoosingWhenThereAreSeveral(t *testing.T) {
	ctx := withToken(context.Background(), "tok")
	for name, tc := range map[string]struct {
		orgs, projects, orgErr, projectErr string
	}{
		"one of each":      {`[{"slug":"acme"}]`, `[{"slug":"atlas"}]`, "", ""},
		"several orgs":     {`[{"slug":"acme"},{"slug":"beta"}]`, `[{"slug":"atlas"}]`, "you belong to 2 organizations and none is selected: pass org; goship_list_orgs shows them", ""},
		"no org":           {`[]`, `[]`, "you do not belong to any organization", ""},
		"several projects": {`[{"slug":"acme"}]`, `[{"slug":"atlas"},{"slug":"web"}]`, "", "acme has 2 projects and none is selected: pass project; goship_list_projects shows them"},
		"no project":       {`[{"slug":"acme"}]`, `[]`, "", "you cannot reach any project in acme"},
	} {
		t.Run(name, func(t *testing.T) {
			api := &fakeAPI{orgs: tc.orgs, projects: tc.projects}
			tc2 := tokenClients{api: api.serve(t), version: "test"}
			c, _ := tc2.Client(ctx)
			org, err := tc2.Org(ctx, "")
			if tc.orgErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.orgErr) {
					t.Errorf("org error %v, want %q", err, tc.orgErr)
				}
				return
			}
			if err != nil || org != "acme" {
				t.Fatalf("org %q %v", org, err)
			}
			project, err := tc2.Project(ctx, c, org, "")
			if tc.projectErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.projectErr) {
					t.Errorf("project error %v, want %q", err, tc.projectErr)
				}
				return
			}
			if err != nil || project != "atlas" {
				t.Errorf("project %q %v", project, err)
			}
		})
	}

	// An explicit choice is taken as is, without asking the API.
	api := &fakeAPI{}
	tc := tokenClients{api: api.serve(t)}
	c, _ := tc.Client(ctx)
	if org, _ := tc.Org(ctx, "beta"); org != "beta" {
		t.Errorf("org override %q", org)
	}
	if p, _ := tc.Project(ctx, c, "beta", "web"); p != "web" {
		t.Errorf("project override %q", p)
	}
	if len(api.calls) != 0 {
		t.Errorf("overrides called the API: %v", api.calls)
	}
}

// With nothing selected on the hosted server, an environment is the one the
// tool was given, else the organization's default; a default that cannot be
// read comes back with how to choose one, for the tool to refuse.
func TestEnvironmentIsTheArgumentElseTheOrgsDefault(t *testing.T) {
	ctx := withToken(context.Background(), "tok")
	api := &fakeAPI{orgs: `[{"slug":"acme","default_environment":"staging"}]`}
	tc := tokenClients{api: api.serve(t)}
	c, _ := tc.Client(ctx)
	if env, err := tc.Environment(ctx, c, "acme", "atlas", ""); env != "staging" || err != nil {
		t.Errorf("default: %q %v", env, err)
	}
	api.calls = nil
	if env, err := tc.Environment(ctx, c, "acme", "atlas", "qa"); env != "qa" || err != nil || len(api.calls) != 0 {
		t.Errorf("override: %q %v, called %v", env, err, api.calls)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer down.Close()
	tc = tokenClients{api: down.URL}
	c, _ = tc.Client(ctx)
	env, err := tc.Environment(ctx, c, "acme", "atlas", "")
	if err == nil || !strings.Contains(err.Error(), "pass environment") || !strings.Contains(err.Error(), "goship_list_environments") {
		t.Errorf("unreadable default: %q %v", env, err)
	}
}

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func connect(t *testing.T, endpoint, token string) *mcp.ClientSession {
	t.Helper()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: bearer{token, http.DefaultTransport}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// serveTools is a minimal host: one shared server, and each request's bearer
// token put in its context with WithToken, as the hosted server does once it
// has checked the token.
func serveTools(t *testing.T, api string) string {
	t.Helper()
	server := New(api, "test")
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		token = strings.TrimSpace(token)
		h.ServeHTTP(w, r.WithContext(WithToken(r.Context(), token)))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// The tools are the local server's, but goship_deploy cannot upload a
// directory; a project tool needs neither org nor project when there is one of
// each, and reaches the API with the caller's token.
func TestToolsCallTheAPIWithTheCallersToken(t *testing.T) {
	api := &fakeAPI{orgs: `[{"slug":"acme"}]`, projects: `[{"slug":"atlas"}]`, users: map[string]string{"Bearer tok-ada": "ada@example.com"}}
	cs := connect(t, serveTools(t, api.serve(t)), "tok-ada")

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		if tool.Name == "goship_deploy" && !strings.Contains(tool.Description, "not available on the hosted server") {
			t.Errorf("goship_deploy is the local one: %s", tool.Description)
		}
	}
	for _, want := range []string{"goship_whoami", "goship_list_orgs", "goship_list_apps", "goship_list_environments"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s in %v", want, names)
		}
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "goship_list_apps", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("list apps: %v %v", err, res)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if !slices.Contains(api.calls, "GET /api/v1/orgs/acme/projects/atlas/environments/development/apps") {
		t.Errorf("API calls %v", api.calls)
	}
	for _, a := range api.auth {
		if a != "Bearer tok-ada" {
			t.Errorf("API got Authorization %q", a)
		}
	}
}

// One server is shared by everyone: concurrent callers must each reach the
// API as themselves, and a call with no token fails asking to reconnect.
func TestEachCallUsesItsOwnCallersToken(t *testing.T) {
	users := map[string]string{"Bearer tok-ada": "ada@example.com", "Bearer tok-bob": "bob@example.com", "Bearer tok-cy": "cy@example.com"}
	api := &fakeAPI{orgs: `[{"slug":"acme"}]`, users: users}
	url := serveTools(t, api.serve(t))

	var wg sync.WaitGroup
	for _, tok := range []string{"tok-ada", "tok-bob", "tok-cy"} {
		cs := connect(t, url, tok)
		for range 10 {
			wg.Go(func() {
				res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "goship_whoami", Arguments: map[string]any{}})
				if err != nil || res.IsError {
					t.Errorf("whoami: %v %v", err, res)
					return
				}
				raw, _ := json.Marshal(res.StructuredContent)
				if want := users["Bearer "+tok]; !strings.Contains(string(raw), want) {
					t.Errorf("caller %s got %s", want, raw)
				}
			})
		}
	}
	wg.Wait()

	res, err := connect(t, url, "").CallTool(context.Background(), &mcp.CallToolParams{Name: "goship_whoami", Arguments: map[string]any{}})
	if err != nil || !res.IsError {
		t.Fatalf("a call without a token: %v %+v", err, res)
	}
	raw, _ := json.Marshal(res.Content)
	if !strings.Contains(string(raw), "reconnect the GoShip connector") {
		t.Errorf("no-token error: %s", raw)
	}
}
