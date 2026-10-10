package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/deploy"
)

func initIn(t *testing.T, files ...string) (dir, out string, err error) {
	t.Helper()
	isolateConfig(t)
	dir = filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		write(t, filepath.Join(dir, f), "")
	}
	out, err = run(t, "", "init", dir)
	return dir, out, err
}

// Whatever init writes, deploy has to be able to read — they are the same file.
func TestInitOutputRoundTripsThroughLoadProject(t *testing.T) {
	dir, _, err := initIn(t, "go.mod")
	if err != nil {
		t.Fatal(err)
	}

	p, name, err := deploy.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if name != initFile {
		t.Errorf("read %q, want %q", name, initFile)
	}
	if p.App != "widget" || p.Platform != "go" {
		t.Errorf("got %+v", p)
	}
	if p.Plan != "" || len(p.Env) != 0 {
		t.Errorf("what cannot be discovered must stay empty, got plan=%q env=%v", p.Plan, p.Env)
	}
}

func TestInitLeavesThePlatformEmptyWhenItCannotTell(t *testing.T) {
	dir, out, err := initIn(t, "README.md")
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := deploy.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Platform != "" {
		t.Errorf("platform = %q, want it left for the user", p.Platform)
	}
	if p.App != "widget" {
		t.Errorf("app = %q, want the directory name even with no platform", p.App)
	}
	// The user has to learn what to put there.
	for _, want := range []string{"left empty", "go.mod", "nodejs"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should mention %q, got:\n%s", want, out)
		}
	}
}

// Overwriting a config someone wrote by hand would lose their plan and env.
func TestInitRefusesToClobber(t *testing.T) {
	dir, _, err := initIn(t, "go.mod")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, initFile), "app: handwritten\nplan: app-large\n")

	if _, err := run(t, "", "init", dir); err == nil {
		t.Fatal("expected init to refuse")
	}
	p, _, _ := deploy.LoadProject(dir)
	if p.Plan != "app-large" {
		t.Errorf("the existing file was modified: %+v", p)
	}

	if _, err := run(t, "", "init", dir, "--force"); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := deploy.LoadProject(dir); p.Plan != "" {
		t.Errorf("--force should have replaced it, got %+v", p)
	}
}

// Any of the accepted spellings counts as existing, or init would write a
// second file that loadProject then ignores in favour of the first.
func TestInitSeesEveryConfigSpelling(t *testing.T) {
	for _, name := range deploy.ProjectFiles {
		t.Run(name, func(t *testing.T) {
			isolateConfig(t)
			dir := filepath.Join(t.TempDir(), "widget")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(dir, name), "app: existing\n")

			if _, err := run(t, "", "init", dir); err == nil {
				t.Errorf("init overwrote or ignored %s", name)
			}
		})
	}
}

func TestAppNameFor(t *testing.T) {
	for _, tc := range []struct{ dir, want string }{
		{"widget", "widget"},
		{"My_Widget.API", "my-widget-api"},
		{"my  widget", "my-widget"},
		{"--weird--", "weird"},
		{"...", "app"},
		{"123-numeric", "app-123-numeric"},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			if got := appNameFor(filepath.Join(t.TempDir(), tc.dir)); got != tc.want {
				t.Errorf("appNameFor(%q) = %q, want %q", tc.dir, got, tc.want)
			}
		})
	}
}

// No platform marker but a container file: the build is the Dockerfile's job,
// and the config has to say so rather than leaving both blank.
func TestInitRecordsADockerfileBuild(t *testing.T) {
	dir, out, err := initIn(t, "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}

	p, _, err := deploy.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Platform != "" {
		t.Errorf("platform = %q, want it empty for a container build", p.Platform)
	}
	if p.Dockerfile != "Dockerfile" {
		t.Errorf("dockerfile = %q, want it recorded", p.Dockerfile)
	}
	if !strings.Contains(out, "Dockerfile") {
		t.Errorf("output should name the container file, got:\n%s", out)
	}
}

// A platform marker wins: a Go service shipping a Dockerfile is still a Go
// service unless the user says otherwise.
func TestInitPrefersThePlatformOverADockerfile(t *testing.T) {
	dir, _, err := initIn(t, "go.mod", "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}

	p, _, _ := deploy.LoadProject(dir)
	if p.Platform != "go" || p.Dockerfile != "" {
		t.Errorf("got platform=%q dockerfile=%q", p.Platform, p.Dockerfile)
	}
}

// The org is local state, so init can record it without a network call.
func TestInitRecordsTheSelectedOrg(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GOSHIP_ORG", "acme")
	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "")

	if _, err := run(t, "", "init", dir); err != nil {
		t.Fatal(err)
	}
	p, _, err := deploy.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Org != "acme" {
		t.Errorf("org = %q, want the selected one", p.Org)
	}
}

// Nothing selected means nothing to record: leave it for the user rather than
// guess, and say so.
func TestInitLeavesTheOrgEmptyWhenNoneIsSelected(t *testing.T) {
	dir, out, err := initIn(t, "go.mod")
	if err != nil {
		t.Fatal(err)
	}
	p, _, _ := deploy.LoadProject(dir)
	if p.Org != "" {
		t.Errorf("org = %q, want it empty", p.Org)
	}
	if !strings.Contains(out, "goship org use") {
		t.Errorf("output should say how to set one, got:\n%s", out)
	}
}

func initWithProjects(t *testing.T, projects, selected string) (*deploy.Project, string) {
	t.Helper()
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/orgs/acme/projects" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		w.Write([]byte(projects)) //nolint:errcheck
	})
	t.Setenv("GOSHIP_TOKEN", "gsp_test")
	t.Setenv("GOSHIP_ORG", "acme")
	t.Setenv("GOSHIP_PROJECT", selected)
	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "")

	out, err := run(t, "", "init", dir)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := deploy.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p, out
}

// An org with several projects gets the selected one pinned, so the repo
// always deploys to the same place.
func TestInitRecordsTheSelectedProjectWhenTheOrgHasSeveral(t *testing.T) {
	p, out := initWithProjects(t, `[{"slug":"clave"},{"slug":"mova"}]`, "mova")
	if p.ProjectSlug != "mova" || !strings.Contains(out, "project:  mova") {
		t.Errorf("project = %q, output:\n%s", p.ProjectSlug, out)
	}
}

func TestInitLeavesTheProjectEmptyWhenTheOrgHasSeveralAndNoneIsSelected(t *testing.T) {
	p, out := initWithProjects(t, `[{"slug":"clave"},{"slug":"mova"}]`, "")
	if p.ProjectSlug != "" || !strings.Contains(out, "goship project use") {
		t.Errorf("project = %q, output:\n%s", p.ProjectSlug, out)
	}
}

// One project is nothing to choose, so the file does not pin it.
func TestInitDoesNotPinTheOnlyProject(t *testing.T) {
	p, _ := initWithProjects(t, `[{"slug":"default"}]`, "default")
	if p.ProjectSlug != "" {
		t.Errorf("project = %q, want it left out", p.ProjectSlug)
	}
}

// A selection the account cannot reach (a stale config, a mistyped
// GOSHIP_PROJECT) must not end up in a file that is meant to be committed.
func TestInitDoesNotPinAProjectTheUserCannotReach(t *testing.T) {
	p, out := initWithProjects(t, `[{"slug":"clave"},{"slug":"mova"}]`, "gone")
	if p.ProjectSlug != "" || !strings.Contains(out, "goship project use") {
		t.Errorf("project = %q, output:\n%s", p.ProjectSlug, out)
	}

	p, out = initWithProjects(t, `[]`, "gone")
	if p.ProjectSlug != "" || !strings.Contains(out, "cannot reach any project") {
		t.Errorf("project = %q, output:\n%s", p.ProjectSlug, out)
	}
}
