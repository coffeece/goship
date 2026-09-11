package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	OutputTable = "table"
	OutputJSON  = "json"
)

// Global carries the flags every command may read.
type Global struct {
	Org     string
	Output  string
	Yes     bool
	Verbose bool
}

func (g *Global) validate() error {
	switch g.Output {
	case OutputTable, OutputJSON:
		return nil
	default:
		return fmt.Errorf("unknown --output %q: use %q or %q", g.Output, OutputTable, OutputJSON)
	}
}

// App is the state shared by every command in the tree.
type App struct {
	Version string
	Global  Global
}

func NewRoot(version string) *cobra.Command {
	app := &App{Version: version}

	root := &cobra.Command{
		Use:           "goship",
		Short:         "Deploy and operate apps on GoShip",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return app.Global.validate()
		},
	}

	f := root.PersistentFlags()
	f.StringVar(&app.Global.Org, "org", "", "organization slug (overrides the current org)")
	f.StringVarP(&app.Global.Output, "output", "o", OutputTable, "output format: table or json")
	f.BoolVarP(&app.Global.Yes, "yes", "y", false, "answer yes to confirmations")
	f.BoolVar(&app.Global.Verbose, "verbose", false, "log HTTP requests to stderr")

	root.AddCommand(newVersionCmd(app))

	return root
}
