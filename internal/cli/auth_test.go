package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/config"
	"github.com/spf13/cobra"
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

// loginAPI is a portal that signs "gui@example.com" in and mints API tokens.
func loginAPI(t *testing.T, calls *[]string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == "/api/v1/login":
			w.Write([]byte(`{"token":"jwt-123"}`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/auth/tokens" && r.Method == http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if name, _ := body["name"].(string); !strings.HasPrefix(name, "goship CLI") || body["expires_in_days"] != float64(sessionDays) {
				t.Errorf("token request = %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"tok-1","token":"gsp_session"}`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/auth/me":
			w.Write([]byte(`{"email":"gui@example.com","groups":["acme"]}`)) //nolint:errcheck
		case strings.HasPrefix(r.URL.Path, "/api/v1/auth/tokens/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// A sign-in is traded for an API token: that is what lasts, shows up in the
// profile page, and can be revoked. The portal's own token lasts hours.
func TestLoginKeepsARevocableAPIToken(t *testing.T) {
	var calls []string
	stubAPI(t, loginAPI(t, &calls))

	out, err := run(t, "hunter2\n", "login", "--email", "gui@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Logged in as gui@example.com") {
		t.Errorf("output = %q", out)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "gsp_session" || cfg.TokenID != "tok-1" {
		t.Errorf("stored token=%q id=%q, want the API token and its id", cfg.Token, cfg.TokenID)
	}
	if len(calls) < 2 || calls[1] != "POST /api/v1/auth/tokens Bearer jwt-123" {
		t.Errorf("calls = %v, want the fresh sign-in to mint the token", calls)
	}
}

func TestBrowserLoginEndsInTheSameSession(t *testing.T) {
	var calls []string
	stubAPI(t, loginAPI(t, &calls))
	orig := loginInBrowser
	loginInBrowser = func(*cobra.Command, *App) (string, error) { return "jwt-from-browser", nil }
	t.Cleanup(func() { loginInBrowser = orig })

	if _, err := run(t, "", "login"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if cfg.Token != "gsp_session" {
		t.Errorf("token = %q", cfg.Token)
	}
	if calls[0] != "POST /api/v1/auth/tokens Bearer jwt-from-browser" {
		t.Errorf("calls = %v", calls)
	}
}

// An API that cannot mint tokens still leaves the person signed in.
func TestLoginFallsBackToTheShortLivedToken(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/login":
			w.Write([]byte(`{"token":"jwt-123"}`)) //nolint:errcheck
		case "/api/v1/auth/me":
			w.Write([]byte(`{"email":"gui@example.com"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	if _, err := run(t, "hunter2\n", "login", "--email", "gui@example.com"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if cfg.Token != "jwt-123" || cfg.TokenID != "" {
		t.Errorf("token=%q id=%q", cfg.Token, cfg.TokenID)
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

func TestLogoutRevokesTheTokenAndClearsIt(t *testing.T) {
	var calls []string
	stubAPI(t, loginAPI(t, &calls))
	if err := (&config.Config{Token: "gsp_session", TokenID: "tok-1"}).Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, "", "logout"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load()
	if cfg.Token != "" || cfg.TokenID != "" {
		t.Errorf("token=%q id=%q, want both cleared", cfg.Token, cfg.TokenID)
	}
	if len(calls) != 1 || calls[0] != "DELETE /api/v1/auth/tokens/tok-1 Bearer gsp_session" {
		t.Errorf("calls = %v, want the token revoked server-side", calls)
	}
}

// Offline, or already revoked from the profile page: still signed out here.
func TestLogoutClearsTheTokenEvenWhenRevokingFails(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	if err := (&config.Config{Token: "gsp_session", TokenID: "tok-1"}).Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, "", "logout"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := config.Load(); cfg.Token != "" {
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
