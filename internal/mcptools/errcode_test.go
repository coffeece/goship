package mcptools

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// An agent branches on the API's code without parsing the message: it is
// always the last line of a failed tool's text, in one fixed form.
func TestAPIErrorCodeIsTheLastLine(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) //nolint:errcheck
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":"your trial covers up to 2 apps on GoShip's cluster; add a card to run more, billed from now","code":"trial_allowance_exceeded"}`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_create_app", map[string]any{"name": "api", "platform": "go", "plan": "app-small"})
	got := text(res)
	if !res.IsError || !strings.Contains(got, "your trial covers up to 2 apps") {
		t.Fatalf("got isError=%v %q", res.IsError, got)
	}
	if lastLine(got) != "code: trial_allowance_exceeded" {
		t.Errorf("last line = %q, want the code; text %q", lastLine(got), got)
	}
}

func TestFailedReleaseEndsWithCodeAfterLogTail(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(`{"type":"step","step":"release","state":"start"}` + "\n" + //nolint:errcheck
			`{"type":"output","step":"release","data":"unit crashed: exit 1\n"}` + "\n" +
			`{"type":"result","ok":false,"error":"deploy failed: release did not become healthy","code":"deploy_failed"}` + "\n"))
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_rollback", map[string]any{"app": "api", "version": "v3"})
	got := text(res)
	if !res.IsError || !strings.Contains(got, "release did not become healthy") || !strings.Contains(got, "unit crashed") || !strings.Contains(got, "last ") {
		t.Fatalf("got isError=%v %q", res.IsError, got)
	}
	if lastLine(got) != "code: deploy_failed" {
		t.Errorf("last line = %q, want the code after the log tail; text %q", lastLine(got), got)
	}
}

// A failure that never reached the API has no code to report.
func TestLocalFailureHasNoCodeLine(t *testing.T) {
	cs := session(t, fakeClients{url: "http://127.0.0.1:1", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_list_apps", nil)
	if !res.IsError {
		t.Fatal("want a tool error")
	}
	for line := range strings.SplitSeq(text(res), "\n") {
		if strings.HasPrefix(line, "code:") {
			t.Errorf("local failure has a code line: %q", text(res))
		}
	}
}
