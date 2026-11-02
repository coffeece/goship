package cli

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/config"
)

const twoEnvironments = `[{"id":"e1","slug":"production","name":"Production","cluster_name":"goship","is_default":true,"enabled":true},
                          {"id":"e2","slug":"staging","name":"Staging","cluster_id":"c-home","cluster_name":"edge1","enabled":true}]`

func TestEnvironmentsListsTheOrgsEnvironments(t *testing.T) {
	var gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(twoEnvironments)) //nolint:errcheck
	})
	out, err := run(t, "", "environments")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/environments" {
		t.Errorf("path = %q", gotPath)
	}
	header := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	if strings.Join(header, " ") != "SLUG NAME CLUSTER DEFAULT ENABLED" {
		t.Errorf("header = %q", header)
	}
	if !strings.Contains(out, "staging") || !strings.Contains(out, "edge1") {
		t.Errorf("output = %q", out)
	}
}

// The selection is per project: another project of the org keeps its own.
func TestEnvironmentUseWritesConfig(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/orgs/acme/environments" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(twoEnvironments)) //nolint:errcheck
	})
	t.Setenv("GOSHIP_ENVIRONMENT", "")

	out, err := run(t, "", "environment", "use", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "acme/atlas/staging") {
		t.Errorf("output = %q", out)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Environments) != 1 || cfg.Environments["acme/atlas"] != "staging" {
		t.Errorf("Environments = %v", cfg.Environments)
	}
	if got := cfg.SelectedEnvironment("acme", "comet"); got != "" {
		t.Errorf("another project picked up the selection: %q", got)
	}
}

func TestEnvironmentUseRefusesAnUnknownSlug(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(twoEnvironments)) //nolint:errcheck
	})
	t.Setenv("GOSHIP_ENVIRONMENT", "")

	_, err := run(t, "", "environment", "use", "qa")
	if err == nil || !strings.Contains(err.Error(), `"qa"`) || !strings.Contains(err.Error(), "goship environments") {
		t.Fatalf("err = %v, want the slug refused and how to list them", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Environments) != 0 {
		t.Errorf("an unknown slug was saved: %v", cfg.Environments)
	}
}

// The person types the cluster's name; the API keys on its id.
func TestEnvironmentCreateSendsClusterID(t *testing.T) {
	var body map[string]any
	var calls []string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/clusters":
			w.Write([]byte(`[{"id":"c-goship","name":"goship","shared":true},{"id":"c-home","name":"edge1","type":"vps"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/database-servers":
			w.Write([]byte(`[{"id":"s-1","name":"pg-staging"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/environments":
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"e2","slug":"staging","name":"Staging","cluster_id":"c-home","cluster_name":"edge1"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	out, err := run(t, "", "environment", "create", "staging", "--name", "Staging", "--cluster", "edge1", "--db-server", "pg-staging")
	if err != nil {
		t.Fatal(err)
	}
	if body["slug"] != "staging" || body["name"] != "Staging" || body["cluster_id"] != "c-home" || body["db_server_id"] != "s-1" {
		t.Errorf("body = %v", body)
	}
	if !strings.Contains(out, "edge1") {
		t.Errorf("output = %q", out)
	}

	// Neither flag: GoShip's shared cluster and PostgreSQL, nothing to resolve.
	calls, body = nil, nil
	if _, err := run(t, "", "environment", "create", "qa"); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "qa" || body["cluster_id"] != nil || body["db_server_id"] != nil {
		t.Errorf("body = %v, want the slug as name and no cluster or server", body)
	}
	if len(calls) != 1 {
		t.Errorf("calls = %q, want only the POST", calls)
	}
}

func TestEnvironmentRmConfirms(t *testing.T) {
	deleted := ""
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		deleted = r.URL.Path
		w.Write([]byte(`{"status":"deleted"}`)) //nolint:errcheck
	})

	if _, err := run(t, "n\n", "environment", "rm", "staging"); err == nil || deleted != "" {
		t.Fatalf("err = %v, deleted %q: want an abort without a request", err, deleted)
	}
	out, err := run(t, "", "environment", "rm", "staging", "--yes")
	if err != nil || deleted != "/api/v1/orgs/acme/environments/staging" || !strings.Contains(out, "staging deleted") {
		t.Fatalf("rm --yes = %q, %v (deleted %q)", out, err, deleted)
	}
}

// A removed environment cannot stay selected: every later command in those
// projects would fail on a name that no longer exists.
func TestEnvironmentRmForgetsTheSavedSelection(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"deleted"}`)) //nolint:errcheck
	})
	t.Setenv("GOSHIP_ENVIRONMENT", "")
	saved := &config.Config{Environments: map[string]string{
		"acme/atlas":   "staging",
		"acme/labs":    "staging",
		"acme/web":     "production",
		"globex/atlas": "staging",
	}}
	if err := saved.Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := run(t, "", "environment", "rm", "staging", "--yes"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"acme/web": "production", "globex/atlas": "staging"}
	if !maps.Equal(cfg.Environments, want) {
		t.Errorf("Environments = %v, want %v", cfg.Environments, want)
	}
}

// The environment is gone either way; a config file that cannot be written is
// a note, not a failure.
func TestEnvironmentRmSurvivesAConfigItCannotSave(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only files")
	}
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"deleted"}`)) //nolint:errcheck
	})
	t.Setenv("GOSHIP_ENVIRONMENT", "")
	if err := (&config.Config{Environments: map[string]string{"acme/atlas": "staging"}}).Save(); err != nil {
		t.Fatal(err)
	}
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}

	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), "environment", "rm", "staging", "--yes")
	if err != nil || !strings.Contains(out, "Environment staging deleted.") {
		t.Fatalf("rm = %q, %v: the delete succeeded and must say so", out, err)
	}
	if !strings.Contains(stderr, "could not forget the saved environment staging") {
		t.Errorf("stderr = %q, want a note", stderr)
	}
}

// The API names the projects that still hold something; that is what the
// person needs, word for word.
func TestEnvironmentRmPrintsTheConflictBody(t *testing.T) {
	const msg = "environment staging still holds resources (atlas: 2 apps, 1 database; comet: 1 volume); remove them first"
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
	})

	_, _, err := runWithStderr(t, context.Background(), strings.NewReader(""), "environment", "rm", "staging", "--yes")
	if err == nil || err.Error() != msg {
		t.Fatalf("err = %v, want %q", err, msg)
	}
}

func TestEnvironmentsShowsEnabledColumn(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"slug":"development","name":"Development","is_default":true,"enabled":true},{"slug":"staging","name":"Staging","enabled":false}]`)) //nolint:errcheck
	})
	out, err := run(t, "", "environments")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasSuffix(strings.TrimSpace(lines[1]), "true") || !strings.HasSuffix(strings.TrimSpace(lines[2]), "false") {
		t.Errorf("output = %q", out)
	}
	if f := strings.Fields(lines[1]); f[0] != "development" || f[len(f)-2] != "true" || f[len(f)-1] != "true" {
		t.Errorf("development row = %q", lines[1])
	}
}

func TestEnvironmentEnableSendsEnabledTrue(t *testing.T) {
	for verb, want := range map[string]bool{"enable": true, "disable": false} {
		var method, path string
		var body map[string]any
		stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			json.NewDecoder(r.Body).Decode(&body)                                                             //nolint:errcheck
			w.Write([]byte(`{"slug":"staging","name":"Staging","enabled":` + strconv.FormatBool(want) + `}`)) //nolint:errcheck
		})
		out, err := run(t, "", "environment", verb, "staging")
		if err != nil {
			t.Fatal(err)
		}
		if method != http.MethodPatch || path != "/api/v1/orgs/acme/environments/staging" || body["enabled"] != want {
			t.Errorf("%s: %s %s %v", verb, method, path, body)
		}
		if !strings.Contains(out, "staging") {
			t.Errorf("%s: output = %q", verb, out)
		}
	}
}

func TestEnvironmentDisablePrintsTheAPIRefusal(t *testing.T) {
	const msg = "development is the default environment; make another one the default first"
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
	})
	_, err := run(t, "", "environment", "disable", "development")
	if err == nil || err.Error() != msg {
		t.Fatalf("err = %v, want %q", err, msg)
	}
}
