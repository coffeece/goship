package main

import (
	"fmt"
	"os"

	"github.com/coffeece/goship/internal/cli"
	"github.com/coffeece/goship/internal/portal"
)

var version = "dev"

func main() {
	root := cli.NewRoot(version)
	root.SetArgs(cli.Rewrite(os.Args[1:]))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if portal.IsUnauthorized(err) {
			fmt.Fprintln(os.Stderr, "Your session has expired or is not valid. Run `goship login`.")
		}
		// A pinned org survives a change of account, and then every org-scoped
		// call answers "forbidden" with no hint as to why.
		if portal.IsForbidden(err) {
			fmt.Fprintln(os.Stderr, "You may not be a member of the selected organization. Check with `goship org list`, then `goship org use <slug>`.")
		}
		os.Exit(1)
	}
}
