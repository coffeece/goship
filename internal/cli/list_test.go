package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Plural nouns list, and each lists under the current org like `apps` does.
func TestPluralListersUseTheCurrentOrg(t *testing.T) {
	for _, tc := range []struct{ cmd, path string }{
		{"dbs", "/api/v1/orgs/acme/databases"},
		{"domains", "/api/v1/orgs/acme/domains"},
		{"volumes", "/api/v1/orgs/acme/volumes"},
		{"nodes", "/api/v1/orgs/acme/nodes"},
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

func TestDBsAllSpansEveryOrg(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/databases":
			w.Write([]byte(`[{"name":"scores"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/databases":
			w.Write([]byte(`[{"name":"mail"}]`)) //nolint:errcheck
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
	if len(got["games"]) != 1 || got["games"][0]["name"] != "scores" ||
		len(got["globex"]) != 1 || got["globex"][0]["name"] != "mail" {
		t.Errorf("got %v", got)
	}
}

func TestDBsWithNoOrgListsEverything(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/databases":
			w.Write([]byte(`[{"name":"scores"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/databases":
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
