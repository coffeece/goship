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
		os.Exit(1)
	}
}
