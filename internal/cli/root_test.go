package cli

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Keep tests off the developer's real ~/.config/goship/config.json.
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("GOSHIP_CONFIG", filepath.Join(t.TempDir(), "config.json"))
}

// v0.1.0 shipped `app link list` as a childless command with nothing to run: it
// printed its parent topic's blurb and exited 0. The tree was derived from
// command-name strings by tsuru-client, so nothing in our source showed it.
func TestEveryLeafCommandRuns(t *testing.T) {
	var walk func(*cobra.Command, []string)
	walk = func(c *cobra.Command, path []string) {
		path = append(path, c.Name())
		children := c.Commands()
		if len(children) == 0 {
			if c.Run == nil && c.RunE == nil {
				t.Errorf("%q has no subcommands and nothing to run", strings.Join(path, " "))
			}
			return
		}
		for _, child := range children {
			walk(child, path)
		}
	}
	walk(NewRoot("test"), nil)
}

func TestOutputFlagRejectsUnknownFormats(t *testing.T) {
	isolateConfig(t)
	root := NewRoot("test")
	root.SetArgs([]string{"version", "--output", "xml"})
	root.SetOut(&strings.Builder{})
	if err := root.Execute(); err == nil {
		t.Fatal("expected --output xml to be rejected")
	}
}

func TestVersionPrintsTheBuildVersion(t *testing.T) {
	isolateConfig(t)
	var out strings.Builder
	root := NewRoot("v1.2.3")
	root.SetArgs([]string{"version"})
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "goship v1.2.3 (") || !strings.Contains(got, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("got %q, want the version and the platform it was built for", got)
	}
}
