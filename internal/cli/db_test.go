package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The password comes back once, so the table must show it.
func TestDBUserAddShowsThePasswordOnce(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/acme/projects/clave/databases/shop/users" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"user":{"id":"u1","username":"reader","access_mode":"ro"},"password":"s3cret"}`)) //nolint:errcheck
	})

	out, err := run(t, "", "db", "user-add", "shop", "reader", "--access", "ro")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"USERNAME", "PASSWORD", "reader", "ro", "s3cret"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestDBServerCreateResolvesTheAccountAndPostsAtItsScope(t *testing.T) {
	var body map[string]any
	var postPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/clave/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"gcp","label":"Prod","status":"active","scope":"org"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/database-servers":
			postPath = r.URL.Path
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":"s1","name":"pg","status":"creating","stage":"creating_instance"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := run(t, "", "db", "server", "create", "--account", "gcp", "--region", "southamerica-east1")
	if err != nil {
		t.Fatal(err)
	}
	// An org-level account puts the server at the organization level.
	if postPath != "/api/v1/orgs/acme/database-servers" || body["cloud_account_id"] != "acc-1" || body["tier"] != "db-f1-micro" || body["name"] != "pg-prod" {
		t.Fatalf("path %s body %v", postPath, body)
	}
	if !strings.Contains(out, "creating") {
		t.Errorf("output = %q", out)
	}
}

func TestDBServerListAndDelete(t *testing.T) {
	var deleted string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/clave/database-servers":
			w.Write([]byte(`[{"id":"s1","name":"pg","status":"active","region":"southamerica-east1","tier":"db-f1-micro","host":"34.1.2.3","database_count":2,"scope":"org"}]`)) //nolint:errcheck
		case "DELETE /api/v1/orgs/acme/database-servers/s1":
			deleted = "s1"
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := run(t, "", "db", "server", "list")
	if err != nil || !strings.Contains(out, "34.1.2.3") || !strings.Contains(out, "pg") {
		t.Fatalf("list: %v %q", err, out)
	}
	if _, err := run(t, "", "db", "server", "delete", "pg"); err != nil || deleted != "s1" {
		t.Fatalf("delete: %v, deleted %q", err, deleted)
	}
}

func TestDBCreateOnAServer(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/clave/database-servers":
			w.Write([]byte(`[{"id":"s1","name":"pg","status":"active"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/projects/clave/services/postgresql/instances":
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if _, err := run(t, "", "db", "create", "shop", "--plan", "db-small", "--server", "pg"); err != nil {
		t.Fatal(err)
	}
	if body["server_id"] != "s1" {
		t.Fatalf("body = %v", body)
	}
}
