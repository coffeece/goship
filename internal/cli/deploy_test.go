package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadProjectIsOptional(t *testing.T) {
	p, name, err := loadProject(t.TempDir())
	if err != nil || p.App != "" || name != "" {
		t.Fatalf("a directory with no config must not be an error: %v", err)
	}
}

// Both spellings are accepted; the dotted form is what people reach for.
func TestLoadProjectAcceptsEveryName(t *testing.T) {
	for _, name := range projectFiles {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, name), "app: api\nplatform: go\nenv:\n  LOG_LEVEL: debug\n")

			p, got, err := loadProject(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != name {
				t.Errorf("read %q, want %q", got, name)
			}
			if p.App != "api" || p.Platform != "go" || p.Env["LOG_LEVEL"] != "debug" {
				t.Errorf("got %+v", p)
			}
		})
	}
}

func TestLoadProjectPrefersTheUndottedName(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".goship.yaml"), "app: dotted\n")
	write(t, filepath.Join(dir, "goship.yaml"), "app: plain\n")

	p, name, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.App != "plain" || name != "goship.yaml" {
		t.Errorf("got app=%q from %q", p.App, name)
	}
}

func TestParseEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	write(t, path, "# comment\n\nexport TOKEN=abc123\nQUOTED=\"a b\"\nURL=https://a?b=c\n")

	env, err := parseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"TOKEN":  "abc123",
		"QUOTED": "a b",
		"URL":    "https://a?b=c",
	} {
		if env[key] != want {
			t.Errorf("%s = %q, want %q", key, env[key], want)
		}
	}
	if len(env) != 3 {
		t.Errorf("unexpected keys: %v", env)
	}
}

func TestParseEnvFileRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	write(t, path, "NOT_A_PAIR\n")

	if _, err := parseEnvFile(path); err == nil {
		t.Fatal("expected an error")
	}
}

// Streaming commands have nothing for the renderer to format, so asking for
// JSON has to fail rather than silently produce a stream.
func TestStreamingCommandsRejectJSONOutput(t *testing.T) {
	for _, name := range []string{"deploy", "logs", "shell", "run", "releases", "rollback"} {
		t.Run(name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("GOSHIP_ORG", "acme")

			_, err := run(t, "", name, "--output", "json")
			if err == nil {
				t.Fatal("expected --output json to be refused")
			}
		})
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The whole point of the config being optional: a directory with nothing but
// source code deploys. The platform is inferred and the app created without a
// flag, a file, or a prompt.
func TestDeployWithNoConfigInfersEverything(t *testing.T) {
	var created map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			w.Write([]byte(freePlanCatalog)) //nolint:errcheck
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound) // the app does not exist yet
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created)             //nolint:errcheck
			w.Write([]byte(`{"name":"widget","platform":"go"}`)) //nolint:errcheck
		}
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")

	// The run fails once it reaches the platform deploy, which needs
	// credentials this test has no business holding. Everything under test
	// happens before that.
	out, _ := run(t, "", "deploy", dir)

	if created == nil {
		t.Fatal("the app was never created")
	}
	if created["name"] != "widget" {
		t.Errorf("name = %v, want the directory name", created["name"])
	}
	if created["platform"] != "go" {
		t.Errorf("platform = %v, want go inferred from go.mod", created["platform"])
	}
	if created["plan"] != "app-free" {
		t.Errorf("plan = %v, want the org's free plan chosen automatically", created["plan"])
	}
	if !strings.Contains(out, "detected from go.mod") {
		t.Errorf("the output should say where the platform came from, got %q", out)
	}
}

func TestDeployReportsWhenItCannotInferThePlatform(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			t.Error("an app was created without a known platform")
		}
		w.WriteHeader(http.StatusNotFound)
	})

	dir := filepath.Join(t.TempDir(), "mystery")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "README.md"), "")

	_, err := run(t, "", "deploy", dir)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"--platform", "go.mod", "nodejs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

const freePlanCatalog = `[
  {"slug":"app-free","kind":"app","display_name":"Free","cpu_milli":100,"memory_mb":256,"price_cents":0,"is_free":true,"is_active":true,"sort_order":10},
  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":1990,"is_free":false,"is_active":true,"sort_order":30}
]`

const paidPlanCatalog = `[
  {"slug":"app-micro","kind":"app","display_name":"Micro","cpu_milli":200,"memory_mb":256,"price_cents":990,"is_free":false,"is_active":true,"sort_order":20},
  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":1990,"is_free":false,"is_active":true,"sort_order":30}
]`

// Picking a paid plan on someone's behalf spends their money. With no free
// grant on the org, the command stops and shows what the choices cost.
func TestDeployWillNotPickAPaidPlanForYou(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			w.Write([]byte(paidPlanCatalog)) //nolint:errcheck
		case r.Method == http.MethodPost:
			t.Error("an app was created without the user choosing a plan")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")

	_, err := run(t, "", "deploy", dir)
	if err == nil {
		t.Fatal("expected the command to stop")
	}
	for _, want := range []string{"app-micro", "R$ 9.90/mo", "app-small", "--plan"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should show %q, got:\n%v", want, err)
		}
	}
}

// An explicit --plan skips the catalogue lookup entirely.
func TestDeployHonoursAnExplicitPlan(t *testing.T) {
	var created map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			t.Error("the catalogue should not be consulted when --plan is given")
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created) //nolint:errcheck
			w.Write([]byte(`{"name":"widget"}`))     //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")

	run(t, "", "deploy", dir, "--plan", "app-large") //nolint:errcheck
	if created["plan"] != "app-large" {
		t.Errorf("plan = %v, want app-large", created["plan"])
	}
}
