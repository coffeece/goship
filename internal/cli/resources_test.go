package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestDatabaseCommandsUseTheServiceInstanceEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		args               []string
	}{
		{"create", http.MethodPost, "/api/v1/orgs/acme/projects/clave/environments/production/services/postgresql/instances",
			[]string{"db", "create", "main", "--plan", "starter"}},
		{"delete", http.MethodDelete, "/api/v1/orgs/acme/projects/clave/environments/production/services/postgresql/instances/main",
			[]string{"db", "rm", "main", "--yes"}},
		{"bind", http.MethodPost, "/api/v1/orgs/acme/projects/clave/environments/production/services/postgresql/instances/main/bind",
			[]string{"db", "bind", "main", "-a", "api"}},
		{"unbind", http.MethodDelete, "/api/v1/orgs/acme/projects/clave/environments/production/services/postgresql/instances/main/bind/api",
			[]string{"db", "unbind", "main", "-a", "api"}},
		{"list", http.MethodGet, "/api/v1/orgs/acme/projects/clave/environments/production/databases",
			[]string{"dbs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotMethod string
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/instances") {
					w.WriteHeader(http.StatusCreated)
					w.Write([]byte(`{"status":"created"}`)) //nolint:errcheck
					return
				}
				w.Write([]byte(`[]`)) //nolint:errcheck
			})

			if _, err := run(t, "", tc.args...); err != nil {
				t.Fatal(err)
			}
			if gotPath != tc.path {
				t.Errorf("path = %q, want %q", gotPath, tc.path)
			}
			if gotMethod != tc.method {
				t.Errorf("method = %q, want %q", gotMethod, tc.method)
			}
		})
	}
}

// Volumes are the one noun that shows where it lives: the cluster is a column
// of the table and stays whole in JSON.
func TestVolumesShowTheCluster(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		volumes := `[` +
			`{"name":"uploads","plan":"volume-small","capacity_gib":5,"cluster":{"id":"c-home","name":"homolog","shared":false}},` +
			`{"name":"cache","plan":"volume-small","capacity_gib":1,"cluster":{"id":"c-sh","name":"GoShip","shared":true}}]`
		w.Write([]byte(volumes)) //nolint:errcheck
	})
	out, err := run(t, "", "volumes")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "CLUSTER") {
		t.Fatalf("want a CLUSTER column:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "uploads") || !strings.HasSuffix(lines[1], "homolog") ||
		!strings.HasPrefix(lines[2], "cache") || !strings.HasSuffix(lines[2], "GoShip") {
		t.Errorf("the cluster name closes each row:\n%s", out)
	}
	if strings.Contains(out, "c-home") || strings.Contains(out, "{") {
		t.Errorf("only the name is shown:\n%s", out)
	}

	out, err = run(t, "", "volumes", "--output", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got []struct {
		Cluster struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Shared bool   `json:"shared"`
		} `json:"cluster"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 || got[0].Cluster.ID != "c-home" || !got[1].Cluster.Shared {
		t.Errorf("json = %v\n%s", err, out)
	}
}

func TestVolumeBindSendsTheMountPoint(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	})

	if _, err := run(t, "", "volume", "bind", "uploads", "-a", "api", "--mount", "/data", "--read-only"); err != nil {
		t.Fatal(err)
	}
	if body["app"] != "api" || body["mount_point"] != "/data" || body["read_only"] != true {
		t.Errorf("body = %v", body)
	}
}

// Unbind is a POST, not a DELETE — the portal restarts the app as part of it.
func TestVolumeUnbindIsAPost(t *testing.T) {
	var gotMethod, gotPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
	})

	if _, err := run(t, "", "volume", "unbind", "uploads", "-a", "api", "--mount", "/data"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/orgs/acme/projects/clave/environments/production/volumes/uploads/unbind" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
}

func TestDomainAddTargetsTheApp(t *testing.T) {
	var gotPath string
	var body map[string]string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	})

	if _, err := run(t, "", "domain", "add", "shop.example.com", "-a", "api"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs/acme/projects/clave/environments/production/apps/api/domains" {
		t.Errorf("path = %q", gotPath)
	}
	if body["domain"] != "shop.example.com" {
		t.Errorf("body = %v", body)
	}
}

func TestDestructiveCommandsAllConfirm(t *testing.T) {
	for _, tc := range []struct {
		args []string
		// asks is what the question must name: where the thing lives.
		asks string
	}{
		{[]string{"app", "rm", "api"}, "acme/clave/production"},
		{[]string{"app", "access", "rm", "api", "ana"}, ""},
		{[]string{"app", "access", "token", "api", "--rotate"}, ""},
		{[]string{"db", "rm", "main"}, "acme/clave/production"},
		{[]string{"volume", "rm", "uploads"}, "acme/clave/production"},
		{[]string{"cluster", "rm", "n1"}, "acme"},
		{[]string{"cluster", "resize", "n1", "--size", "x"}, "acme"},
		{[]string{"environment", "rm", "staging"}, "acme"},
		{[]string{"cloud", "disconnect", "acme-do"}, ""},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				// Resolving a cluster's name is a read; it may happen before
				// the question. Anything else must not.
				if r.Method == http.MethodGet {
					w.Write([]byte(`[{"id":"n1","name":"n1"}]`)) //nolint:errcheck
					return
				}
				t.Errorf("%v reached the API without confirmation", tc.args)
			})
			_, stderr, err := runWithStderr(t, context.Background(), strings.NewReader("n\n"), tc.args...)
			if err == nil {
				t.Errorf("%v should abort when the answer is no", tc.args)
			}
			if !strings.Contains(stderr, tc.asks) {
				t.Errorf("%v asked %q, want it to name %q", tc.args, stderr, tc.asks)
			}
		})
	}
}

// Deleting names the environment it deleted from as well as the question did,
// so the transcript says what happened where.
func TestDeletesSayWhereTheyHappened(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"app", "rm", "api", "-y"}, "App api deleted from acme/clave/production."},
		{[]string{"db", "rm", "main", "-y"}, "Database main deleted from acme/clave/production."},
		{[]string{"volume", "rm", "uploads", "-y"}, "Volume uploads deleted from acme/clave/production."},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) {})
			out, err := run(t, "", tc.args...)
			if err != nil || !strings.Contains(out, tc.want) {
				t.Errorf("out = %q, %v; want %q", out, err, tc.want)
			}
		})
	}
}

// A question that does not name the environment is a question about the
// wrong thing when two environments hold an app of the same name.
func TestDeleteQuestionNamesTheChosenEnvironment(t *testing.T) {
	for _, args := range [][]string{
		{"app", "rm", "api"},
		{"db", "rm", "main"},
		{"volume", "rm", "uploads"},
	} {
		stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) {
			t.Error("reached the API without confirmation")
		})
		_, stderr, err := runWithStderr(t, context.Background(), strings.NewReader("n\n"), append(args, "--environment", "staging")...)
		if err == nil || !strings.Contains(stderr, "acme/clave/staging") || strings.Contains(stderr, "production") {
			t.Errorf("%v: asked %q, %v; want acme/clave/staging", args, stderr, err)
		}
	}
}
