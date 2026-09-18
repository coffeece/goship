package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
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
			json.NewDecoder(r.Body).Decode(&created)                                        //nolint:errcheck
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget","platform":"go"}`)) //nolint:errcheck
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
  {"slug":"app-free","kind":"app","display_name":"Free","cpu_milli":100,"memory_mb":256,"price_cents":0,"is_free":true,"is_active":true,"sort_order":10,"billed":false},
  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":1990,"is_free":false,"is_active":true,"sort_order":30,"billed":true}
]`

const paidPlanCatalog = `[
  {"slug":"app-micro","kind":"app","display_name":"Micro","cpu_milli":200,"memory_mb":256,"price_cents":990,"is_free":false,"is_active":true,"sort_order":20,"billed":true},
  {"slug":"app-small","kind":"app","display_name":"Small","cpu_milli":500,"memory_mb":512,"price_cents":1990,"is_free":false,"is_active":true,"sort_order":30,"billed":true}
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
			json.NewDecoder(r.Body).Decode(&created)                        //nolint:errcheck
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
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

// Deploy creates whatever it cannot find. Without this check, a wrong --org
// silently makes a second app of the same name somewhere else rather than
// deploying the one the user meant.
func TestDeployStopsWhenTheAppLivesInAnotherOrg(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"acme"},{"id":"2","slug":"other"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/other/apps/widget":
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
		case r.Method == http.MethodPost:
			t.Error("a duplicate app was created in the wrong org")
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
	for _, want := range []string{`"widget"`, "--org other", "goship org use other"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%v", want, err)
		}
	}
}

// A single-org account pays nothing for the check, and a genuinely new app
// still gets created.
func TestDeployStillCreatesWhenTheNameIsFreeEverywhere(t *testing.T) {
	var created map[string]any
	orgsListed := 0
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs":
			orgsListed++
			w.Write([]byte(`[{"id":"1","slug":"acme"}]`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			w.Write([]byte(freePlanCatalog)) //nolint:errcheck
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created)                        //nolint:errcheck
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")

	run(t, "", "deploy", dir) //nolint:errcheck
	if created == nil {
		t.Fatal("a genuinely new app must still be created")
	}
	if orgsListed != 1 {
		t.Errorf("orgs listed %d times, want exactly one lookup", orgsListed)
	}
}

// A pinned org left over from another account answers 403, not 404. The search
// has to run there too, or the user is told "forbidden" while the app they
// asked for is sitting in an org they do belong to.
func TestDeployFindsTheAppWhenThePinnedOrgIsForbidden(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"mine"}]`)) //nolint:errcheck
		case r.URL.Path == "/api/v1/orgs/mine/apps/widget":
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
		case r.Method == http.MethodPost:
			t.Error("an app was created despite one existing in a reachable org")
		default:
			w.WriteHeader(http.StatusForbidden)
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
	for _, want := range []string{"not a member", "--org mine"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%v", want, err)
		}
	}
}

// Forbidden with the app nowhere reachable keeps the membership error rather
// than trying to create into an org the caller cannot use.
func TestDeployKeepsForbiddenWhenTheAppIsNowhere(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/orgs" {
			w.Write([]byte(`[{"id":"1","slug":"mine"}]`)) //nolint:errcheck
			return
		}
		if r.Method == http.MethodPost {
			t.Error("tried to create into a forbidden org")
		}
		w.WriteHeader(http.StatusForbidden)
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")

	_, err := run(t, "", "deploy", dir)
	// A bare 403 renders as "Forbidden"; the API's JSON body is lowercase.
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "forbidden") {
		t.Fatalf("got %v", err)
	}
}

// The app name is known before the org has to be. With none selected and the
// app living in exactly one of the caller's orgs, there is nothing to ask.
func TestDeployPicksTheOrgThatOwnsTheApp(t *testing.T) {
	var deployedOrg string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"alpha"},{"id":"2","slug":"beta"}]`)) //nolint:errcheck
		case "/api/v1/orgs/beta/apps/widget":
			deployedOrg = "beta"
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")

	out, _ := run(t, "", "deploy", dir)

	if deployedOrg != "beta" {
		t.Errorf("never looked the app up in beta")
	}
	if !strings.Contains(out, "Using org beta") {
		t.Errorf("the choice must be visible, got %q", out)
	}
}

// In two orgs at once, picking one would be a guess.
func TestDeployAsksWhenTheNameIsAmbiguous(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"alpha"},{"id":"2","slug":"beta"}]`)) //nolint:errcheck
		case "/api/v1/orgs/alpha/apps/widget", "/api/v1/orgs/beta/apps/widget":
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")

	_, err := run(t, "", "deploy", dir)
	if err == nil {
		t.Fatal("expected the command to stop")
	}
	for _, want := range []string{"alpha", "beta", "--org"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%v", want, err)
		}
	}
}

// The platform's stream ends on "OK" and never says where the app is. A custom
// domain wins over the platform address, because that is the URL people use.
func TestPublicURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  portal.App
		want string
	}{
		{"cname wins", portal.App{
			CNames:    []string{"blog.com"},
			Addresses: []string{"https://blog.apps.goship.sh"},
		}, "https://blog.com"},
		{"cname already absolute", portal.App{CNames: []string{"http://example.com"}}, "https://example.com"},
		{"falls back to the platform address", portal.App{
			Addresses: []string{"https://widget.apps.goship.sh"},
		}, "https://widget.apps.goship.sh"},
		{"nothing to show", portal.App{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicURL(&tc.app); got != tc.want {
				t.Errorf("publicURL = %q, want %q", got, tc.want)
			}
		})
	}
}

// An existing app with a container file deploys through it. The flag reaches
// tsuru-client as --dockerfile, which is what makes it a container build
// rather than a platform build.
func TestDeployBuildsFromADockerfile(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			t.Error("an app was created when one already exists")
		}
		w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "Dockerfile"), "FROM alpine\n")

	out, _ := run(t, "", "deploy", dir)
	if !strings.Contains(out, "Building widget from Dockerfile") {
		t.Errorf("the container build should be announced, got %q", out)
	}
}

// A container-only project creates an app with no platform at all — Tsuru
// resolves a platform only when one is given, so the image the Dockerfile
// builds is the whole definition.
func TestDeployCreatesAPlatformlessAppForADockerfile(t *testing.T) {
	var created map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			w.Write([]byte(freePlanCatalog)) //nolint:errcheck
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created)                        //nolint:errcheck
			w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "Dockerfile"), "FROM alpine\n")

	out, _ := run(t, "", "deploy", dir)

	if created == nil {
		t.Fatal("the app was never created")
	}
	if p, ok := created["platform"]; ok && p != "" {
		t.Errorf("platform = %v, want it absent for a container build", p)
	}
	if !strings.Contains(out, "built from Dockerfile") {
		t.Errorf("the creation line should say what builds it, got %q", out)
	}
}

// The config's dockerfile survives the round trip and drives the build.
func TestDeployHonoursTheDockerfileFromConfig(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"widget","tsuru_name":"acme-widget"}`)) //nolint:errcheck
	})

	dir := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module widget\n")
	write(t, filepath.Join(dir, "Dockerfile.prod"), "FROM alpine\n")
	write(t, filepath.Join(dir, "goship.yml"), "app: widget\ndockerfile: Dockerfile.prod\n")

	out, _ := run(t, "", "deploy", dir)
	if !strings.Contains(out, "Building widget from Dockerfile.prod") {
		t.Errorf("the config's dockerfile should win over the go.mod, got %q", out)
	}
}

// On the customer's own hardware there is nothing to bill, so the API applies
// the free plan itself when none is named. Offering the paid catalogue here
// would be asking someone to pay for a machine they already own.
func TestDeployOnANodeDoesNotAskForAPlan(t *testing.T) {
	var created map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(`[{"id":"n-1","name":"do-server1"}]`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			t.Error("the paid catalogue must not be consulted for a node-placed app")
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created)                      //nolint:errcheck
			w.Write([]byte(`{"name":"quake","tsuru_name":"acme-quake"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	dir := filepath.Join(t.TempDir(), "quake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module quake\n")

	out, _ := run(t, "", "deploy", dir, "--node", "do-server1")

	if created == nil {
		t.Fatal("the app was never created")
	}
	if created["node_id"] != "n-1" {
		t.Errorf("node_id = %v, want the id resolved from the name", created["node_id"])
	}
	if p, ok := created["plan"]; ok && p != "" {
		t.Errorf("plan = %v, want none — the API applies the free plan on a node", p)
	}
	if !strings.Contains(out, "on your node") {
		t.Errorf("the placement should be visible, got %q", out)
	}
}

// The same placement can live in the project file.
func TestDeployReadsTheNodeFromConfig(t *testing.T) {
	var created map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/orgs/acme/nodes":
			w.Write([]byte(`[{"id":"n-1","name":"do-server1"}]`)) //nolint:errcheck
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created)                      //nolint:errcheck
			w.Write([]byte(`{"name":"quake","tsuru_name":"acme-quake"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	dir := filepath.Join(t.TempDir(), "quake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module quake\n")
	write(t, filepath.Join(dir, "goship.yml"), "app: quake\nnode: do-server1\n")

	run(t, "", "deploy", dir) //nolint:errcheck
	if created["node_id"] != "n-1" {
		t.Errorf("node_id = %v, want the id resolved from the config's name", created["node_id"])
	}
}

// The build must be addressed by the mangled tsuru_name, not the display name:
// the platform does not know "quake", only "acme-quake". Passing the display
// name is the bug that produced `App quake not found` after a clean create.
func TestDeployBuildsAgainstTheMangledName(t *testing.T) {
	var gotBuild buildArgs
	orig := runBuild
	runBuild = func(_ *cobra.Command, _ *App, a buildArgs) error { gotBuild = a; return nil }
	t.Cleanup(func() { runBuild = orig })

	var created map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/available-plans"):
			w.Write([]byte(freePlanCatalog)) //nolint:errcheck
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound) // new app
		case r.Method == http.MethodPost:
			json.NewDecoder(r.Body).Decode(&created)                      //nolint:errcheck
			w.Write([]byte(`{"name":"quake","tsuru_name":"acme-quake"}`)) //nolint:errcheck
		}
	})

	dir := filepath.Join(t.TempDir(), "quake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "go.mod"), "module quake\n")

	if _, err := run(t, "", "deploy", dir); err != nil {
		t.Fatal(err)
	}
	if gotBuild.tsuruName != "acme-quake" {
		t.Errorf("build addressed %q, want the mangled acme-quake", gotBuild.tsuruName)
	}
	if gotBuild.displayName != "quake" {
		t.Errorf("display name = %q, want quake (used only in messages)", gotBuild.displayName)
	}
}
