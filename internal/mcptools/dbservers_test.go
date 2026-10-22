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

func TestSetDatabaseServerDiskLimitResolvesNameAndScope(t *testing.T) {
	var calls []string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPut {
			calls = append(calls, r.URL.Path+" "+body)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"server": "pg-prod", "disk_limit_gb": 200}, `/api/v1/orgs/acme/projects/clave/database-servers/srv-own/disk-limit {"disk_limit_gb":200}`},
		{map[string]any{"server": "srv-org", "disk_limit_gb": 0}, `/api/v1/orgs/acme/database-servers/srv-org/disk-limit {"disk_limit_gb":0}`},
	} {
		calls = nil
		res := call(t, cs, "goship_set_database_server_disk_limit", c.args)
		if res.IsError {
			t.Fatal(text(res))
		}
		if len(calls) != 1 || calls[0] != c.want {
			t.Errorf("%v called %v, want %s", c.args, calls, c.want)
		}
	}
	res := call(t, cs, "goship_set_database_server_disk_limit", map[string]any{"server": "pg-prod", "disk_limit_gb": -1})
	if !res.IsError {
		t.Error("a negative cap must be refused")
	}
	res = call(t, cs, "goship_set_database_server_disk_limit", map[string]any{"server": "nope", "disk_limit_gb": 50})
	if !res.IsError || !strings.Contains(text(res), "goship_list_database_servers") {
		t.Errorf("unknown server: %q", text(res))
	}
}

func TestSetDatabaseSizeLimitZeroClears(t *testing.T) {
	var bodies []string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/size-limit") {
			bodies = append(bodies, string(b))
		}
		w.Write([]byte(`{"message":"ok"}`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	for _, c := range []struct {
		mb   int
		want string
	}{{500, `{"size_limit_mb":500}`}, {0, `{"size_limit_mb":null}`}} {
		bodies = nil
		res := call(t, cs, "goship_set_database_size_limit", map[string]any{"database": "db", "size_limit_mb": c.mb})
		if res.IsError {
			t.Fatal(text(res))
		}
		if len(bodies) != 1 || bodies[0] != c.want {
			t.Errorf("mb=%d sent %v, want %s", c.mb, bodies, c.want)
		}
	}
	res := call(t, cs, "goship_set_database_size_limit", map[string]any{"database": "db", "size_limit_mb": 5})
	if !res.IsError || !strings.Contains(text(res), "minimum is 10") {
		t.Errorf("5 MB: %q", text(res))
	}
}

func TestCreateDatabaseSizeLimitAndPlanRules(t *testing.T) {
	var posted string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/instances") {
			posted = body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	res := call(t, cs, "goship_create_database", map[string]any{"name": "db", "server": "pg-prod", "size_limit_mb": 500})
	if res.IsError {
		t.Fatal(text(res))
	}
	if !strings.Contains(posted, `"size_limit_mb":500`) {
		t.Errorf("posted %s", posted)
	}
	for _, args := range []map[string]any{
		{"name": "db"},
		{"name": "db", "plan": "db-small", "size_limit_mb": 500},
		{"name": "db", "server": "pg-prod", "size_limit_mb": 5},
	} {
		if res := call(t, cs, "goship_create_database", args); !res.IsError {
			t.Errorf("%v should be refused", args)
		}
	}
}

func TestCreateDatabaseServerSendsDiskLimit(t *testing.T) {
	var posted string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost {
			posted = body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "clave"}, Options{})
	if res := call(t, cs, "goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "r", "disk_limit_gb": 50}); res.IsError {
		t.Fatal(text(res))
	}
	if !strings.Contains(posted, `"disk_limit_gb":50`) {
		t.Errorf("posted %s", posted)
	}
}
