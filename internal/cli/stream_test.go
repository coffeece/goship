package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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

func TestRollbackShowsItsSteps(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"type":"step","step":"release","state":"start"}
{"type":"output","step":"release","data":" ---> All units ready\n"}
{"type":"step","step":"release","state":"done","detail":"1/1 units healthy"}
{"type":"step","step":"route","state":"start"}
{"type":"result","ok":true}
`)) //nolint:errcheck
	})

	out, err := run(t, "", "rollback", "v3", "-a", "blog")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Rolling back blog to v3\n", "✓ Release 1/1 units healthy", "✓ Route"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "All units ready") {
		t.Errorf("the log leaked:\n%s", out)
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

// Piped input reaches the shell and its output comes back: the same path an
// interactive session takes, minus the raw terminal.
func TestShellPipesInputToTheUnitAndPrintsItsOutput(t *testing.T) {
	pipedShellGrace = 50 * time.Millisecond
	t.Cleanup(func() { pipedShellGrace = 60 * time.Second })

	var path, query, auth string
	up := websocket.Upgrader{}
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		path, query, auth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()                                     //nolint:errcheck
		ws.WriteMessage(websocket.TextMessage, []byte("$ ")) //nolint:errcheck
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		ws.WriteMessage(websocket.TextMessage, []byte("ran: "+string(msg)))                                     //nolint:errcheck
		ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")) //nolint:errcheck
	})
	t.Setenv("GOSHIP_TOKEN", "gsp_test")

	out, err := run(t, "ls /app\n", "shell", "-a", "blog", "--isolated", "-u", "blog-web-1")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/orgs/acme/apps/blog/shell" || auth != "Bearer gsp_test" {
		t.Errorf("path=%q auth=%q", path, auth)
	}
	if !strings.Contains(query, "isolated=true") || !strings.Contains(query, "unit=blog-web-1") {
		t.Errorf("query = %q", query)
	}
	if out != "$ ran: ls /app\n" {
		t.Errorf("output = %q", out)
	}
}

func TestShellReportsARefusalFromTheAPI(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"data conflict: app \"blog\" is paused"}`)) //nolint:errcheck
	})

	_, err := run(t, "", "shell", "-a", "blog")
	if err == nil || !strings.Contains(err.Error(), "paused") {
		t.Errorf("error = %v, want the API's reason", err)
	}
}

func TestStreamedCommandsRefuseJSONOutput(t *testing.T) {
	stubAPIWithOrg(t, func(http.ResponseWriter, *http.Request) { t.Error("must be refused before any request") })

	if _, err := run(t, "", "logs", "-a", "blog", "--output", "json"); err == nil {
		t.Error("logs accepted --output json")
	}
}
