package deploy

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// captureStderr returns what fn wrote to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = saved }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestProjectFileReadsTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "goship.yaml"), "app: api\nenvironment: staging\nenv:\n  LOG_LEVEL: debug\n")
	p, _, err := LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Environment != "staging" || p.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("got %+v: environment: is the environment, env: stays the variables", p)
	}
}

// node: placed an app on a machine; clusters belong to environments now. The
// file still loads, and says so once however often a command reads it.
func TestProjectFileWarnsOnNodeKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "goship.yaml"), "app: api\nnode: do-server1\n")

	var p *Project
	stderr := captureStderr(t, func() {
		for range 3 {
			var err error
			if p, _, err = LoadProject(dir); err != nil {
				t.Fatal(err)
			}
		}
	})
	want := `goship.yaml: "node" is no longer used; pick an environment instead` + "\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q once", stderr, want)
	}
	if p.App != "api" {
		t.Errorf("the rest of the file was dropped: %+v", p)
	}

	quiet := t.TempDir()
	writeFile(t, filepath.Join(quiet, "goship.yaml"), "app: api\nenvironment: staging\n")
	if got := captureStderr(t, func() { LoadProject(quiet) }); got != "" { //nolint:errcheck
		t.Errorf("a file without node: warned: %q", got)
	}
}
