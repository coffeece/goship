package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/config"
)

func stubAPI(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	isolateConfig(t)
	t.Setenv("GOSHIP_API", srv.URL)
	t.Setenv("GOSHIP_TOKEN", "")
}

func run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	root := NewRoot("test")
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&strings.Builder{})
	root.SetIn(strings.NewReader(stdin))

	err := root.Execute()
	return out.String(), err
}

func TestLoginStoresTheToken(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if body["email"] != "gui@example.com" || body["password"] != "hunter2" {
			t.Errorf("credentials not forwarded: %v", body)
		}
		w.Write([]byte(`{"token":"jwt-123"}`)) //nolint:errcheck
	})

	if _, err := run(t, "hunter2\n", "login", "--email", "gui@example.com"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "jwt-123" {
		t.Errorf("token = %q, want it persisted", cfg.Token)
	}
}

// --email takes the password path. The password is read from the same buffered
// reader every prompt uses, so piping it in has to work.
func TestLoginWithEmailReadsThePipedPassword(t *testing.T) {
	var got map[string]string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got) //nolint:errcheck
		w.Write([]byte(`{"token":"t"}`))     //nolint:errcheck
	})

	if _, err := run(t, "hunter2\n", "login", "--email", "gui@example.com"); err != nil {
		t.Fatal(err)
	}
	if got["email"] != "gui@example.com" || got["password"] != "hunter2" {
		t.Errorf("got %v", got)
	}
}

func TestLoginRefusesWhenTheTokenComesFromTheEnvironment(t *testing.T) {
	stubAPI(t, func(http.ResponseWriter, *http.Request) {
		t.Error("should not have called the API")
	})
	t.Setenv("GOSHIP_TOKEN", "from-env")

	_, err := run(t, "", "login", "--email", "a@b.c")
	if err == nil || !strings.Contains(err.Error(), "GOSHIP_TOKEN") {
		t.Fatalf("got %v", err)
	}
}

func TestLogoutClearsTheToken(t *testing.T) {
	stubAPI(t, func(http.ResponseWriter, *http.Request) {})
	if err := (&config.Config{Token: "jwt"}).Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, "", "logout"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if cfg.Token != "" {
		t.Errorf("token = %q, want it cleared", cfg.Token)
	}
}

func TestWhoamiRendersJSONOnDemand(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"id":"u1","email":"gui@example.com","groups":["acme"]}`)) //nolint:errcheck
	})

	out, err := run(t, "", "whoami", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var me map[string]any
	if err := json.Unmarshal([]byte(out), &me); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if me["email"] != "gui@example.com" {
		t.Errorf("got %v", me)
	}
}

func TestOrgUseRejectsAnOrgYouAreNotIn(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"id":"1","name":"Acme","slug":"acme"}]`)) //nolint:errcheck
	})

	if _, err := run(t, "", "org", "use", "other"); err == nil {
		t.Fatal("expected a membership error")
	}

	if _, err := run(t, "", "org", "use", "acme"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if cfg.Org != "acme" {
		t.Errorf("org = %q, want it persisted", cfg.Org)
	}
}

func TestCommandsFailWithoutAnOrg(t *testing.T) {
	isolateConfig(t)
	os.Unsetenv("GOSHIP_ORG")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.OrgOrError(""); err == nil {
		t.Fatal("expected an error naming `goship org use`")
	}
}

// An account that belongs to exactly one organization has nothing to choose,
// so requiring `org use` before the first command is pure friction.
func TestSingleOrgAccountsNeedNoSelection(t *testing.T) {
	var gotPath string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs" {
			w.Write([]byte(`[{"id":"1","slug":"onlyone"}]`)) //nolint:errcheck
			return
		}
		gotPath = r.URL.Path
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	os.Unsetenv("GOSHIP_ORG")

	if _, err := run(t, "", "apps"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/onlyone/apps" {
		t.Errorf("path = %q, want the sole org selected automatically", gotPath)
	}
}

// With several orgs and none chosen, a command that ACTS on one org still has
// to ask — guessing would act on the wrong one. (A listing like `apps` instead
// falls back to all; see TestAppsWithNoOrgListsEverything.)
func TestMultipleOrgsStillRequireAChoice(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"1","slug":"a"},{"id":"2","slug":"b"}]`)) //nolint:errcheck
	})
	os.Unsetenv("GOSHIP_ORG")

	// A mutating command must not guess an org — it still demands a choice.
	_, err := run(t, "", "app", "stop", "some-app")
	if err == nil || !strings.Contains(err.Error(), "goship org use") {
		t.Fatalf("got %v", err)
	}
}

// With no org selected, `app info` (read-only) locates the app across every
// org the user belongs to instead of demanding a choice.
func TestAppInfoFindsAppAcrossOrgs(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"a"},{"id":"2","slug":"b"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/a/apps":
			w.Write([]byte(`[]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/b/apps":
			w.Write([]byte(`[{"name":"my-game"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/b/apps/my-game":
			w.Write([]byte(`{"name":"my-game","plan_name":"Small"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	out, err := run(t, "", "app", "info", "my-game")
	if err != nil {
		t.Fatalf("app info across orgs: %v", err)
	}
	if !strings.Contains(out, "my-game") {
		t.Errorf("expected the app in output, got %q", out)
	}
}

// A name present in more than one org is ambiguous: info must refuse to guess
// and ask for --org.
func TestAppInfoAmbiguousAcrossOrgs(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"a"},{"id":"2","slug":"b"}]`)) //nolint:errcheck
		case "/api/v1/orgs/a/apps", "/api/v1/orgs/b/apps":
			w.Write([]byte(`[{"name":"dup"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	_, err := run(t, "", "app", "info", "dup")
	if err == nil || !strings.Contains(err.Error(), "more than one organization") {
		t.Fatalf("got %v", err)
	}
}
