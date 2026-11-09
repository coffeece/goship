package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Commands resolve the org from config, so tests that exercise them need one.
func stubAPIWithOrg(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	stubAPI(t, h)
	t.Setenv("GOSHIP_ORG", "acme")
	t.Setenv("GOSHIP_PROJECT", "atlas")
	t.Setenv("GOSHIP_ENVIRONMENT", "production")
}

func TestAppsListsUnderTheCurrentOrg(t *testing.T) {
	var gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"name":"api","status":"running","plan_name":"Small"}]`)) //nolint:errcheck
	})

	out, err := run(t, "", "apps")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/projects/atlas/environments/production/apps" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(out, "api") || !strings.Contains(out, "Small") {
		t.Errorf("output = %q", out)
	}
}

func TestOrgFlagOverridesTheStoredOrg(t *testing.T) {
	var gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[]`)) //nolint:errcheck
	})

	if _, err := run(t, "", "apps", "--org", "other"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/other/projects/atlas/environments/production/apps" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestAppCreateSendsThePlatform(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)             //nolint:errcheck
		w.Write([]byte(`{"name":"api","platform":"go"}`)) //nolint:errcheck
	})

	if _, err := run(t, "", "app", "create", "api", "--platform", "go"); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "api" || body["platform"] != "go" {
		t.Errorf("body = %v", body)
	}
}

// syncBuilder is a stdout the portal stub can read while the command runs.
type syncBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuilder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuilder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// `app create` names the org, project and environment before it asks for
// anything, as `deploy` does, so a wrong environment is seen before it is
// acted on.
func TestAppCreatePrintsTheTargetFirst(t *testing.T) {
	var out syncBuilder
	var printedBeforeTheRequest string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			printedBeforeTheRequest = out.String()
		}
		w.Write([]byte(`{"name":"api","platform":"go"}`)) //nolint:errcheck
	})

	root := NewRoot("test")
	root.SetArgs([]string{"app", "create", "api", "--platform", "go", "--environment", "staging"})
	root.SetOut(&out)
	root.SetErr(&strings.Builder{})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if want := "Creating api in acme/atlas/staging\n"; printedBeforeTheRequest != want {
		t.Errorf("before the request stdout = %q, want %q", printedBeforeTheRequest, want)
	}
	if !strings.HasPrefix(out.String(), "Creating api in acme/atlas/staging\n") || strings.Count(out.String(), "Creating") != 1 {
		t.Errorf("stdout = %q", out.String())
	}
}

// -o json stays one JSON document: the line is for people.
func TestAppCreateJSONHasNoCreatingLine(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"api","platform":"go"}`)) //nolint:errcheck
	})
	out, err := run(t, "", "app", "create", "api", "--platform", "go", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["name"] != "api" {
		t.Errorf("stdout is not the created app: %v\n%s", err, out)
	}
}

// Nothing selected: the app is searched in every environment of every
// project, and found in the one that holds it.
func TestAppInfoFindsTheAppInAnotherEnvironment(t *testing.T) {
	// holders are the "project/environment" pairs that hold web.
	serve := func(t *testing.T, touched *[]string, holders ...string) {
		stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
			*touched = append(*touched, r.URL.Path)
			switch r.URL.Path {
			case "/api/v1/orgs":
				w.Write([]byte(`[{"slug":"acme","default_environment":"production"}]`)) //nolint:errcheck
			case "/api/v1/orgs/acme/projects":
				w.Write([]byte(`[{"slug":"atlas"},{"slug":"comet"}]`)) //nolint:errcheck
			case "/api/v1/orgs/acme/environments":
				w.Write([]byte(`[{"slug":"production"},{"slug":"staging"}]`)) //nolint:errcheck
			default:
				for _, h := range holders {
					project, env, _ := strings.Cut(h, "/")
					if r.URL.Path == "/api/v1/orgs/acme/projects/"+project+"/environments/"+env+"/apps/web" {
						w.Write([]byte(`{"name":"web","platform":"go","plan_name":"Small"}`)) //nolint:errcheck
						return
					}
				}
				http.NotFound(w, r)
			}
		})
		t.Setenv("GOSHIP_ORG", "")
		t.Setenv("GOSHIP_PROJECT", "")
		t.Setenv("GOSHIP_ENVIRONMENT", "")
	}

	t.Run("only in staging", func(t *testing.T) {
		var touched []string
		serve(t, &touched, "comet/staging")
		out, err := run(t, "", "app", "info", "web")
		if err != nil || !strings.Contains(out, "Small") {
			t.Fatalf("out %q err %v", out, err)
		}
		// info then reads it where it was found, not in the default.
		if last := touched[len(touched)-1]; last != "/api/v1/orgs/acme/projects/comet/environments/staging/apps/web" {
			t.Errorf("last request = %s", last)
		}
	})

	t.Run("in staging and production", func(t *testing.T) {
		var touched []string
		serve(t, &touched, "atlas/staging", "atlas/production")
		_, err := run(t, "", "app", "info", "web")
		if err == nil {
			t.Fatal("two environments hold web: info must not pick one")
		}
		for _, want := range []string{`"web" exists in: acme/atlas/production, acme/atlas/staging`, "--org, --project and --environment"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, missing %q", err, want)
			}
		}
	})

	t.Run("an environment that was asked for is the only one searched", func(t *testing.T) {
		var touched []string
		serve(t, &touched, "comet/staging", "atlas/production")
		out, err := run(t, "", "app", "info", "web", "--environment", "production")
		if err != nil || !strings.Contains(out, "Small") {
			t.Fatalf("out %q err %v", out, err)
		}
		for _, p := range touched {
			if strings.Contains(p, "/environments/staging") || p == "/api/v1/orgs/acme/environments" {
				t.Errorf("looked beyond production: %s", p)
			}
		}
	})
}

func TestAppCreateRequiresAPlatform(t *testing.T) {
	stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) {
		t.Error("should not have called the API")
	})
	if _, err := run(t, "", "app", "create", "api"); err == nil {
		t.Fatal("expected --platform to be required")
	}
}

// Deleting an app destroys everything it runs, so it must not happen because
// the user was piping input at it.
func TestAppRemoveNeedsConfirmation(t *testing.T) {
	called := false
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			called = true
		}
	})

	if _, err := run(t, "n\n", "app", "rm", "api"); err == nil {
		t.Fatal("expected the command to abort")
	}
	if called {
		t.Error("app was deleted without consent")
	}

	if _, err := run(t, "", "app", "rm", "api", "--yes"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("--yes should have gone through")
	}
}

func TestLifecycleCommandsHitTheirEndpoint(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		t.Run(action, func(t *testing.T) {
			var gotPath, gotMethod string
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
			})

			if _, err := run(t, "", "app", action, "api"); err != nil {
				t.Fatal(err)
			}
			if want := "/api/v1/orgs/acme/projects/atlas/environments/production/apps/api/" + action; gotPath != want {
				t.Errorf("path = %q, want %q", gotPath, want)
			}
			if gotMethod != http.MethodPost {
				t.Errorf("method = %q", gotMethod)
			}
		})
	}
}

func TestEnvSetParsesKeyValuePairs(t *testing.T) {
	var body struct {
		Envs []struct {
			Name, Value string
		} `json:"envs"`
		NoRestart bool `json:"no_restart"`
	}
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	})

	if _, err := run(t, "", "env", "set", "-a", "api", "PORT=8080", "URL=https://a?b=c", "--no-restart"); err != nil {
		t.Fatal(err)
	}
	if len(body.Envs) != 2 {
		t.Fatalf("envs = %v", body.Envs)
	}
	if body.Envs[0].Name != "PORT" || body.Envs[0].Value != "8080" {
		t.Errorf("first = %v", body.Envs[0])
	}
	// Only the first = separates key from value; the rest belongs to the value.
	if body.Envs[1].Value != "https://a?b=c" {
		t.Errorf("second = %v", body.Envs[1])
	}
	if !body.NoRestart {
		t.Error("--no-restart was not forwarded")
	}
}

func TestEnvSetRejectsMalformedPairs(t *testing.T) {
	stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) {
		t.Error("should not have called the API")
	})
	if _, err := run(t, "", "env", "set", "-a", "api", "NOPE"); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestEnvRequiresAnApp(t *testing.T) {
	stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) {
		t.Error("should not have called the API")
	})
	if _, err := run(t, "", "env", "list"); err == nil {
		t.Fatal("expected --app to be required")
	}
}

// A repo that belongs to one organization should deploy there whatever
// `goship org use` was last pointed at — the project file is the more specific
// statement. The flag still wins over both.
func TestProjectFileOrgBeatsTheSelectedOrg(t *testing.T) {
	var gotPath string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	t.Setenv("GOSHIP_ORG", "selected")
	t.Setenv("GOSHIP_PROJECT", "atlas")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "goship.yml"), []byte("org: from-project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if _, err := run(t, "", "apps"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/from-project/projects/atlas/environments/development/apps" {
		t.Errorf("path = %q, want the project's org", gotPath)
	}

	if _, err := run(t, "", "apps", "--org", "explicit"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/explicit/projects/atlas/environments/development/apps" {
		t.Errorf("path = %q, want --org to win", gotPath)
	}
}

// "apps" alone shows only the current project. --all spans every project in
// every org the user belongs to, grouped as org/project/environment — the
// fuller view that matches what people expect after seeing their apps
// scattered across orgs.
func TestAppsAllSpansEveryProject(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects", "/api/v1/orgs/globex/projects":
			w.Write([]byte(`[{"slug":"default"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/environments", "/api/v1/orgs/globex/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects/default/environments/production/apps":
			w.Write([]byte(`[{"name":"arcade","status":"stopped","plan_name":"Medium"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/default/environments/production/apps":
			w.Write([]byte(`[{"name":"globex-api","status":"running","plan_name":"Small"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	out, err := run(t, "", "apps", "--all")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"games", "arcade", "globex", "globex-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("--all output missing %q:\n%s", want, out)
		}
	}
	// Grouped by org/project/environment: each header precedes its own apps.
	if got, want := sectionHeaders(out), []string{"games/default/production", "globex/default/production"}; !slices.Equal(got, want) {
		t.Errorf("sections = %q, want %q", got, want)
	}
	if strings.Index(out, "games/default/production") > strings.Index(out, "arcade") {
		t.Errorf("expected the org header before its apps:\n%s", out)
	}
}

func TestAppsAllJSONKeysByOrgProjectAndEnvironment(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects":
			w.Write([]byte(`[{"slug":"default"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects/default/environments/production/apps":
			w.Write([]byte(`[{"name":"arcade"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	out, err := run(t, "", "apps", "--all", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string][]map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got["games/default/production"]) != 1 || got["games/default/production"][0]["name"] != "arcade" {
		t.Errorf("got %v", got)
	}
}

// A listing with no org selected is not an error: it falls back to every org,
// the same view as --all. Demanding `org use` first for a read is friction.
func TestAppsWithNoOrgListsEverything(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects", "/api/v1/orgs/globex/projects":
			w.Write([]byte(`[{"slug":"default"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/environments", "/api/v1/orgs/globex/environments":
			w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/projects/default/environments/production/apps":
			w.Write([]byte(`[{"name":"arcade"}]`)) //nolint:errcheck
		case "/api/v1/orgs/globex/projects/default/environments/production/apps":
			w.Write([]byte(`[{"name":"globex-api"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	out, err := run(t, "", "apps")
	if err != nil {
		t.Fatalf("apps with no org must not error: %v", err)
	}
	for _, want := range []string{"games", "arcade", "globex", "globex-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestOrgsLists(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs" {
			w.Write([]byte(`[{"id":"1","name":"Games","slug":"games"}]`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	out, err := run(t, "", "orgs")
	if err != nil || !strings.Contains(out, "games") {
		t.Fatalf("orgs = %q, %v", out, err)
	}
}

func TestAppInfoShowsTheAccessCheckOfPrivateApps(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"ok", `{"name":"api","visibility":"private","access_check":{"checked_at":"2026-10-20T10:00:00Z","ok":true}}`, "Access check: protected (checked 2026-10-20T10:00:00Z)"},
		{"failed", `{"name":"api","visibility":"private","access_check":{"checked_at":"2026-10-20T10:00:00Z","ok":false,"host":"api.example.com"}}`, "Access check: NOT confirmed on api.example.com"},
		{"login check unreachable", `{"name":"api","visibility":"private","access_check":{"checked_at":"2026-10-20T10:00:00Z","ok":false,"host":"api.example.com","reason":"login_check_unreachable"}}`, "Access check: REFUSING visitors on api.example.com: the server can't reach GoShip's login check on port 8443; allow outbound TCP 8443 to api.goship.sh (checked 2026-10-20T10:00:00Z)"},
		{"pending", `{"name":"api","visibility":"private"}`, "Access check: pending"},
		{"public", `{"name":"api","visibility":"public"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tc.body)) //nolint:errcheck
			})
			out, err := run(t, "", "app", "info", "api")
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if strings.Contains(out, "Access check") {
					t.Errorf("public app shows a check: %q", out)
				}
				return
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}
