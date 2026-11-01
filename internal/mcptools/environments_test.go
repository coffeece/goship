package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// The environment argument is part of every path a project tool calls.
func TestEnvironmentArgReachesThePath(t *testing.T) {
	var paths []string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/apps"):
			w.Write([]byte(`[{"name":"api"}]`)) //nolint:errcheck
		default:
			w.Write([]byte(`{"name":"api"}`)) //nolint:errcheck
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})

	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"goship_list_apps", map[string]any{"environment": "staging"}, "GET /api/v1/orgs/acme/projects/clave/environments/staging/apps"},
		{"goship_get_app", map[string]any{"app": "api", "environment": "staging"}, "GET /api/v1/orgs/acme/projects/clave/environments/staging/apps/api"},
		{"goship_get_database", map[string]any{"database": "db", "environment": "staging"}, "GET /api/v1/orgs/acme/projects/clave/environments/staging/databases/db"},
		{"goship_list_apps", nil, "GET /api/v1/orgs/acme/projects/clave/environments/production/apps"},
	} {
		paths = nil
		if res := call(t, cs, c.tool, c.args); res.IsError {
			t.Errorf("%s %v: %s", c.tool, c.args, text(res))
			continue
		}
		if len(paths) != 1 || paths[0] != c.want {
			t.Errorf("%s %v called %v, want %s", c.tool, c.args, paths, c.want)
		}
	}
}

// An environment that does not exist is the API's answer, word for word, and
// the call is not retried in another environment.
func TestUnknownEnvironmentIsNotSwallowed(t *testing.T) {
	var paths []string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"environment \"qa\" not found in acme"}`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	res := call(t, cs, "goship_list_apps", map[string]any{"environment": "qa"})
	if !res.IsError || text(res) != `environment "qa" not found in acme` {
		t.Errorf("isError=%v %q", res.IsError, text(res))
	}
	if len(paths) != 1 || paths[0] != "GET /api/v1/orgs/acme/projects/clave/environments/qa/apps" {
		t.Errorf("called %v", paths)
	}
}

// When nothing chose the environment and the organization's default cannot
// be read, a tool refuses rather than act in a guessed production.
func TestAnUnreadableDefaultEnvironmentIsRefused(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
	})
	fc := fakeClients{url: url, token: "tok", org: "acme", project: "clave", envErr: errors.New("could not determine the default environment of acme: pass environment")}
	cs := session(t, fc, Options{})
	for tool, args := range map[string]map[string]any{"goship_list_apps": nil, "goship_delete_app": {"app": "api"}} {
		res := call(t, cs, tool, args)
		if !res.IsError || !strings.Contains(text(res), "pass environment") {
			t.Errorf("%s: isError=%v %q", tool, res.IsError, text(res))
		}
	}
}

// goship_deploy weighs `goship environment use` like every other tool: after
// its environment argument and the deployed directory's goship.yml.
func TestDeployToolHonoursTheSavedEnvironment(t *testing.T) {
	saved := fakeClients{org: "acme", project: "clave", saved: map[string]string{"acme/clave": "staging"}}
	for _, c := range []struct {
		name, file, want string
		args             map[string]any
	}{
		{"saved", "", "staging", map[string]any{}},
		{"argument beats it", "", "production", map[string]any{"environment": "production"}},
		{"goship.yml beats it", "environment: production\n", "production", map[string]any{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir+"/go.mod", "module web\n")
			if c.file != "" {
				writeFile(t, dir+"/goship.yml", c.file)
			}
			paths, _ := deployPaths(t, saved, dir, c.args, "clave/"+c.want)
			want := "POST /api/v1/orgs/acme/projects/clave/environments/" + c.want + "/apps/web/deploys"
			if !slices.Contains(paths, want) {
				t.Errorf("paths = %q, want %s", paths, want)
			}
		})
	}
}

// Without a plan, goship_create_app picks one the environment's cluster
// offers for free.
func TestCreateAppPicksAFreePlanOfTheEnvironment(t *testing.T) {
	var created map[string]any
	var asked string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects/clave/environments/staging/available-plans":
			asked = r.URL.RawQuery
			w.Write([]byte(`[{"slug":"app-small","is_active":true,"billed":true},{"slug":"byon-app","is_active":true,"billed":false,"default":true}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/clave/environments/staging/apps":
			json.NewDecoder(r.Body).Decode(&created) //nolint:errcheck
			w.Write([]byte(`{"name":"web"}`))        //nolint:errcheck
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cs := session(t, fakeClients{url: url, token: "t", org: "acme", project: "clave"}, Options{})
	if res := call(t, cs, "goship_create_app", map[string]any{"name": "web", "platform": "go", "environment": "staging"}); res.IsError {
		t.Fatalf("goship_create_app: %s", text(res))
	}
	if asked != "kind=app" || created["plan"] != "byon-app" {
		t.Errorf("asked ?%s, created %v", asked, created)
	}
	if _, ok := created["node_id"]; ok {
		t.Errorf("created %v names a node", created)
	}
}

// environmentsAPI is an organization with GoShip's shared cluster, one of
// its own and one database server.
func environmentsAPI(t *testing.T, record func(r *http.Request, body string)) string {
	t.Helper()
	return api(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		record(r, string(b))
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/clusters":
			w.Write([]byte(`[{"id":"c-goship","name":"goship","shared":true},{"id":"c-home","name":"homolog"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/database-servers":
			w.Write([]byte(`[{"id":"s-1","name":"pg-staging"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/environments":
			w.Write([]byte(`[{"slug":"production","is_default":true,"cluster_name":"goship"},{"slug":"staging","cluster_name":"homolog"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/environments":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"slug":"staging"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestListEnvironmentsNeedsNoProject(t *testing.T) {
	url := environmentsAPI(t, func(*http.Request, string) {})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_list_environments", nil)
	if res.IsError {
		t.Fatal(text(res))
	}
	var out EnvironmentList
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Environments) != 2 || !out.Environments[0].IsDefault {
		t.Errorf("structured content %s: %v", raw, err)
	}
}

func TestCreateEnvironmentResolvesTheClusterByName(t *testing.T) {
	var posts []string
	url := environmentsAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost {
			posts = append(posts, body)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})

	res := call(t, cs, "goship_create_environment", map[string]any{"slug": "staging", "name": "Staging", "cluster": "homolog", "db_server": "pg-staging"})
	if res.IsError {
		t.Fatal(text(res))
	}
	if len(posts) != 1 || posts[0] != `{"name":"Staging","slug":"staging","cluster_id":"c-home","db_server_id":"s-1"}` {
		t.Errorf("posted %v", posts)
	}

	// An id is taken as is; without cluster or server, GoShip's own are used.
	posts = nil
	if res := call(t, cs, "goship_create_environment", map[string]any{"slug": "qa", "cluster": "c-goship"}); res.IsError {
		t.Fatal(text(res))
	}
	if res := call(t, cs, "goship_create_environment", map[string]any{"slug": "dev"}); res.IsError {
		t.Fatal(text(res))
	}
	if len(posts) != 2 || posts[0] != `{"name":"qa","slug":"qa","cluster_id":"c-goship"}` || posts[1] != `{"name":"dev","slug":"dev"}` {
		t.Errorf("posted %v", posts)
	}

	posts = nil
	res = call(t, cs, "goship_create_environment", map[string]any{"slug": "qa", "cluster": "nope"})
	if !res.IsError || !strings.Contains(text(res), `no cluster "nope"`) || !strings.Contains(text(res), "goship_list_clusters") || len(posts) != 0 {
		t.Errorf("unknown cluster: isError=%v %q, posted %v", res.IsError, text(res), posts)
	}
}

// A refusal to delete names what is still in the environment; the agent
// gets it as the API wrote it.
func TestDeleteEnvironmentReturnsTheConflict(t *testing.T) {
	const conflict = `environment "staging" still holds apps in clave (api, worker) and databases in mova (db)`
	var method, path string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": conflict}) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_delete_environment", map[string]any{"environment": "staging"})
	if !res.IsError || text(res) != conflict {
		t.Errorf("isError=%v %q", res.IsError, text(res))
	}
	if method != http.MethodDelete || path != "/api/v1/orgs/acme/environments/staging" {
		t.Errorf("called %s %s", method, path)
	}
}

// environmentArgDescription is the description of the environment property in
// the input schema a server advertises for tool.
func environmentArgDescription(t *testing.T, o Options, tool string) string {
	t.Helper()
	tools, err := session(t, fakeClients{}, o).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools.Tools {
		if tl.Name != tool {
			continue
		}
		raw, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		p, ok := schema.Properties["environment"]
		if !ok {
			t.Fatalf("%s has no environment property: %s", tool, raw)
		}
		return p.Description
	}
	t.Fatalf("%s is not registered", tool)
	return ""
}

// What an agent reads about the default environment must hold on the server
// it talks to: the local one carries a selection of its own that the hosted
// one has no way to.
func TestEnvironmentDescriptionTellsTheTruthPerServer(t *testing.T) {
	const selection = "goship environment use"
	local := environmentArgDescription(t, Options{Deploy: true}, "goship_delete_app")
	hosted := environmentArgDescription(t, Options{}, "goship_delete_app")

	if !strings.Contains(local, selection) {
		t.Errorf("local server: %q does not name the selection it applies", local)
	}
	if strings.Contains(hosted, selection) {
		t.Errorf("hosted server: %q names a selection the server has none of", hosted)
	}
	if local == hosted {
		t.Errorf("both servers say %q", local)
	}
	if want := "production unless changed"; !strings.Contains(hosted, want) {
		t.Errorf("hosted server: %q lacks %q", hosted, want)
	}

	// The local text reaches every tool that takes the argument, goship_deploy
	// and the org-scoped ones included, and only those.
	if got := environmentArgDescription(t, Options{Deploy: true}, "goship_deploy"); got != local {
		t.Errorf("goship_deploy says %q, want %q", got, local)
	}
	for _, o := range []Options{{Deploy: true}, {}} {
		if got := environmentArgDescription(t, o, "goship_delete_environment"); got != "environment slug (goship_list_environments)" {
			t.Errorf("deploy=%v: goship_delete_environment names the environment to delete, not a default: %q", o.Deploy, got)
		}
	}
}
