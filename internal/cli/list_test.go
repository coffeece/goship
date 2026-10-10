package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

// Plural nouns list, and each lists under the current org like `apps` does.
func TestPluralListersUseTheCurrentOrg(t *testing.T) {
	for _, tc := range []struct{ cmd, path string }{
		{"dbs", "/api/v1/orgs/acme/projects/clave/databases"},
		{"domains", "/api/v1/orgs/acme/domains"},
		{"volumes", "/api/v1/orgs/acme/projects/clave/volumes"},
		{"nodes", "/api/v1/orgs/acme/projects/clave/nodes"},
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

func TestDBsAllSpansEveryProject(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects":
			w.Write([]byte(`[{"slug":"default"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects":
			w.Write([]byte(`[{"slug":"default"},{"slug":"labs"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects/default/databases":
			w.Write([]byte(`[{"name":"scores"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/default/databases":
			w.Write([]byte(`[{"name":"mail"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/labs/databases":
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
	if len(got) != 3 || got["games/default"][0]["name"] != "scores" ||
		got["globex/default"][0]["name"] != "mail" || got["globex/labs"][0]["name"] != "scratch" {
		t.Errorf("got %v", got)
	}

	out, err = run(t, "", "dbs", "--all")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"games/default", "globex/default", "globex/labs", "scratch"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing section %q:\n%s", want, out)
		}
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
		case "/api/v1/orgs/games/projects/default/databases":
			w.Write([]byte(`[{"name":"scores"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/default/databases":
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
	for _, want := range []string{"games", "scores", "globex", "mail"} {
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
	if gotPath != "/api/v1/orgs/acme/projects/default/apps" {
		t.Errorf("path = %q", gotPath)
	}
}

// Scripts written against `<noun> list` keep working after the move.
func TestDeprecatedListStillLists(t *testing.T) {
	for _, args := range [][]string{
		{"db", "list"}, {"domain", "list"}, {"volume", "list"}, {"node", "list"}, {"token", "ls"},
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

// --org narrows an unresolved project to that org's projects, not to every org.
func TestListerWithOrgFlagAndNoProjectListsOnlyThatOrg(t *testing.T) {
	var touched []string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		touched = append(touched, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"clave"},{"slug":"mova"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/clave/apps":
			w.Write([]byte(`[{"name":"web"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/mova/apps":
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
	if len(got) != 2 || got["acme/clave"][0]["name"] != "web" || got["acme/mova"][0]["name"] != "api" {
		t.Errorf("got %v", got)
	}
	if slices.Contains(touched, "/api/v1/orgs") {
		t.Errorf("other orgs were listed: %q", touched)
	}
}
