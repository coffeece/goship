package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestLogsPrintsEachLineWithItsSourceAndUnit(t *testing.T) {
	var path, query string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		w.Write([]byte(`{"type":"log","date":"2026-09-19T10:00:00Z","source":"web","unit":"blog-web-1","message":"listening on :8888\n"}` + "\n" + `{"type":"result","ok":true}` + "\n")) //nolint:errcheck
	})

	out, err := run(t, "", "logs", "-a", "blog", "-l", "5", "--no-date")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/orgs/acme/apps/blog/logs" || !strings.Contains(query, "lines=5") {
		t.Errorf("request = %s?%s", path, query)
	}
	if out != "[web][blog-web-1]: listening on :8888\n" {
		t.Errorf("output = %q", out)
	}
}

func TestRunSendsTheCommandAsTyped(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)                                                                  //nolint:errcheck
		w.Write([]byte(`{"type":"output","data":"migrated\n"}` + "\n" + `{"type":"result","ok":true}` + "\n")) //nolint:errcheck
	})

	out, err := run(t, "", "run", "-a", "blog", "--once", "--", "rake", "db:migrate", "--trace")
	if err != nil {
		t.Fatal(err)
	}
	if body["command"] != "rake db:migrate --trace" || body["once"] != true {
		t.Errorf("body = %v", body)
	}
	if out != "migrated\n" {
		t.Errorf("output = %q", out)
	}
}

func TestRunExitsNonZeroWhenTheCommandFails(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"type":"result","ok":false,"error":"command failed: command terminated with exit code 2"}` + "\n")) //nolint:errcheck
	})

	_, err := run(t, "", "run", "-a", "blog", "--", "false")
	if err == nil || !strings.Contains(err.Error(), "command failed: command terminated with exit code 2") {
		t.Errorf("error = %v", err)
	}
}

// `goship releases` prints 12; the API wants v12. Both must work.
func TestRollbackAcceptsABareVersionNumber(t *testing.T) {
	for _, typed := range []string{"12", "v12"} {
		var body map[string]any
		stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&body)                 //nolint:errcheck
			w.Write([]byte(`{"type":"result","ok":true}` + "\n")) //nolint:errcheck
		})
		if _, err := run(t, "", "rollback", typed, "-a", "blog"); err != nil {
			t.Fatal(err)
		}
		if body["version"] != "v12" {
			t.Errorf("typed %q: version = %v, want v12", typed, body["version"])
		}
	}
}

func TestReleasesListsTheHistory(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"version":3,"origin":"app-deploy","user":"dev@acme.test","message":"fix","can_rollback":true}]`)) //nolint:errcheck
	})

	out, err := run(t, "", "releases", "-a", "blog")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"3", "dev@acme.test", "fix"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

func TestShellSaysWhatToUseInstead(t *testing.T) {
	stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) { t.Error("shell must not call the API") })

	_, err := run(t, "", "shell", "-a", "blog")
	if err == nil || !strings.Contains(err.Error(), "goship run") {
		t.Errorf("error = %v, want a pointer to `goship run`", err)
	}
}

func TestStreamedCommandsRefuseJSONOutput(t *testing.T) {
	stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) { t.Error("must be refused before any request") })

	if _, err := run(t, "", "logs", "-a", "blog", "--output", "json"); err == nil {
		t.Error("logs accepted --output json")
	}
}
