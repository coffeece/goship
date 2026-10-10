package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
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
			w.Write([]byte(`[{"slug":"initech","name":"Initech"}]`)) //nolint:errcheck
		case "/api/v1/orgs/initech/projects":
			w.Write([]byte(`[{"slug":"default","name":"Default","is_default":true,"role":"admin"}]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	app := target(t, srv)
	tg, err := app.Target(context.Background())
	if err != nil || tg != (Target{Org: "initech", Project: "default"}) {
		t.Fatalf("Target = %+v, %v", tg, err)
	}
}

func TestTargetDemandsAChoiceBetweenProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"initech","name":"Initech"}]`)) //nolint:errcheck
		case "/api/v1/orgs/initech/projects":
			w.Write([]byte(`[{"slug":"atlas","role":"developer"},{"slug":"comet","role":"developer"}]`)) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	app := target(t, srv)
	if _, err := app.Target(context.Background()); !errors.Is(err, errNoProject) {
		t.Fatalf("err = %v, want errNoProject", err)
	}
	app.Global.Project = "comet"
	if tg, err := app.Target(context.Background()); err != nil || tg.Project != "comet" {
		t.Fatalf("--project: %+v, %v", tg, err)
	}
	app.Global.Project = ""
	app.ProjectProject = "atlas"
	if tg, _ := app.Target(context.Background()); tg.Project != "atlas" {
		t.Fatalf("goship.yaml project: %+v", tg)
	}
}

// The project a goship.yaml pins belongs to the org it pins; naming another
// org on the command line must not carry the project along.
func TestProjectFileProjectIsIgnoredForAnotherOrg(t *testing.T) {
	app := &App{Config: &config.Config{}, ProjectOrg: "initech", ProjectProject: "atlas"}
	t.Setenv("GOSHIP_PROJECT", "")
	if got := app.selectedProject("initech"); got != "atlas" {
		t.Errorf("selectedProject(initech) = %q", got)
	}
	if got := app.selectedProject("other"); got != "" {
		t.Errorf("selectedProject(other) = %q, want none", got)
	}
}

func TestSelectedProjectOrder(t *testing.T) {
	app := &App{Config: &config.Config{Projects: map[string]string{"initech": "from-config"}}}
	t.Setenv("GOSHIP_PROJECT", "")
	if got := app.selectedProject("initech"); got != "from-config" {
		t.Errorf("config: %q", got)
	}
	t.Setenv("GOSHIP_PROJECT", "from-env")
	if got := app.selectedProject("initech"); got != "from-env" {
		t.Errorf("env beats config: %q", got)
	}
	app.ProjectProject = "from-file"
	if got := app.selectedProject("initech"); got != "from-file" {
		t.Errorf("goship.yaml beats env: %q", got)
	}
	app.Global.Project = "from-flag"
	if got := app.selectedProject("initech"); got != "from-flag" {
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
			w.Write([]byte(`[{"slug":"initech"}]`)) //nolint:errcheck
		case "/api/v1/orgs/initech/projects":
			w.Write([]byte(`[{"slug":"atlas"},{"slug":"comet"}]`)) //nolint:errcheck
		default:
			for _, p := range holders {
				if r.URL.Path == "/api/v1/orgs/initech/projects/"+p+"/apps/web" {
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
	app := target(t, twoProjects(t, "comet"))
	tg, err := app.TargetForApp(context.Background(), "web")
	if err != nil || tg != (Target{Org: "initech", Project: "comet"}) {
		t.Fatalf("TargetForApp = %+v, %v", tg, err)
	}
}

func TestTargetForAppRefusesToGuessBetweenProjects(t *testing.T) {
	app := target(t, twoProjects(t, "atlas", "comet"))
	_, err := app.TargetForApp(context.Background(), "web")
	if err == nil || !strings.Contains(err.Error(), "initech/atlas") || !strings.Contains(err.Error(), "initech/comet") {
		t.Fatalf("err = %v, want both org/project pairs", err)
	}
}

func TestTargetForAppUsesTheSelectionWithoutSearching(t *testing.T) {
	app := target(t, twoProjects(t, "atlas", "comet"))
	app.Global.Org, app.Global.Project = "initech", "comet"
	tg, err := app.TargetForApp(context.Background(), "web")
	if err != nil || tg != (Target{Org: "initech", Project: "comet"}) {
		t.Fatalf("TargetForApp = %+v, %v", tg, err)
	}
}

func TestProjectFlagSelectsTheProjectPath(t *testing.T) {
	var gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	if _, err := run(t, "", "apps", "--project", "comet"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/projects/comet/apps" {
		t.Errorf("path = %q", gotPath)
	}
}
