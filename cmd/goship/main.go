package main

import (
	"fmt"
	"os"

	"github.com/coffeece/goship/internal/cli"
)

var version = "dev"

func main() {
	root := cli.NewRoot(version)
	root.SetArgs(cli.Rewrite(os.Args[1:]))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
