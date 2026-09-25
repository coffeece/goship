package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

func newVersionCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "goship %s (%s)\n", app.Version, buildDetails())
			return err
		},
	}
}

// buildDetails says which commit a binary was built from, when the go command
// recorded it, and for which platform — what a bug report needs.
func buildDetails() string {
	details := fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return details
	}
	var revision, built string
	var modified bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value[:min(12, len(s.Value))]
		case "vcs.time":
			built = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return details
	}
	if modified {
		revision += "-dirty"
	}
	if built != "" {
		revision += ", " + built
	}
	return revision + ", " + details
}
