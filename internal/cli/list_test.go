package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

// sectionHeaders are the titles of the sections a table listing printed: the
// first line of each blank-line-separated block.
func sectionHeaders(out string) []string {
	var heads []string
	for block := range strings.SplitSeq(strings.TrimSpace(out), "\n\n") {
		heads = append(heads, strings.SplitN(block, "\n", 2)[0])
	}
	return heads
}

// Plural nouns list, and each lists under the current org like `apps` does.
func TestPluralListersUseTheCurrentOrg(t *testing.T) {
	for _, tc := range []struct{ cmd, path string }{
		{"dbs", "/api/v1/orgs/acme/projects/atlas/environments/production/databases"},
		{"domains", "/api/v1/orgs/acme/domains"},
		{"volumes", "/api/v1/orgs/acme/projects/atlas/environments/production/volumes"},
		{"clusters", "/api/v1/orgs/acme/clusters"},
		{"environments", "/api/v1/orgs/acme/environments"},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			var gotPath string
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Write([]byte(`[]`)) //nolint:errcheck
			})
			if _, err := run(t, "", tc.cmd); err != nil {
				t.Fatal(err)
			}
			if gotPath != tc.path {
				t.Errorf("path = %q, want %q", gotPath, tc.path)
			}
		})
	}
}

// --all walks organization, project, environment, and each stop is a section
// headed org/project/environment. An environment whose listing answers 403 is
// skipped with a note, and an empty one still prints its header, as an empty
// project always has.
func TestAppsAllSpansEnvironments(t *testing.T) {
	var touched []string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		touched = append(touched, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"acme","default_environment":"production"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"atlas"},{"slug":"comet"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/environments":
			w.Write([]byte(`[{"slug":"production"},{"slug":"staging"},{"slug":"restricted"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/atlas/environments/production/apps":
			w.Write([]byte(`[{"name":"web"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/atlas/environments/staging/apps":
			w.Write([]byte(`[{"name":"web-preview"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/comet/environments/production/apps":
			w.Write([]byte(`[{"name":"api"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/comet/environments/staging/apps":
			w.Write([]byte(`[]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/atlas/environments/restricted/apps",
			"/api/v1/orgs/acme/projects/comet/environments/restricted/apps":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"not for you"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	t.Setenv("GOSHIP_ORG", "")
	t.Setenv("GOSHIP_PROJECT", "")
	t.Setenv("GOSHIP_ENVIRONMENT", "")

	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), "apps", "--all")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"acme/atlas/production", "acme/atlas/staging", "acme/comet/production", "acme/comet/staging"}
	if got := sectionHeaders(out); !slices.Equal(got, want) {
		t.Errorf("sections = %q, want %q\n%s", got, want, out)
	}
	for _, want := range []string{"web-preview", "api", "No results."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	for _, skipped := range []string{"acme/atlas/restricted", "acme/comet/restricted"} {
		if strings.Contains(out, skipped) {
			t.Errorf("%s is forbidden and must not be a section:\n%s", skipped, out)
		}
		if !strings.Contains(stderr, "Skipping "+skipped+": your role there cannot see this.") {
			t.Errorf("stderr should say %s was skipped: %q", skipped, stderr)
		}
	}
	// The environments are asked once for the organization, not per project.
	if n := slices.Index(touched, "/api/v1/orgs/acme/environments"); n < 0 || slices.Contains(touched[n+1:], "/api/v1/orgs/acme/environments") {
		t.Errorf("environments asked more than once: %q", touched)
	}

	out, _, err = runWithStderr(t, context.Background(), strings.NewReader(""), "apps", "--all", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 4 || got["acme/atlas/production"][0]["name"] != "web" || got["acme/atlas/staging"][0]["name"] != "web-preview" ||
		got["acme/comet/production"][0]["name"] != "api" || len(got["acme/comet/staging"]) != 0 {
		t.Errorf("got %v", got)
	}
}

// An environment that was chosen narrows every project of an --all listing to
// it, and nothing needs the list of environments.
func TestAppsAllHonoursAChosenEnvironment(t *testing.T) {
	var touched []string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		touched = append(touched, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"acme"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"atlas"},{"slug":"comet"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/atlas/environments/staging/apps",
			"/api/v1/orgs/acme/projects/comet/environments/staging/apps":
			w.Write([]byte(`[{"name":"web"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	t.Setenv("GOSHIP_ORG", "")
	t.Setenv("GOSHIP_PROJECT", "")
	t.Setenv("GOSHIP_ENVIRONMENT", "")

	out, err := run(t, "", "apps", "-A", "--environment", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sectionHeaders(out), []string{"acme/atlas/staging", "acme/comet/staging"}; !slices.Equal(got, want) {
		t.Errorf("sections = %q, want %q", got, want)
	}
	if slices.Contains(touched, "/api/v1/orgs/acme/environments") {
		t.Errorf("a chosen environment needs no list of environments: %q", touched)
	}
}

func TestDBsAllSpansEveryProject(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects":
			w.Write([]byte(`[{"slug":"default"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects":
			w.Write([]byte(`[{"slug":"default"},{"slug":"labs"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/environments", "/api/v1/orgs/globex/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects/default/environments/production/databases":
			w.Write([]byte(`[{"name":"scores"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/default/environments/production/databases":
			w.Write([]byte(`[{"name":"mail"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/labs/environments/production/databases":
			w.Write([]byte(`[{"name":"scratch"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "", "dbs", "--all", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 3 || got["games/default/production"][0]["name"] != "scores" ||
		got["globex/default/production"][0]["name"] != "mail" || got["globex/labs/production"][0]["name"] != "scratch" {
		t.Errorf("got %v", got)
	}

	out, err = run(t, "", "dbs", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sectionHeaders(out), []string{"games/default/production", "globex/default/production", "globex/labs/production"}; !slices.Equal(got, want) {
		t.Errorf("sections = %q, want %q", got, want)
	}
	if !strings.Contains(out, "scratch") {
		t.Errorf("missing scratch:\n%s", out)
	}
}

// With several projects and none selected, a read lists them all instead of
// demanding `project use` first.
func TestDBsWithNoProjectListsEverything(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects", "/api/v1/orgs/globex/projects":
			w.Write([]byte(`[{"slug":"default"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/environments", "/api/v1/orgs/globex/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects/default/environments/production/databases":
			w.Write([]byte(`[{"name":"scores"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/default/environments/production/databases":
			w.Write([]byte(`[{"name":"mail"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	out, err := run(t, "", "dbs")
	if err != nil {
		t.Fatalf("dbs with no org must not error: %v", err)
	}
	for _, want := range []string{"games/default/production", "scores", "globex/default/production", "mail"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestListerUsesTheOnlyProjectWhenNoneIsSelected(t *testing.T) {
	var gotPath string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"default"}]`)) //nolint:errcheck
		default:
			gotPath = r.URL.Path
			w.Write([]byte(`[]`)) //nolint:errcheck
		}
	})
	t.Setenv("GOSHIP_ORG", "acme")

	if _, err := run(t, "", "apps"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/projects/default/environments/production/apps" {
		t.Errorf("path = %q", gotPath)
	}
}

// Scripts written against `<noun> list` keep working after the move.
func TestDeprecatedListStillLists(t *testing.T) {
	for _, args := range [][]string{
		{"db", "list"}, {"domain", "list"}, {"volume", "list"}, {"token", "ls"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			called := false
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.Write([]byte(`[]`)) //nolint:errcheck
			})
			if _, err := run(t, "", args...); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Error("no request made")
			}
		})
	}
}

func TestTokensLists(t *testing.T) {
	var gotPath string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"id":"t1","name":"ci"}]`)) //nolint:errcheck
	})
	out, err := run(t, "", "tokens")
	if err != nil || !strings.Contains(out, "ci") {
		t.Fatalf("tokens = %q, %v", out, err)
	}
	if gotPath != "/api/v1/auth/tokens" {
		t.Errorf("path = %q", gotPath)
	}
}

// --org narrows an unresolved project to that org's projects, not to every
// org.
func TestListerWithOrgFlagAndNoProjectListsOnlyThatOrg(t *testing.T) {
	var touched []string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		touched = append(touched, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"slug":"acme","default_environment":"production"},{"slug":"other"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"atlas"},{"slug":"comet"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/atlas/environments/production/apps":
			w.Write([]byte(`[{"name":"web"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/comet/environments/production/apps":
			w.Write([]byte(`[{"name":"api"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	t.Setenv("GOSHIP_ORG", "")
	t.Setenv("GOSHIP_PROJECT", "")

	out, err := run(t, "", "apps", "--org", "acme", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 2 || got["acme/atlas/production"][0]["name"] != "web" || got["acme/comet/production"][0]["name"] != "api" {
		t.Errorf("got %v", got)
	}
	if slices.ContainsFunc(touched, func(p string) bool { return strings.HasPrefix(p, "/api/v1/orgs/other") }) {
		t.Errorf("other orgs were listed: %q", touched)
	}
}
