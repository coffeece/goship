package mcptools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// serversAPI is an organization with two database servers and two cloud
// accounts.
func serversAPI(t *testing.T, record func(r *http.Request, body string)) string {
	t.Helper()
	return api(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		record(r, string(b))
		switch r.URL.Path {
		case "/api/v1/orgs/acme/database-servers":
			if r.Method == http.MethodGet {
				w.Write([]byte(`[{"id":"srv-prod","name":"pg-prod","status":"ready"},{"id":"srv-staging","name":"pg-staging","status":"ready"}]`)) //nolint:errcheck
				return
			}
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":"srv-new","name":"pg-gcp","status":"provisioning"}`)) //nolint:errcheck
		case "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-gcp","provider":"gcp","label":"gcp"},{"id":"acc-aws","provider":"aws","label":"aws"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/cloud-accounts/acc-gcp/regions", "/api/v1/orgs/acme/cloud-accounts/acc-gcp/sizes", "/api/v1/orgs/acme/database-server-tiers":
			w.Write([]byte(`[]`)) //nolint:errcheck
		default:
			w.Write([]byte(`{}`)) //nolint:errcheck
		}
	})
}

func TestCreateDatabaseOnAServerSendsItsID(t *testing.T) {
	var posted string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/orgs/acme/projects/atlas/environments/development/services/postgresql/instances" {
			posted = body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_create_database", map[string]any{"name": "db", "plan": "db-small", "server": "pg-prod"})
	if res.IsError {
		t.Fatal(text(res))
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(posted), &body); err != nil || body["server_id"] != "srv-prod" {
		t.Errorf("posted %s: %v", posted, err)
	}

	res = call(t, cs, "goship_create_database", map[string]any{"name": "db", "plan": "db-small", "server": "nope"})
	if !res.IsError || !strings.Contains(text(res), "goship_list_database_servers") {
		t.Errorf("unknown server: isError=%v %q", res.IsError, text(res))
	}
}

// "goship" names GoShip's PostgreSQL: the API resolves it, so the tool sends it
// as it is, never looks it up among the organization's servers, and still
// needs the plan GoShip bills by.
func TestCreateDatabaseOnTheGoShipServerSendsTheWordUnresolved(t *testing.T) {
	var posted string
	var paths []string
	url := serversAPI(t, func(r *http.Request, body string) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/instances") {
			posted = body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_create_database", map[string]any{"name": "db", "plan": "db-small", "server": "goship"})
	if res.IsError {
		t.Fatal(text(res))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(posted), &body); err != nil || body["server_id"] != "goship" || body["plan"] != "db-small" {
		t.Errorf("posted %s: %v", posted, err)
	}
	if len(paths) != 1 {
		t.Errorf("called %v, want only the POST", paths)
	}
	if !strings.Contains(text(res), "Database db is being created.") {
		t.Errorf("result = %q", text(res))
	}

	for _, args := range []map[string]any{
		{"name": "db", "server": "goship"},
		{"name": "db", "plan": "db-small", "server": "goship", "size_limit_mb": 500},
	} {
		if res := call(t, cs, "goship_create_database", args); !res.IsError {
			t.Errorf("%v should be refused", args)
		}
	}

	desc := properties(t, toolsByName(t, Options{})["goship_create_database"])["server"]
	if !strings.Contains(desc, "goship") {
		t.Errorf("the server argument does not mention goship: %q", desc)
	}
}

// Cloud accounts and database servers belong to the organization: their
// tools resolve no project, and every path they call is the organization's.
func TestCloudToolsAreOrgLevel(t *testing.T) {
	var paths []string
	url := serversAPI(t, func(r *http.Request, _ string) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
	})
	// No project is selected, so a tool that asked for one would fail.
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"goship_list_cloud_accounts", nil},
		{"goship_list_cloud_regions", map[string]any{"account_id": "acc-gcp"}},
		{"goship_list_cloud_sizes", map[string]any{"account_id": "acc-gcp", "region": "r"}},
		{"goship_delete_cloud_account", map[string]any{"account_id": "acc-gcp"}},
		{"goship_list_database_servers", nil},
		{"goship_list_database_server_tiers", nil},
		{"goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "r"}},
		{"goship_set_database_server_disk_limit", map[string]any{"server": "pg-prod", "disk_limit_gb": 50}},
		{"goship_retry_database_server", map[string]any{"server": "pg-prod"}},
		{"goship_delete_database_server", map[string]any{"server": "pg-prod"}},
	} {
		paths = nil
		res := call(t, cs, c.tool, c.args)
		if res.IsError {
			t.Errorf("%s: %s", c.tool, text(res))
			continue
		}
		if len(paths) == 0 {
			t.Errorf("%s called nothing", c.tool)
		}
		for _, p := range paths {
			if !strings.Contains(p, " /api/v1/orgs/acme/") || strings.Contains(p, "/projects/") {
				t.Errorf("%s called %s", c.tool, p)
			}
		}
	}
	for name, tl := range toolsByName(t, Options{}) {
		if strings.Contains(name, "cloud") || strings.Contains(name, "database_server") {
			if _, ok := properties(t, tl)["project"]; ok {
				t.Errorf("%s takes a project", name)
			}
		}
	}
}

func TestDatabaseServerToolsResolveTheName(t *testing.T) {
	var paths []string
	url := serversAPI(t, func(r *http.Request, _ string) {
		if r.Method != http.MethodGet {
			paths = append(paths, r.Method+" "+r.URL.RequestURI())
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})

	res := call(t, cs, "goship_list_database_servers", nil)
	if res.IsError {
		t.Fatal(text(res))
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"pg-staging"`) {
		t.Errorf("listing %s", raw)
	}

	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"goship_retry_database_server", map[string]any{"server": "pg-prod"}, "POST /api/v1/orgs/acme/database-servers/srv-prod/retry"},
		{"goship_retry_database_server", map[string]any{"server": "srv-staging"}, "POST /api/v1/orgs/acme/database-servers/srv-staging/retry"},
		{"goship_delete_database_server", map[string]any{"server": "pg-staging"}, "DELETE /api/v1/orgs/acme/database-servers/srv-staging"},
		{"goship_delete_database_server", map[string]any{"server": "pg-prod", "force": true}, "DELETE /api/v1/orgs/acme/database-servers/srv-prod?force=true"},
		{"goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "southamerica-east1"}, "POST /api/v1/orgs/acme/database-servers"},
		{"goship_create_database_server", map[string]any{"cloud_account": "acc-aws", "region": "us-east-1", "tier": "db-g1-small", "name": "pg-aws"}, "POST /api/v1/orgs/acme/database-servers"},
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "southamerica-east1"})
	if res.IsError {
		t.Fatal(text(res))
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(posted), &body); err != nil || body["cloud_account_id"] != "acc-gcp" || body["name"] != "pg-gcp" || body["tier"] != "db-f1-micro" {
		t.Errorf("posted %s: %v", posted, err)
	}
}

func TestDisableDatabaseExtensionDeletesIt(t *testing.T) {
	var got string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_disable_database_extension", map[string]any{"database": "db", "extension": "postgis"})
	if res.IsError || got != "DELETE /api/v1/orgs/acme/projects/atlas/environments/development/databases/db/extensions/postgis" {
		t.Errorf("isError=%v %q called %s", res.IsError, text(res), got)
	}
}

func TestSetDatabaseServerDiskLimitResolvesTheName(t *testing.T) {
	var calls []string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPut {
			calls = append(calls, r.URL.Path+" "+body)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"server": "pg-prod", "disk_limit_gb": 200}, `/api/v1/orgs/acme/database-servers/srv-prod/disk-limit {"disk_limit_gb":200}`},
		{map[string]any{"server": "srv-staging", "disk_limit_gb": 0}, `/api/v1/orgs/acme/database-servers/srv-staging/disk-limit {"disk_limit_gb":0}`},
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
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
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	if res := call(t, cs, "goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "r", "disk_limit_gb": 50}); res.IsError {
		t.Fatal(text(res))
	}
	if !strings.Contains(posted, `"disk_limit_gb":50`) {
		t.Errorf("posted %s", posted)
	}
}

func TestCreateDatabaseServerSendsTheEnvironment(t *testing.T) {
	var posted string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost {
			posted = body
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_create_database_server", map[string]any{"cloud_account": "gcp", "region": "southamerica-east1", "environment": "production"})
	if res.IsError {
		t.Fatal(text(res))
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(posted), &body); err != nil || body["environment"] != "production" || body["name"] != "pg-production" {
		t.Errorf("posted %s: %v", posted, err)
	}
}

func TestBindDatabaseServerResolvesTheNameAndSendsTheEnvironment(t *testing.T) {
	var posted, path string
	url := serversAPI(t, func(r *http.Request, body string) {
		if r.Method == http.MethodPost {
			posted, path = body, r.URL.Path
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_bind_database_server", map[string]any{"server": "pg-prod", "environment": "production"})
	if res.IsError {
		t.Fatal(text(res))
	}
	if path != "/api/v1/orgs/acme/database-servers/srv-prod/environment" || !strings.Contains(posted, `"environment":"production"`) {
		t.Errorf("posted %s to %s", posted, path)
	}
}

func TestEnablePITRPostsForTheResolvedServer(t *testing.T) {
	var path string
	url := serversAPI(t, func(r *http.Request, _ string) {
		if r.Method == http.MethodPost {
			path = r.URL.Path
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_enable_database_server_pitr", map[string]any{"server": "pg-staging"})
	if res.IsError {
		t.Fatal(text(res))
	}
	if path != "/api/v1/orgs/acme/database-servers/srv-staging/point-in-time-recovery" {
		t.Errorf("posted to %s", path)
	}
}
