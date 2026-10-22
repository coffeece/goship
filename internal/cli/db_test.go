package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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
	if _, err := run(t, "n\n", "db", "server", "delete", "pg"); err == nil || deleted != "" {
		t.Fatalf("delete without consent: %v, deleted %q; want it aborted", err, deleted)
	}
	out, err = run(t, "", "db", "server", "delete", "pg", "-y")
	if err != nil || deleted != "s1" {
		t.Fatalf("delete -y: %v, deleted %q", err, deleted)
	}
	if !strings.Contains(out, "is being removed from your project") {
		t.Errorf("delete output = %q", out)
	}
}

func TestDBServerDeleteForceForgets(t *testing.T) {
	var deleted string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/clave/database-servers":
			w.Write([]byte(`[{"id":"s1","name":"pg","status":"error","cloud_instance_name":"goship-abc","cloud_account":{"label":"acme-prod"},"scope":"org"}]`)) //nolint:errcheck
		case "DELETE /api/v1/orgs/acme/database-servers/s1":
			deleted = r.URL.RawQuery
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if _, err := run(t, "n\n", "db", "server", "delete", "pg", "--force"); err == nil || deleted != "" {
		t.Fatalf("force without consent: %v, deleted %q; want it aborted", err, deleted)
	}
	out, err := run(t, "", "db", "server", "delete", "pg", "--force", "-y")
	if err != nil || deleted != "force=true" {
		t.Fatalf("delete --force: %v, query %q; want force=true", err, deleted)
	}
	if !strings.Contains(out, "forgotten") || !strings.Contains(out, "gcloud sql instances delete goship-abc --project acme-prod") {
		t.Errorf("force output = %q, want the instance and the gcloud command", out)
	}
}

func TestDBServerAmbiguousNameIsRefused(t *testing.T) {
	var deleted, body string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/clave/database-servers":
			w.Write([]byte(`[{"id":"s1","name":"pg","status":"active","scope":"org"},{"id":"s2","name":"pg","status":"active","scope":"project"}]`)) //nolint:errcheck
		case "DELETE /api/v1/orgs/acme/database-servers/s1", "DELETE /api/v1/orgs/acme/projects/clave/database-servers/s1",
			"DELETE /api/v1/orgs/acme/database-servers/s2", "DELETE /api/v1/orgs/acme/projects/clave/database-servers/s2":
			deleted = r.URL.Path
			w.WriteHeader(http.StatusAccepted)
		case "POST /api/v1/orgs/acme/projects/clave/services/postgresql/instances":
			body = "posted"
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	_, err := run(t, "", "db", "server", "delete", "pg", "-y")
	if err == nil || !strings.Contains(err.Error(), `more than one database server named "pg"`) || !strings.Contains(err.Error(), "pass the id") || deleted != "" {
		t.Fatalf("ambiguous delete: %v, deleted %q", err, deleted)
	}
	if _, err := run(t, "", "db", "create", "shop", "--plan", "db-small", "--server", "pg"); err == nil || body != "" {
		t.Fatalf("ambiguous --server: %v, posted %q", err, body)
	}
	if _, err := run(t, "", "db", "server", "delete", "s2", "-y"); err != nil || deleted != "/api/v1/orgs/acme/projects/clave/database-servers/s2" {
		t.Fatalf("delete by id: %v, deleted %q", err, deleted)
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
	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), "db", "create", "shop", "--plan", "db-small", "--server", "pg")
	if err != nil {
		t.Fatal(err)
	}
	if body["server_id"] != "s1" {
		t.Fatalf("body = %v", body)
	}
	if _, sent := body["plan"]; sent {
		t.Fatalf("plan sent with a server: %v", body)
	}
	if !strings.Contains(out, "Database shop requested on server pg.") || !strings.Contains(stderr, "note: databases on your own server have no plan; --plan ignored") {
		t.Fatalf("out = %q, stderr = %q", out, stderr)
	}
}

func stubServerCreate(t *testing.T, body *map[string]any) {
	t.Helper()
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/clave/database-servers":
			w.Write([]byte(`[{"id":"s1","name":"pg","status":"active"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/projects/clave/services/postgresql/instances":
			json.NewDecoder(r.Body).Decode(body) //nolint:errcheck
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestDBCreateOnServerNeedsNoPlan(t *testing.T) {
	var body map[string]any
	stubServerCreate(t, &body)
	out, err := run(t, "", "db", "create", "shop", "--server", "pg")
	if err != nil || !strings.Contains(out, "Database shop requested on server pg.") || strings.Contains(out, "note:") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if _, sent := body["plan"]; sent || body["server_id"] != "s1" {
		t.Fatalf("body = %v", body)
	}
}

func TestDBCreateWithoutServerNeedsPlan(t *testing.T) {
	var body map[string]any
	stubServerCreate(t, &body)
	if _, err := run(t, "", "db", "create", "shop"); err == nil || !strings.Contains(err.Error(), "--plan is required") || body != nil {
		t.Fatalf("err = %v, body = %v", err, body)
	}
	out, err := run(t, "", "db", "create", "shop", "--plan", "db-small")
	if err != nil || !strings.Contains(out, "Database shop requested on plan db-small.") || body["plan"] != "db-small" {
		t.Fatalf("out = %q, err = %v, body = %v", out, err, body)
	}
}

func TestDBCreateSendsSizeLimit(t *testing.T) {
	var body map[string]any
	stubServerCreate(t, &body)
	if _, err := run(t, "", "db", "create", "shop", "--server", "pg", "--size-limit", "2GB"); err != nil {
		t.Fatal(err)
	}
	if body["size_limit_mb"] != float64(2048) {
		t.Fatalf("body = %v", body)
	}
	if _, err := run(t, "", "db", "create", "shop", "--server", "pg", "--size-limit", "5"); err == nil {
		t.Fatal("a limit under 10 MB was accepted")
	}
}

func TestDBSizeLimitSetsAndClears(t *testing.T) {
	var raw string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/orgs/acme/projects/clave/databases/shop/size-limit" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		raw = strings.TrimSpace(string(b))
		w.Write([]byte(`{"message":"Size limit saved; it applies within 5 minutes."}`)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "size-limit", "shop", "500MB")
	if err != nil || raw != `{"size_limit_mb":500}` || !strings.Contains(out, "applies within 5 minutes") {
		t.Fatalf("set: out = %q, err = %v, body = %s", out, err, raw)
	}
	if _, err := run(t, "", "db", "size-limit", "shop", "none"); err != nil || raw != `{"size_limit_mb":null}` {
		t.Fatalf("clear: err = %v, body = %s", err, raw)
	}
}

func TestDBServerDiskLimit(t *testing.T) {
	var path, raw string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/clave/database-servers":
			w.Write([]byte(`[{"id":"s1","name":"pg","status":"active","scope":"org"}]`)) //nolint:errcheck
		case "PUT /api/v1/orgs/acme/database-servers/s1/disk-limit":
			path = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			raw = strings.TrimSpace(string(b))
			w.Write([]byte(`{"id":"s1"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := run(t, "", "db", "server", "disk-limit", "pg", "200")
	if err != nil || path == "" || raw != `{"disk_limit_gb":200}` || !strings.Contains(out, "200 GB") {
		t.Fatalf("out = %q, err = %v, body = %s", out, err, raw)
	}
	if _, err := run(t, "", "db", "server", "disk-limit", "s1", "none"); err != nil || raw != `{"disk_limit_gb":0}` {
		t.Fatalf("none: err = %v, body = %s", err, raw)
	}
	if _, err := run(t, "", "db", "server", "disk-limit", "pg", "lots"); err == nil {
		t.Fatal("bad cap accepted")
	}
}

func TestDBServerCreateDiskLimit(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/cloud-accounts", "GET /api/v1/orgs/acme/projects/clave/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","label":"prod","provider":"gcp","scope":"org"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/database-servers":
			body = map[string]any{}
			json.NewDecoder(r.Body).Decode(&body)                               //nolint:errcheck
			w.Write([]byte(`{"id":"s1","name":"pg-prod","status":"creating"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if _, err := run(t, "", "db", "server", "create", "--account", "gcp", "--region", "r1", "--disk-limit", "none"); err != nil {
		t.Fatal(err)
	}
	if v, ok := body["disk_limit_gb"]; !ok || v != float64(0) {
		t.Fatalf("none: body = %v", body)
	}
	if _, err := run(t, "", "db", "server", "create", "--account", "gcp", "--region", "r1", "--disk-limit", "50GB"); err != nil || body["disk_limit_gb"] != float64(50) {
		t.Fatalf("50GB: err = %v, body = %v", err, body)
	}
	if _, err := run(t, "", "db", "server", "create", "--account", "gcp", "--region", "r1"); err != nil {
		t.Fatal(err)
	}
	if _, sent := body["disk_limit_gb"]; sent {
		t.Fatalf("default sent a cap: %v", body)
	}
}

func TestDBInfoOnOwnServerShowsLimits(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects/clave/databases/shop":
			w.Write([]byte(`{"name":"shop","status":"active","placement":"pg"}`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/clave/databases/shop/stats":
			w.Write([]byte(`{"size_mb":"12","connections":2,"max_size_mb":2048,"max_connections":25,"server_connections":9}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := run(t, "", "db", "info", "shop")
	if err != nil || !strings.Contains(out, "size limit: 2048 MB") || !strings.Contains(out, "server connections: 9 of 25") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}

const extensionsJSON = `{"enabled":[{"name":"postgis","version":"3.6.4"}],"available":[{"name":"pg_trgm","version":"1.6","comment":"text similarity"}],"pending":["citext"]}`

func TestDBExtensionsListsEnabledPendingAndAvailable(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/orgs/acme/projects/clave/databases/shop/extensions" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(extensionsJSON)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "extensions", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "VERSION", "STATUS", "postgis", "3.6.4", "enabled", "citext", "enabling", "pg_trgm", "available"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestDBExtensionAddPollsUntilEnabled(t *testing.T) {
	saved, savedDeadline := extensionPollInterval, extensionPollDeadline
	extensionPollInterval, extensionPollDeadline = 0, 5*time.Second
	t.Cleanup(func() { extensionPollInterval, extensionPollDeadline = saved, savedDeadline })

	gets := 0
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"name":"postgis","status":"enabling"}`)) //nolint:errcheck
		case http.MethodGet:
			gets++
			if gets == 1 {
				w.Write([]byte(`{"enabled":[],"available":[],"pending":["postgis"]}`)) //nolint:errcheck
				return
			}
			w.Write([]byte(`{"enabled":[{"name":"postgis","version":"3.6.4"}],"available":[],"pending":[]}`)) //nolint:errcheck
		}
	})
	out, err := run(t, "", "db", "extension-add", "shop", "postgis")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Extension postgis 3.6.4 enabled on shop.") || gets < 2 {
		t.Fatalf("gets=%d out=%q", gets, out)
	}
}

func TestDBExtensionAddGivesUpPolitely(t *testing.T) {
	saved, savedDeadline := extensionPollInterval, extensionPollDeadline
	extensionPollInterval, extensionPollDeadline = 0, 0
	t.Cleanup(func() { extensionPollInterval, extensionPollDeadline = saved, savedDeadline })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"name":"postgis","status":"enabling"}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"enabled":[],"available":[],"pending":["postgis"]}`)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "extension-add", "shop", "postgis")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Still enabling") {
		t.Fatalf("out=%q", out)
	}
}

func TestDBExtensionAddPrintsTheEnabledLine(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"name":"pg_trgm","version":"1.6","status":"enabled"}`)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "extension-add", "shop", "pg_trgm")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Extension pg_trgm 1.6 enabled on shop.") {
		t.Fatalf("out=%q", out)
	}
}

func TestDBExtensionRm(t *testing.T) {
	var got string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		w.Write([]byte(`{"status":"disabled"}`)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "extension-rm", "shop", "pg_trgm")
	if err != nil {
		t.Fatal(err)
	}
	if got != "DELETE /api/v1/orgs/acme/projects/clave/databases/shop/extensions/pg_trgm" || !strings.Contains(out, "Extension pg_trgm disabled on shop.") {
		t.Fatalf("got=%q out=%q", got, out)
	}
}

// Once the server drops the name from both lists the install failed; waiting
// the full deadline and then claiming it is "still enabling" would be false.
func TestDBExtensionAddReportsAFailedInstall(t *testing.T) {
	saved, savedDeadline := extensionPollInterval, extensionPollDeadline
	extensionPollInterval, extensionPollDeadline = 0, 5*time.Second
	t.Cleanup(func() { extensionPollInterval, extensionPollDeadline = saved, savedDeadline })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"name":"postgis","status":"enabling"}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"enabled":[],"available":[{"name":"postgis","version":"3.6.4","comment":"geo"}],"pending":[]}`)) //nolint:errcheck
	})
	out, err := run(t, "", "db", "extension-add", "shop", "postgis")
	if err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("err = %v, out = %q; want a 'did not complete' error", err, out)
	}
	if strings.Contains(out, "Still enabling") {
		t.Fatalf("must not claim it is still enabling: %q", out)
	}
}
