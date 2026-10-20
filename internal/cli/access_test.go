package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAppCreatePrivateSendsTheFlag(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.Write([]byte(`{"name":"api"}`))     //nolint:errcheck
	})
	if _, err := run(t, "", "app", "create", "api", "--platform", "go", "--private"); err != nil {
		t.Fatal(err)
	}
	if body["private"] != true {
		t.Errorf("body = %v", body)
	}
}

func TestAppVisibilityPutsTheValue(t *testing.T) {
	var method, path string
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"name":"api","deploys":0}`)) //nolint:errcheck
			return
		}
		method, path = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.WriteHeader(http.StatusNoContent)
	})
	out, err := run(t, "", "app", "visibility", "api", "private")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/api/v1/orgs/acme/projects/clave/apps/api/visibility" || body["visibility"] != "private" {
		t.Errorf("got %s %s %v", method, path, body)
	}
	if !strings.Contains(out, "App api is now private.") || !strings.Contains(out, "first deploy") {
		t.Errorf("output = %q", out)
	}
}

func TestAppVisibilityPublicOnNewAppSkipsFirstDeployHint(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"name":"api","deploys":0}`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	out, err := run(t, "", "app", "visibility", "api", "public")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "App api is now public.") || strings.Contains(out, "first deploy") {
		t.Errorf("output = %q", out)
	}
}

func TestAppVisibilityRefusesOtherValues(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("reached the API: %s", r.URL.Path)
	})
	_, err := run(t, "", "app", "visibility", "api", "secret")
	if err == nil || !strings.Contains(err.Error(), "visibility must be public or private") {
		t.Errorf("err = %v", err)
	}
}

func TestAccessAddPrintsTheGeneratedPasswordOnce(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"username":"ana","password":"s3cret-pw"}`)) //nolint:errcheck
	})
	out, err := run(t, "", "app", "access", "add", "api", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "s3cret-pw") != 1 || !strings.Contains(out, "ana") {
		t.Errorf("output = %q", out)
	}
}

func TestAccessAddWithGivenPasswordPrintsNoPassword(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"username":"ana"}`)) //nolint:errcheck
	})
	out, err := run(t, "", "app", "access", "add", "api", "ana", "--password", "mine")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Access user ana added.") {
		t.Errorf("output = %q", out)
	}
}

func TestAccessListRendersUsers(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"users":[{"username":"ana","created_at":"2026-10-19T10:00:00Z"}]}`)) //nolint:errcheck
	})
	out, err := run(t, "", "app", "access", "list", "api")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "USERNAME") || !strings.Contains(out, "ana") {
		t.Errorf("output = %q", out)
	}
}

func TestAccessRmConfirms(t *testing.T) {
	var method string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	if _, err := run(t, "y\n", "app", "access", "rm", "api", "ana"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %q", method)
	}
}

func TestAccessTokenRevealHitsTheQuery(t *testing.T) {
	var query string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(`{"token":"tok-123"}`)) //nolint:errcheck
	})
	out, err := run(t, "", "app", "access", "token", "api", "--reveal")
	if err != nil {
		t.Fatal(err)
	}
	if query != "reveal=1" || !strings.Contains(out, "tok-123") {
		t.Errorf("query = %q, output = %q", query, out)
	}
}

func TestAccessTokenRotatePosts(t *testing.T) {
	var method, path string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Write([]byte(`{"token":"new-tok"}`)) //nolint:errcheck
	})
	out, err := run(t, "y\n", "app", "access", "token", "api", "--rotate")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || !strings.HasSuffix(path, "/access/token/rotate") || !strings.Contains(out, "new-tok") {
		t.Errorf("got %s %s %q", method, path, out)
	}
}
