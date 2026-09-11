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

// Both prompts read from one buffered reader; a reader per prompt would
// swallow the password line when credentials are piped in.
func TestLoginReadsBothPromptsFromAPipe(t *testing.T) {
	var got map[string]string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got) //nolint:errcheck
		w.Write([]byte(`{"token":"t"}`))     //nolint:errcheck
	})

	if _, err := run(t, "gui@example.com\nhunter2\n", "login"); err != nil {
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
