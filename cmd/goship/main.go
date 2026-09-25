package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/coffeece/goship/internal/cli"
	"github.com/coffeece/goship/internal/portal"
)

// version is set by the release build.
var version = "dev"

func main() {
	root := cli.NewRoot(buildVersion())
	root.SetArgs(cli.Rewrite(os.Args[1:]))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if portal.IsUnauthorized(err) {
			fmt.Fprintln(os.Stderr, "Your session has expired or is not valid. Run `goship login`.")
		}
		// A pinned org survives a change of account, and then every org-scoped
		// call answers "forbidden" with no hint as to why.
		if portal.IsForbidden(err) {
			fmt.Fprintln(os.Stderr, "You may not be a member of the selected organization. Check with `goship orgs`, then `goship org use <slug>`.")
		}
		os.Exit(1)
	}
}

// buildVersion is what the release build stamped or, for a binary from
// `go install …@v0.1.0`, the module version the go command recorded. The API
// reads it to tell an outdated binary to upgrade, so it must not be "dev" when
// there is a real version to report.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return strings.TrimPrefix(bi.Main.Version, "v")
	}
	return version
}
