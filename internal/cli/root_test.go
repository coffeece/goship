package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/config"
	"github.com/spf13/cobra"
)

// Keep tests off the developer's real ~/.config/goship/config.json.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("GOSHIP_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("GOSHIP_TOKEN", "")
}

// v0.1.0 shipped `app link list` as a childless command with nothing to run: it
// printed its parent topic's blurb and exited 0. The tree was derived from
// command-name strings by tsuru-client, so nothing in our source showed it.
func TestEveryLeafCommandRuns(t *testing.T) {
	var walk func(*cobra.Command, []string)
	walk = func(c *cobra.Command, path []string) {
		path = append(path, c.Name())
		children := c.Commands()
		if len(children) == 0 {
			if c.Run == nil && c.RunE == nil {
				t.Errorf("%q has no subcommands and nothing to run", strings.Join(path, " "))
			}
			return
		}
		for _, child := range children {
			walk(child, path)
		}
	}
	walk(NewRoot("test"), nil)
}

func TestOutputFlagRejectsUnknownFormats(t *testing.T) {
	isolateConfig(t)
	root := NewRoot("test")
	root.SetArgs([]string{"version", "--output", "xml"})
	root.SetOut(&strings.Builder{})
	if err := root.Execute(); err == nil {
		t.Fatal("expected --output xml to be rejected")
	}
}

func TestVersionPrintsTheBuildVersion(t *testing.T) {
	isolateConfig(t)
	var out strings.Builder
	root := NewRoot("v1.2.3")
	root.SetArgs([]string{"version"})
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "goship v1.2.3 (") || !strings.Contains(got, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("got %q, want the version and the platform it was built for", got)
	}
}

// target returns an App against srv with nothing selected anywhere.
func target(t *testing.T, srv *httptest.Server) *App {
	t.Helper()
	t.Setenv("GOSHIP_ORG", "")
	t.Setenv("GOSHIP_PROJECT", "")
	return &App{Config: &config.Config{API: srv.URL, Token: "t"}}
}

func TestTargetPicksTheOnlyProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"menlo","name":"Menlo"}]`)) //nolint:errcheck
		case "/api/v1/orgs/menlo/projects":
			w.Write([]byte(`[{"slug":"default","name":"Default","is_default":true,"role":"admin"}]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	app := target(t, srv)
	tg, err := app.Target(context.Background())
	if err != nil || tg != (Target{Org: "menlo", Project: "default"}) {
		t.Fatalf("Target = %+v, %v", tg, err)
	}
}

func TestTargetDemandsAChoiceBetweenProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"menlo","name":"Menlo"}]`)) //nolint:errcheck
		case "/api/v1/orgs/menlo/projects":
			w.Write([]byte(`[{"slug":"clave","role":"developer"},{"slug":"mova","role":"developer"}]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	app := target(t, srv)
	if _, err := app.Target(context.Background()); !errors.Is(err, errNoProject) {
		t.Fatalf("err = %v, want errNoProject", err)
	}
	app.Global.Project = "mova"
	if tg, err := app.Target(context.Background()); err != nil || tg.Project != "mova" {
		t.Fatalf("--project: %+v, %v", tg, err)
	}
	app.Global.Project = ""
	app.ProjectProject = "clave"
	if tg, _ := app.Target(context.Background()); tg.Project != "clave" {
		t.Fatalf("goship.yaml project: %+v", tg)
	}
}

// The project a goship.yaml pins belongs to the org it pins; naming another
// org on the command line must not carry the project along.
func TestProjectFileProjectIsIgnoredForAnotherOrg(t *testing.T) {
	app := &App{Config: &config.Config{}, ProjectOrg: "menlo", ProjectProject: "clave"}
	t.Setenv("GOSHIP_PROJECT", "")
	if got := app.selectedProject("menlo"); got != "clave" {
		t.Errorf("selectedProject(menlo) = %q", got)
	}
	if got := app.selectedProject("other"); got != "" {
		t.Errorf("selectedProject(other) = %q, want none", got)
	}
}

func TestSelectedProjectOrder(t *testing.T) {
	app := &App{Config: &config.Config{Projects: map[string]string{"menlo": "from-config"}}}
	t.Setenv("GOSHIP_PROJECT", "")
	if got := app.selectedProject("menlo"); got != "from-config" {
		t.Errorf("config: %q", got)
	}
	t.Setenv("GOSHIP_PROJECT", "from-env")
	if got := app.selectedProject("menlo"); got != "from-env" {
		t.Errorf("env beats config: %q", got)
	}
	app.ProjectProject = "from-file"
	if got := app.selectedProject("menlo"); got != "from-file" {
		t.Errorf("goship.yaml beats env: %q", got)
	}
	app.Global.Project = "from-flag"
	if got := app.selectedProject("menlo"); got != "from-flag" {
		t.Errorf("--project beats the file: %q", got)
	}
}

// twoProjects serves one org with two projects; the app lives in the projects
// named by holders.
func twoProjects(t *testing.T, holders ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"menlo"}]`)) //nolint:errcheck
		case "/api/v1/orgs/menlo/projects":
			w.Write([]byte(`[{"slug":"clave"},{"slug":"mova"}]`)) //nolint:errcheck
		default:
			for _, p := range holders {
				if r.URL.Path == "/api/v1/orgs/menlo/projects/"+p+"/apps/web" {
					w.Write([]byte(`{"name":"web"}`)) //nolint:errcheck
					return
				}
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTargetForAppFindsTheOnlyProjectHoldingIt(t *testing.T) {
	app := target(t, twoProjects(t, "mova"))
	tg, err := app.TargetForApp(context.Background(), "web")
	if err != nil || tg != (Target{Org: "menlo", Project: "mova"}) {
		t.Fatalf("TargetForApp = %+v, %v", tg, err)
	}
}

func TestTargetForAppRefusesToGuessBetweenProjects(t *testing.T) {
	app := target(t, twoProjects(t, "clave", "mova"))
	_, err := app.TargetForApp(context.Background(), "web")
	if err == nil || !strings.Contains(err.Error(), "menlo/clave") || !strings.Contains(err.Error(), "menlo/mova") {
		t.Fatalf("err = %v, want both org/project pairs", err)
	}
}

func TestTargetForAppUsesTheSelectionWithoutSearching(t *testing.T) {
	app := target(t, twoProjects(t, "clave", "mova"))
	app.Global.Org, app.Global.Project = "menlo", "mova"
	tg, err := app.TargetForApp(context.Background(), "web")
	if err != nil || tg != (Target{Org: "menlo", Project: "mova"}) {
		t.Fatalf("TargetForApp = %+v, %v", tg, err)
	}
}

func TestProjectFlagSelectsTheProjectPath(t *testing.T) {
	var gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	if _, err := run(t, "", "apps", "--project", "mova"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/projects/mova/apps" {
		t.Errorf("path = %q", gotPath)
	}
}

// twoOrgs serves two orgs with projects clave and mova each; web lives in the
// "org/project" pairs named by holders, and every request is recorded.
func twoOrgs(t *testing.T, touched *[]string, holders ...string) {
	t.Helper()
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		*touched = append(*touched, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"acme"},{"slug":"globex"}]`)) //nolint:errcheck
			return
		case "/api/v1/orgs/acme/projects", "/api/v1/orgs/globex/projects":
			w.Write([]byte(`[{"slug":"clave"},{"slug":"mova"}]`)) //nolint:errcheck
			return
		}
		for _, h := range holders {
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+strings.Replace(h, "/", "/projects/", 1)+"/apps/web" {
				w.Write([]byte(`{"name":"web","platform":"go"}`)) //nolint:errcheck
				return
			}
			if r.URL.Path == "/api/v1/orgs/"+strings.Replace(h, "/", "/projects/", 1)+"/apps" {
				w.Write([]byte(`[{"name":"web"}]`)) //nolint:errcheck
				return
			}
		}
		if strings.HasSuffix(r.URL.Path, "/apps") {
			w.Write([]byte(`[]`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	t.Setenv("GOSHIP_ORG", "")
	t.Setenv("GOSHIP_PROJECT", "")
}

// A project that was asked for is never swapped for another when no org
// resolves: the app search only looks in projects of that name.
func TestTargetForAppKeepsTheProjectWhenNoOrgResolves(t *testing.T) {
	var touched []string
	twoOrgs(t, &touched, "acme/clave")

	_, err := run(t, "", "app", "info", "web", "--project", "mova")
	if err == nil || !strings.Contains(err.Error(), `not found in any project named "mova"`) {
		t.Fatalf("err = %v", err)
	}
	for _, p := range touched {
		if strings.Contains(p, "/projects/clave") {
			t.Errorf("looked in another project: %s", p)
		}
	}

	touched = nil
	twoOrgs(t, &touched, "acme/clave", "globex/mova")
	out, err := run(t, "", "app", "info", "web", "--project", "mova")
	if err != nil || !strings.Contains(out, "web") || !slices.Contains(touched, "GET /api/v1/orgs/globex/projects/mova/apps/web") {
		t.Fatalf("out %q err %v touched %q", out, err, touched)
	}
}

func TestGOSHIPProjectNarrowsTheAppSearchToo(t *testing.T) {
	var touched []string
	twoOrgs(t, &touched, "acme/clave")
	t.Setenv("GOSHIP_PROJECT", "mova")
	if _, err := run(t, "", "app", "info", "web"); err == nil {
		t.Fatal("web is only in clave; GOSHIP_PROJECT=mova must not find it")
	}
}

func TestListerKeepsTheProjectWhenNoOrgResolves(t *testing.T) {
	var touched []string
	twoOrgs(t, &touched, "acme/clave", "globex/mova")
	out, err := run(t, "", "apps", "--project", "mova", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "clave") || !strings.Contains(out, "globex/mova") {
		t.Errorf("output:\n%s", out)
	}
}

// A saved or GOSHIP_PROJECT selection that is no longer reachable earns an
// explanation, not the API's bare 404.
func TestStaleProjectSelectionIsExplained(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs/acme/projects" {
			w.Write([]byte(`[{"slug":"clave"}]`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	t.Setenv("GOSHIP_PROJECT", "gone")

	for _, args := range [][]string{{"apps"}, {"app", "rm", "web", "--yes"}, {"app", "info", "web"}} {
		_, err := run(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), `project "gone" is not reachable in acme`) || !strings.Contains(err.Error(), "goship project use") {
			t.Errorf("%v: err = %v", args, err)
		}
	}

	// Said on the command line, the project is the user's own words: the API's
	// answer stands, and a project that does exist keeps its plain errors.
	if _, err := run(t, "", "apps", "--project", "gone"); err == nil || strings.Contains(err.Error(), "not reachable") {
		t.Errorf("--project: err = %v", err)
	}
	t.Setenv("GOSHIP_PROJECT", "clave")
	if _, err := run(t, "", "app", "rm", "web", "--yes"); err == nil || strings.Contains(err.Error(), "not reachable") {
		t.Errorf("a valid project: err = %v", err)
	}
}
