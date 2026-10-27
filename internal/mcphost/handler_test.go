package mcphost

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const prmURL = "https://mcp.goship.example/.well-known/oauth-protected-resource/mcp"

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
		case "/api/v1/orgs/acme/projects/clave/apps":
			w.Write([]byte(`[{"name":"api","status":"running"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func testConfig(api string) Config {
	return Config{APIURL: api, ResourceURL: "https://mcp.goship.example/mcp", Issuer: testIssuer}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestProtectedResourceMetadata(t *testing.T) {
	srv := httptest.NewServer(Handler(testConfig("http://api.invalid"), nil, "test", quiet()))
	defer srv.Close()
	want := `{"resource":"https://mcp.goship.example/mcp","authorization_servers":["https://goship.example"],"bearer_methods_supported":["header"],"resource_name":"GoShip"}`
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || strings.TrimSpace(string(body)) != want {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
}

func TestMCPWithoutAValidTokenStartsTheSignIn(t *testing.T) {
	s := newSigner(t, "k1")
	v, _, _ := newTestVerifier(t, s)
	srv := httptest.NewServer(Handler(testConfig("http://api.invalid"), v, "test", quiet()))
	defer srv.Close()

	for name, tc := range map[string]struct{ auth, desc string }{
		"no header":    {"", "sign in to GoShip"},
		"not bearer":   {"Basic YTpi", "sign in to GoShip"},
		"empty bearer": {"Bearer ", "sign in to GoShip"},
		"forged":       {"Bearer " + newSigner(t, "k1").sign(t, nil), "the GoShip token is not valid"},
		"expired":      {"Bearer " + s.sign(t, map[string]any{"exp": 1}), "the GoShip token has expired"},
		"an API token": {"Bearer gsp_123", "the GoShip token is not valid"},
	} {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			want := `Bearer error="invalid_token", error_description="` + tc.desc + `", resource_metadata="` + prmURL + `"`
			if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != want {
				t.Errorf("got %d %q\nwant 401 %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"), want)
			}
		})
	}
}

func TestMCPAnswers503WhenKeysAreUnreachable(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer down.Close()
	srv := httptest.NewServer(Handler(testConfig("http://api.invalid"), NewVerifier(testIssuer, down.URL), "test", quiet()))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+newSigner(t, "k1").sign(t, nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("WWW-Authenticate") != "" {
		t.Errorf("got %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
}

// bearer adds a token to every request, the way a connector does.
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

// TestHostedServerEndToEnd drives the server the way claude.ai would: a
// signed-in Streamable HTTP client lists the tools and calls some, and the
// calls reach the API with the user's own token.
func TestHostedServerEndToEnd(t *testing.T) {
	s := newSigner(t, "k1")
	v, _, _ := newTestVerifier(t, s)
	token := s.sign(t, nil)
	api := &fakeAPI{orgs: `[{"slug":"acme"}]`, projects: `[{"slug":"clave"}]`, users: map[string]string{"Bearer " + token: "ada@example.com"}}
	srv := httptest.NewServer(Handler(testConfig(api.serve(t)), v, "test", quiet()))
	defer srv.Close()
	cs := connect(t, srv.URL+"/mcp", token)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
		// A deploy reads a local directory; the hosted server only says so.
		if tool.Name == "goship_deploy" && !strings.Contains(tool.Description, "Not available on the hosted server") {
			t.Errorf("goship_deploy is the local one: %s", tool.Description)
		}
	}
	for _, want := range []string{"goship_whoami", "goship_list_orgs", "goship_list_apps"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing %s in %v", want, names)
		}
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "goship_whoami", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("whoami: %v %v", err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), "ada@example.com") || !strings.Contains(string(raw), "acme") {
		t.Errorf("whoami: %s", raw)
	}

	// With one org and one project, a project tool needs neither.
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "goship_list_apps", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("list apps: %v %v", err, res)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if !slices.Contains(api.calls, "GET /api/v1/orgs/acme/projects/clave/apps") {
		t.Errorf("API calls %v", api.calls)
	}
	for _, a := range api.auth {
		if a != "Bearer "+token {
			t.Errorf("API got Authorization %q", a)
		}
	}
}

func TestOrgAndProjectNeedChoosingWhenThereAreSeveral(t *testing.T) {
	ctx := withToken(context.Background(), "tok")
	for name, tc := range map[string]struct {
		orgs, projects, orgErr, projectErr string
	}{
		"one of each":      {`[{"slug":"acme"}]`, `[{"slug":"clave"}]`, "", ""},
		"several orgs":     {`[{"slug":"acme"},{"slug":"beta"}]`, `[{"slug":"clave"}]`, "you belong to 2 organizations and none is selected: pass org; goship_list_orgs shows them", ""},
		"no org":           {`[]`, `[]`, "you do not belong to any organization", ""},
		"several projects": {`[{"slug":"acme"}]`, `[{"slug":"clave"},{"slug":"web"}]`, "", "acme has 2 projects and none is selected: pass project; goship_list_projects shows them"},
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
			if err != nil || project != "clave" {
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

func TestConfigFromEnv(t *testing.T) {
	c, err := ConfigFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8888" || c.APIURL != "https://api.goship.sh" || c.ResourceURL != "https://mcp.goship.sh/mcp" ||
		c.Issuer != "https://goship.sh" || c.JWKSURL() != "https://goship.sh/.well-known/jwks.json" ||
		c.ResourceMetadataURL() != "https://mcp.goship.sh/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("defaults %+v", c)
	}
	env := map[string]string{"PORT": "9000", "GOSHIP_ISSUER": "not a url"}
	if _, err := ConfigFromEnv(func(k string) string { return env[k] }); err == nil {
		t.Error("a bad issuer must be refused")
	}
}

// TestEachCallUsesItsOwnCallersToken guards the shared server: concurrent
// users must each reach the API as themselves.
func TestEachCallUsesItsOwnCallersToken(t *testing.T) {
	s := newSigner(t, "k1")
	v, _, _ := newTestVerifier(t, s)
	users := map[string]string{}
	var tokens []string
	for _, email := range []string{"ada@example.com", "bob@example.com", "cy@example.com"} {
		tok := s.sign(t, map[string]any{"sub": email, "email": email})
		users["Bearer "+tok] = email
		tokens = append(tokens, tok)
	}
	api := &fakeAPI{orgs: `[{"slug":"acme"}]`, users: users}
	srv := httptest.NewServer(Handler(testConfig(api.serve(t)), v, "test", quiet()))
	defer srv.Close()

	var wg sync.WaitGroup
	for _, tok := range tokens {
		cs := connect(t, srv.URL+"/mcp", tok)
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
}
