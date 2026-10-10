package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/config"
)

func TestProjectsListsTheOrgsProjects(t *testing.T) {
	var gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`[{"slug":"default","name":"Default","is_default":true,"role":"admin"}]`)) //nolint:errcheck
	})
	out, err := run(t, "", "projects")
	if err != nil || gotPath != "/api/v1/orgs/acme/projects" || !strings.Contains(out, "default") || !strings.Contains(out, "admin") {
		t.Fatalf("projects = %q, %v (path %q)", out, err, gotPath)
	}
}

func TestProjectUsePersistsPerOrg(t *testing.T) {
	var gotPath string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects":
			w.Write([]byte(`[{"slug":"atlas"},{"slug":"comet"}]`)) //nolint:errcheck
		default:
			gotPath = r.URL.Path
			w.Write([]byte(`[]`)) //nolint:errcheck
		}
	})
	t.Setenv("GOSHIP_ORG", "acme")
	t.Setenv("GOSHIP_PROJECT", "")

	if _, err := run(t, "", "project", "use", "nope"); err == nil || !strings.Contains(err.Error(), "goship projects") {
		t.Fatalf("an unreachable project must be refused: %v", err)
	}
	if out, err := run(t, "", "project", "use", "comet"); err != nil || !strings.Contains(out, "acme/comet") {
		t.Fatalf("project use = %q, %v", out, err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Projects["acme"] != "comet" {
		t.Errorf("Projects = %v", cfg.Projects)
	}

	// Chosen, so the next project-scoped command lands there.
	if _, err := run(t, "", "apps"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/projects/comet/apps" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestProjectCreateSendsNameAndSlug(t *testing.T) {
	var body map[string]string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/acme/projects" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"slug":"atlas","name":"Atlas","role":"admin"}`)) //nolint:errcheck
	})
	if _, err := run(t, "", "project", "create", "atlas", "--name", "Atlas"); err != nil {
		t.Fatal(err)
	}
	if body["slug"] != "atlas" || body["name"] != "Atlas" {
		t.Errorf("body = %v", body)
	}
	if _, err := run(t, "", "project", "create", "comet"); err != nil || body["name"] != "comet" {
		t.Errorf("the name defaults to the slug: %v, %v", body, err)
	}
}
