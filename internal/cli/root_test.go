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
	t.Setenv("GOSHIP_ENVIRONMENT", "")
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
	t.Setenv("GOSHIP_ENVIRONMENT", "")
	return &App{Config: &config.Config{API: srv.URL, Token: "t"}}
}

func TestTargetPicksTheOnlyProject(t *testing.T) {
	orgsListed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			orgsListed++
			w.Write([]byte(`[{"slug":"menlo","name":"Menlo","default_environment":"live"}]`)) //nolint:errcheck
		case "/api/v1/orgs/menlo/projects":
			w.Write([]byte(`[{"slug":"default","name":"Default","is_default":true,"role":"admin"}]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	app := target(t, srv)
	tg, err := app.Target(context.Background())
	if err != nil || tg != (Target{Org: "menlo", Project: "default", Environment: "live"}) {
		t.Fatalf("Target = %+v, %v", tg, err)
	}
	// The org and the default environment come from one GET /orgs.
	if orgsListed != 1 {
		t.Errorf("GET /orgs asked %d times, want once per command", orgsListed)
	}
}

// orgsDown is a portal whose GET /orgs fails; everything else is a 404.
func orgsDown(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs" {
			http.Error(w, `{"error":"down"}`, http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A command that only reads may go on in production when the default cannot
// be read, but says so, once.
func TestTargetNotesTheGuessOnce(t *testing.T) {
	app := target(t, orgsDown(t))
	app.Global.Org, app.Global.Project = "acme", "clave"
	var stderr strings.Builder
	app.Err = &stderr
	for range 2 {
		tg, err := app.Target(context.Background())
		if err != nil || tg != (Target{Org: "acme", Project: "clave", Environment: "production"}) {
			t.Fatalf("Target = %+v, %v", tg, err)
		}
	}
	got := stderr.String()
	if !strings.HasPrefix(got, "using production: could not read the organization's default environment (") || strings.Count(got, "\n") != 1 {
		t.Errorf("stderr = %q, want the note once", got)
	}
}

// A command that changes something refuses the guess instead.
func TestTargetToChangeRefusesTheGuess(t *testing.T) {
	app := target(t, orgsDown(t))
	app.Global.Org, app.Global.Project = "acme", "clave"
	var stderr strings.Builder
	app.Err = &stderr
	tg, err := app.TargetToChange(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "could not determine the default environment of acme: ") || !strings.HasSuffix(err.Error(), "; pass --environment") {
		t.Fatalf("err = %v", err)
	}
	if tg.Environment != "" || stderr.Len() != 0 {
		t.Errorf("target %+v, stderr %q: no guess, no note", tg, stderr.String())
	}
	app.Global.Environment = "staging"
	if tg, err := app.TargetToChange(context.Background()); err != nil || tg.Environment != "staging" {
		t.Errorf("chosen: %+v, %v", tg, err)
	}
}

// Each source of the environment reaches Target, in order; the full order is
// pinned by deploy's TestTargetEnvironmentResolution.
func TestTargetFillsTheEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs" {
			w.Write([]byte(`[{"slug":"acme","default_environment":"production"}]`)) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	app := target(t, srv)
	app.Global.Org, app.Global.Project = "acme", "clave"
	env := func() string {
		t.Helper()
		tg, err := app.Target(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return tg.Environment
	}

	if got := env(); got != "production" {
		t.Errorf("nothing selected: %q", got)
	}
	app.Config.UseEnvironment("acme", "clave", "qa")
	if got := env(); got != "qa" {
		t.Errorf("saved choice: %q", got)
	}
	t.Setenv("GOSHIP_ENVIRONMENT", "dev")
	if got := env(); got != "dev" {
		t.Errorf("GOSHIP_ENVIRONMENT beats the saved choice: %q", got)
	}
	app.ProjectOrg, app.ProjectEnvironment = "other", "staging"
	if got := env(); got != "dev" {
		t.Errorf("another org's goship.yaml must not count: %q", got)
	}
	app.ProjectOrg = "acme"
	if got := env(); got != "staging" {
		t.Errorf("goship.yaml beats GOSHIP_ENVIRONMENT: %q", got)
	}
	app.Global.Environment = "preview"
	if got := env(); got != "preview" {
		t.Errorf("--environment beats the file: %q", got)
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
		case "/api/v1/orgs/menlo/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		default:
			for _, p := range holders {
				if r.URL.Path == "/api/v1/orgs/menlo/projects/"+p+"/environments/production/apps/web" {
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
	if err != nil || tg != (Target{Org: "menlo", Project: "mova", Environment: "production"}) {
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
	if err != nil || tg != (Target{Org: "menlo", Project: "mova", Environment: "production"}) {
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
	if gotPath != "/api/v1/orgs/acme/projects/mova/environments/production/apps" {
		t.Errorf("path = %q", gotPath)
	}
}

// twoOrgs serves two orgs with projects clave and mova each; web lives in the
// default environment of the "org/project" pairs named by holders, and every
// request is recorded.
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
		case "/api/v1/orgs/acme/environments", "/api/v1/orgs/globex/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
			return
		}
		for _, h := range holders {
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/"+strings.Replace(h, "/", "/projects/", 1)+"/environments/production/apps/web" {
				w.Write([]byte(`{"name":"web","platform":"go"}`)) //nolint:errcheck
				return
			}
			if r.URL.Path == "/api/v1/orgs/"+strings.Replace(h, "/", "/projects/", 1)+"/environments/production/apps" {
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
	if err != nil || !strings.Contains(out, "web") || !slices.Contains(touched, "GET /api/v1/orgs/globex/projects/mova/environments/production/apps/web") {
		t.Fatalf("out %q err %v touched %q", out, err, touched)
	}
}

// An environment that was asked for is part of what was not found.
func TestTargetForAppNamesTheEnvironmentItSearchedIn(t *testing.T) {
	var touched []string
	twoOrgs(t, &touched, "acme/clave")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--org", "acme"}, `app "web" not found in environment qa of any project of acme that you can reach`},
		{[]string{"--project", "mova"}, `app "web" not found in environment qa of any project named "mova" that you can reach`},
		{nil, `app "web" not found in environment qa of any of your projects`},
	} {
		args := append([]string{"app", "info", "web", "--environment", "qa"}, tc.args...)
		if _, err := run(t, "", args...); err == nil || err.Error() != tc.want {
			t.Errorf("%v: err = %v, want %q", tc.args, err, tc.want)
		}
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
	if strings.Contains(out, "clave") || !strings.Contains(out, "globex/mova/production") {
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

// A saved or GOSHIP_ENVIRONMENT selection the organization no longer has earns
// an explanation, not the API's bare 404.
func TestStaleEnvironmentSelectionIsExplained(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/environments":
			w.Write([]byte(twoEnvironments)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"clave"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	explained := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), `environment "gone" is not one of acme's environments; run goship environments and goship environment use <slug>`)
	}
	commands := [][]string{{"apps"}, {"app", "rm", "web", "--yes"}, {"app", "info", "web"}}

	// Saved by `goship environment use`.
	t.Setenv("GOSHIP_ENVIRONMENT", "")
	if err := (&config.Config{Environments: map[string]string{"acme/clave": "gone"}}).Save(); err != nil {
		t.Fatal(err)
	}
	for _, args := range commands {
		if _, err := run(t, "", args...); !explained(err) {
			t.Errorf("saved %v: err = %v", args, err)
		}
	}

	// From the process environment.
	if err := (&config.Config{}).Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSHIP_ENVIRONMENT", "gone")
	for _, args := range commands {
		if _, err := run(t, "", args...); !explained(err) {
			t.Errorf("GOSHIP_ENVIRONMENT %v: err = %v", args, err)
		}
	}

	// Said on the command line, it is the user's own words and the API's
	// answer stands.
	if _, err := run(t, "", "apps", "--environment", "gone"); err == nil || explained(err) {
		t.Errorf("--environment: err = %v", err)
	}

	// An environment the organization does have keeps its plain errors.
	t.Setenv("GOSHIP_ENVIRONMENT", "staging")
	if _, err := run(t, "", "app", "rm", "web", "--yes"); err == nil || strings.Contains(err.Error(), "is not one of") {
		t.Errorf("a listed environment: err = %v", err)
	}
}
