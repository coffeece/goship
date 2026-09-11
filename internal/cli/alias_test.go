package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRewriteMapsLegacyNames(t *testing.T) {
	for _, tc := range []struct{ in, want []string }{
		{[]string{"app-list"}, []string{"apps"}},
		{[]string{"env-set", "-a", "api", "K=V"}, []string{"env", "set", "-a", "api", "K=V"}},
		{[]string{"apps"}, []string{"apps"}},
		{nil, nil},
	} {
		if got := Rewrite(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("Rewrite(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Every alias must land on a command that exists; a typo here would make a
// v0.1.0 name fail with "unknown command" instead of working.
func TestEveryAliasResolves(t *testing.T) {
	root := NewRoot("test")

	for old, path := range legacyNames {
		found, _, err := root.Find(path)
		if err != nil {
			t.Errorf("%s → %v: %v", old, path, err)
			continue
		}
		if !sameCommand(found, path) {
			t.Errorf("%s → %v resolved to %q instead", old, path, found.CommandPath())
		}
	}
}

func sameCommand(c *cobra.Command, path []string) bool {
	return c.CommandPath() == "goship "+strings.Join(path, " ")
}

// Commands v0.1.0 shipped that this CLI deliberately drops. Listing them keeps
// the removal a decision rather than an oversight.
func TestRemovedCommandsAreNotAliased(t *testing.T) {
	for _, removed := range []string{
		"plugin-install", "plugin-remove", "plugin-list", "plugin-bundle",
		"certificate-set", "certificate-unset", "certificate-list",
		"unit-add", "unit-remove", "unit-kill", "unit-set",
		"service-list", "service-instance-add", "service-plan-list",
		"job-create", "job-list", "job-trigger",
		"init", "change-password",
	} {
		if _, ok := legacyNames[removed]; ok {
			t.Errorf("%s is listed as removed but still aliased", removed)
		}
	}
}
