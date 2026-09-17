package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Commands resolve the org from config, so tests that exercise them need one.
func stubAPIWithOrg(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	stubAPI(t, h)
	t.Setenv("GOSHIP_ORG", "acme")
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
	if gotPath != "/api/v1/orgs/acme/apps" {
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
	if gotPath != "/api/v1/orgs/other/apps" {
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
			if want := "/api/v1/orgs/acme/apps/api/" + action; gotPath != want {
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

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "goship.yml"), []byte("org: from-project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if _, err := run(t, "", "apps"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/from-project/apps" {
		t.Errorf("path = %q, want the project's org", gotPath)
	}

	if _, err := run(t, "", "apps", "--org", "explicit"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/explicit/apps" {
		t.Errorf("path = %q, want --org to win", gotPath)
	}
}
