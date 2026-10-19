package mcptools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// serversAPI is a project with one server of its own and one shared by the
// organization.
func serversAPI(t *testing.T, record func(r *http.Request, body string)) string {
	t.Helper()
	return api(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		record(r, string(b))
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects/clave/database-servers":
			if r.Method == http.MethodGet {
				w.Write([]byte(`[{"id":"srv-own","name":"pg-prod","scope":"project","status":"ready"},{"id":"srv-org","name":"pg-shared","scope":"org","status":"ready"}]`)) //nolint:errcheck
				return
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":"srv-new","name":"pg-gcp","status":"provisioning"}`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/clave/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-org","provider":"gcp","label":"gcp","scope":"org"},{"id":"acc-own","provider":"aws","label":"aws","scope":"project"}]`)) //nolint:errcheck
		default:
			w.Write([]byte(`{}`)) //nolint:errcheck
		}
	})
}

func TestCreateDatabaseOnAServerSendsItsID(t *testing.T) {
	var posted string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/projects/clave/services/postgresql/instances" {
			posted = body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	res := call(t, cs, "goship_create_database", map[string]any{"name": "db", "plan": "db-small", "server": "pg-prod"})
	if res.IsError {
		t.Fatal(text(res))
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(posted), &body); err != nil || body["server_id"] != "srv-own" {
		t.Errorf("posted %s: %v", posted, err)
	}

	res = call(t, cs, "goship_create_database", map[string]any{"name": "db", "plan": "db-small", "server": "nope"})
	if !res.IsError || !strings.Contains(text(res), "goship_list_database_servers") {
		t.Errorf("unknown server: isError=%v %q", res.IsError, text(res))
	}
}

func TestDatabaseServerToolsFollowTheServerScope(t *testing.T) {
	var paths []string
	url := serversAPI(t, func(r *http.Request, _ string) {
		if r.Method != http.MethodGet {
			paths = append(paths, r.Method+" "+r.URL.RequestURI())
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})

	res := call(t, cs, "goship_list_database_servers", nil)
	if res.IsError {
		t.Fatal(text(res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"pg-shared"`) {
		t.Errorf("the listing must include the organization's servers: %s", raw)
	}

	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"goship_retry_database_server", map[string]any{"server": "pg-prod"}, "POST /api/v1/orgs/acme/projects/clave/database-servers/srv-own/retry"},
		{"goship_retry_database_server", map[string]any{"server": "pg-shared"}, "POST /api/v1/orgs/acme/database-servers/srv-org/retry"},
		{"goship_delete_database_server", map[string]any{"server": "pg-shared"}, "DELETE /api/v1/orgs/acme/database-servers/srv-org"},
		{"goship_delete_database_server", map[string]any{"server": "pg-prod", "force": true}, "DELETE /api/v1/orgs/acme/projects/clave/database-servers/srv-own?force=true"},
		{"goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "southamerica-east1"}, "POST /api/v1/orgs/acme/database-servers"},
		{"goship_create_database_server", map[string]any{"cloud_account": "acc-own", "region": "us-east-1", "tier": "db-g1-small", "name": "pg-aws"}, "POST /api/v1/orgs/acme/projects/clave/database-servers"},
	} {
		paths = nil
		res := call(t, cs, c.tool, c.args)
		if res.IsError {
			t.Errorf("%s %v: %s", c.tool, c.args, text(res))
			continue
		}
		if len(paths) != 1 || paths[0] != c.want {
			t.Errorf("%s %v called %v, want %s", c.tool, c.args, paths, c.want)
		}
	}
}

func TestCreateDatabaseServerDefaultsNameAndTier(t *testing.T) {
	var posted string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost {
			posted = body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	res := call(t, cs, "goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "southamerica-east1"})
	if res.IsError {
		t.Fatal(text(res))
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(posted), &body); err != nil || body["cloud_account_id"] != "acc-org" || body["name"] != "pg-gcp" || body["tier"] != "db-f1-micro" {
		t.Errorf("posted %s: %v", posted, err)
	}
}

func TestMoveCloudAccount(t *testing.T) {
	var path, posted string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost {
			path, posted = r.URL.Path, body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	res := call(t, cs, "goship_move_cloud_account", map[string]any{"account_id": "acc-own", "to_project": "mova"})
	if res.IsError {
		t.Fatal(text(res))
	}
	if path != "/api/v1/orgs/acme/cloud-accounts/acc-own/move" || !strings.Contains(posted, `"project":"mova"`) {
		t.Errorf("called %s with %s", path, posted)
	}
	res = call(t, cs, "goship_move_cloud_account", map[string]any{"account_id": "acc-own"})
	if res.IsError || !strings.Contains(posted, `"project":""`) {
		t.Errorf("to the organization: isError=%v %q posted %s", res.IsError, text(res), posted)
	}
}

func TestDisableDatabaseExtensionDeletesIt(t *testing.T) {
	var got string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	res := call(t, cs, "goship_disable_database_extension", map[string]any{"database": "db", "extension": "postgis"})
	if res.IsError || got != "DELETE /api/v1/orgs/acme/projects/clave/databases/db/extensions/postgis" {
		t.Errorf("isError=%v %q called %s", res.IsError, text(res), got)
	}
}
