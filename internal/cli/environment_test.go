package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/config"
)

const twoEnvironments = `[{"id":"e1","slug":"production","name":"Production","cluster_name":"goship","is_default":true},
                          {"id":"e2","slug":"staging","name":"Staging","cluster_id":"c-home","cluster_name":"homolog"}]`

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
	if strings.Join(header, " ") != "SLUG NAME CLUSTER DEFAULT" {
		t.Errorf("header = %q", header)
	}
	if !strings.Contains(out, "staging") || !strings.Contains(out, "homolog") {
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
	if !strings.Contains(out, "acme/clave/staging") {
		t.Errorf("output = %q", out)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Environments) != 1 || cfg.Environments["acme/clave"] != "staging" {
		t.Errorf("Environments = %v", cfg.Environments)
	}
	if got := cfg.SelectedEnvironment("acme", "mova"); got != "" {
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
			w.Write([]byte(`[{"id":"c-goship","name":"goship","shared":true},{"id":"c-home","name":"homolog","type":"vps"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/database-servers":
			w.Write([]byte(`[{"id":"s-1","name":"pg-staging"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/environments":
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"e2","slug":"staging","name":"Staging","cluster_id":"c-home","cluster_name":"homolog"}`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})

	out, err := run(t, "", "environment", "create", "staging", "--name", "Staging", "--cluster", "homolog", "--db-server", "pg-staging")
	if err != nil {
		t.Fatal(err)
	}
	if body["slug"] != "staging" || body["name"] != "Staging" || body["cluster_id"] != "c-home" || body["db_server_id"] != "s-1" {
		t.Errorf("body = %v", body)
	}
	if !strings.Contains(out, "homolog") {
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

// The API names the projects that still hold something; that is what the
// person needs, word for word.
func TestEnvironmentRmPrintsTheConflictBody(t *testing.T) {
	const msg = "environment staging still holds resources (clave: 2 apps, 1 database; mova: 1 volume); remove them first"
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
	})

	_, _, err := runWithStderr(t, context.Background(), strings.NewReader(""), "environment", "rm", "staging", "--yes")
	if err == nil || err.Error() != msg {
		t.Fatalf("err = %v, want %q", err, msg)
	}
}
