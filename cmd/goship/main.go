package main

import (
	"fmt"
	"os"

	"github.com/coffeece/goship/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.NewRoot(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
