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
	if path != "/api/v1/orgs/acme/projects/atlas/environments/production/apps/blog/logs" || !strings.Contains(query, "lines=5") {
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

// The platform runs the command through a shell, so arguments the local shell
// already split must keep their boundaries: `-- sh -c 'echo ok'` ran `sh -c echo
// ok` and printed an empty line.
func TestRunKeepsEachArgumentWhole(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)                 //nolint:errcheck
		w.Write([]byte(`{"type":"result","ok":true}` + "\n")) //nolint:errcheck
	})

	if _, err := run(t, "", "run", "-a", "blog", "--", "sh", "-c", "echo it's ok", ""); err != nil {
		t.Fatal(err)
	}
	if want := `sh -c 'echo it'\''s ok' ''`; body["command"] != want {
		t.Errorf("command = %v, want %s", body["command"], want)
	}
}

// A single argument is a shell line the user wrote for the app's shell.
func TestRunPassesASingleArgumentAsAShellLine(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)                 //nolint:errcheck
		w.Write([]byte(`{"type":"result","ok":true}` + "\n")) //nolint:errcheck
	})

	if _, err := run(t, "", "run", "-a", "blog", "cd tmp && ls | wc -l"); err != nil {
		t.Fatal(err)
	}
	if body["command"] != "cd tmp && ls | wc -l" {
		t.Errorf("command = %v", body["command"])
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
	if path != "/api/v1/orgs/acme/projects/atlas/environments/production/apps/blog/shell" || auth != "Bearer gsp_test" {
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

func TestFailedRollbackPointsAtTheHistory(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"type":"step","step":"release","state":"start"}
{"type":"result","ok":false,"error":"deploy failed: timeout"}
`))
	})

	_, err := run(t, "", "rollback", "v3", "-a", "blog")
	if err == nil || !strings.Contains(err.Error(), "History: goship releases -a blog") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "goship rollback") {
		t.Errorf("a production rollback should not be suggested again: %v", err)
	}
}

func TestRefusedRollbackDoesNotPointAnywhere(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"app is paused"}`)) //nolint:errcheck
	})

	_, err := run(t, "", "rollback", "v3", "-a", "blog")
	if err == nil || strings.Contains(err.Error(), "History:") || strings.Contains(err.Error(), "Full log") {
		t.Errorf("error = %v", err)
	}
}

// `releases log` finds the release in the history and prints what it wrote:
// the log alone on stdout, what it belongs to on stderr.
func TestReleasesLogPrintsTheStoredLog(t *testing.T) {
	var detailPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deploys") {
			w.Write([]byte(`[{"id":"evt-9","version":0,"error":"build failed"},{"id":"evt-8","version":8},{"id":"evt-7","version":7}]`)) //nolint:errcheck
			return
		}
		detailPath = r.URL.Path
		w.Write([]byte(`{"id":"evt-8","version":8,"log":"---> building\n---> done\n","log_available":true,"log_truncated":true}`)) //nolint:errcheck
	})

	out, err := run(t, "", "releases", "log", "v8", "-a", "blog")
	if err != nil {
		t.Fatal(err)
	}
	if detailPath != "/api/v1/orgs/acme/projects/atlas/environments/production/apps/blog/deploys/evt-8" {
		t.Errorf("read %q", detailPath)
	}
	// Standard output carries the log and nothing else, so it can be piped.
	if out != "---> building\n---> done\n" {
		t.Errorf("stdout = %q", out)
	}
}

// With no version it reads the newest release, which right after a failed
// deploy is that deploy.
func TestReleasesLogDefaultsToTheNewestRelease(t *testing.T) {
	var detailPath string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deploys") {
			w.Write([]byte(`[{"id":"evt-9","version":0,"error":"build failed"},{"id":"evt-8","version":8}]`)) //nolint:errcheck
			return
		}
		detailPath = r.URL.Path
		w.Write([]byte(`{"id":"evt-9","error":"build failed","log":"main.go:12: undefined: foo\n","log_available":true}`)) //nolint:errcheck
	})

	out, err := run(t, "", "releases", "log", "-a", "blog")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(detailPath, "/deploys/evt-9") || !strings.Contains(out, "undefined: foo") {
		t.Errorf("read %q, output %q", detailPath, out)
	}
}

// A release without a kept log, and a version that is not in the history,
// each say what is wrong.
func TestReleasesLogSaysWhenThereIsNothingToPrint(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deploys") {
			w.Write([]byte(`[{"id":"evt-8","version":8}]`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"id":"evt-8","version":8,"log_available":false}`)) //nolint:errcheck
	})

	if _, err := run(t, "", "releases", "log", "8", "-a", "blog"); err == nil || !strings.Contains(err.Error(), "no log was kept for v8 of blog") {
		t.Errorf("no log: err = %v", err)
	}
	if _, err := run(t, "", "releases", "log", "v99", "-a", "blog"); err == nil || !strings.Contains(err.Error(), "no release v99") {
		t.Errorf("unknown version: err = %v", err)
	}
}

// A build can write escape sequences; they must not reach the terminal of
// whoever reads the stored log later.
func TestReleasesLogStripsTerminalEscapes(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deploys") {
			w.Write([]byte(`[{"id":"evt-8","version":8}]`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"id":"evt-8","version":8,"log":"\u001b[32mok\u001b[0m\n\u001b]0;owned\u0007title\nbell\u0007 end","log_available":true}`)) //nolint:errcheck
	})

	out, err := run(t, "", "releases", "log", "-a", "blog")
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok\ntitle\nbell end" {
		t.Errorf("stdout = %q", out)
	}
}

func TestReleasesLogShowsPhaseTimingsOnStderr(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deploys") {
			w.Write([]byte(`[{"id":"evt-8","version":8}]`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"id":"evt-8","version":8,"log":"ok\n","log_available":true,"phases":{"build":141.2,"build.script":124.8,"build.script.compile":100,"build.export":11.7,"release":38,"route":1.2}}`)) //nolint:errcheck
	})

	var out, errOut strings.Builder
	root := NewRoot("test")
	root.SetArgs([]string{"releases", "log", "-a", "blog"})
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ok\n" {
		t.Errorf("stdout = %q", out.String())
	}
	want := "Timings\n" +
		"  Build                 2m 21s\n" +
		"    Build script        2m 5s\n" +
		"      Compile and link  1m 40s\n" +
		"    Image export        11.7s\n" +
		"  Release               38.0s\n" +
		"  Route                 1.2s\n"
	if !strings.Contains(errOut.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", errOut.String(), want)
	}
}

func TestReleasesLogOmitsTimingsWithoutPhases(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deploys") {
			w.Write([]byte(`[{"id":"evt-8","version":8}]`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"id":"evt-8","version":8,"log":"ok\n","log_available":true,"phases":{}}`)) //nolint:errcheck
	})

	var errOut strings.Builder
	root := NewRoot("test")
	root.SetArgs([]string{"releases", "log", "-a", "blog"})
	root.SetOut(&strings.Builder{})
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut.String(), "Timings") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

// Timings are not log content: a release whose log is restricted or was never
// kept still shows them, and the command still fails.
func TestReleasesLogShowsTimingsWhenTheLogIsNotAvailable(t *testing.T) {
	for name, body := range map[string]string{
		"restricted": `{"id":"evt-8","version":8,"log_restricted":true,"phases":{"build":20,"build.script.hooks":2,"release":3}}`,
		"not kept":   `{"id":"evt-8","version":8,"log_available":false,"phases":{"build":20,"build.script.hooks":2,"release":3}}`,
		"no timings": `{"id":"evt-8","version":8,"log_restricted":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/deploys") {
					w.Write([]byte(`[{"id":"evt-8","version":8}]`)) //nolint:errcheck
					return
				}
				w.Write([]byte(body)) //nolint:errcheck
			})
			var out, errOut strings.Builder
			root := NewRoot("test")
			root.SetArgs([]string{"releases", "log", "-a", "blog"})
			root.SetOut(&out)
			root.SetErr(&errOut)
			err := root.Execute()
			if err == nil {
				t.Fatal("want the log error")
			}
			if out.String() != "" {
				t.Errorf("stdout = %q", out.String())
			}
			has := strings.Contains(errOut.String(), "Timings") && strings.Contains(errOut.String(), "Hooks and other")
			if name == "no timings" && has || name != "no timings" && !has {
				t.Errorf("stderr = %q", errOut.String())
			}
		})
	}
}

func TestPhaseDuration(t *testing.T) {
	for in, want := range map[float64]string{0: "0.0s", 59.9: "59.9s", 59.96: "1m 0s", 60: "1m 0s", 119.6: "2m 0s"} {
		if got := phaseDuration(in); got != want {
			t.Errorf("phaseDuration(%v) = %q, want %q", in, got, want)
		}
	}
}
