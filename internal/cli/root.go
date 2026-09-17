package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/coffeece/goship/internal/config"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
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
	case render.Table, render.JSON:
		return nil
	default:
		return fmt.Errorf("unknown --output %q: use %q or %q", g.Output, render.Table, render.JSON)
	}
}

// App is the state shared by every command in the tree.
type App struct {
	Version string
	Global  Global
	Config  *config.Config
	Out     io.Writer
}

func (a *App) Renderer() *render.Renderer {
	return render.New(a.Out, a.Global.Output)
}

// Org resolves the organization a command runs against: --org, then whatever
// `org use` persisted. With neither, an account that belongs to exactly one
// organization has nothing to choose, so pick it rather than demand a step.
func (a *App) Org(ctx context.Context) (string, error) {
	if a.Global.Org != "" {
		return a.Global.Org, nil
	}
	if a.Config.Org != "" {
		return a.Config.Org, nil
	}
	if orgs, err := a.Portal().Orgs(ctx); err == nil && len(orgs) == 1 {
		return orgs[0].Slug, nil
	}
	return a.Config.OrgOrError("")
}

func (a *App) Portal() *portal.Client {
	var opts []portal.Option
	if a.Global.Verbose {
		opts = append(opts, portal.WithTrace(os.Stderr))
	}
	return portal.New(a.Config.API, resolveToken(a.Config.Token), opts...)
}

func NewRoot(version string) *cobra.Command {
	app := &App{Version: version}

	root := &cobra.Command{
		Use:           "goship",
		Short:         "Deploy and operate apps on GoShip",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.Global.validate(); err != nil {
				return err
			}
			app.Out = cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			app.Config = cfg
			return nil
		},
	}

	f := root.PersistentFlags()
	f.StringVar(&app.Global.Org, "org", "", "organization slug (overrides the current org)")
	f.StringVarP(&app.Global.Output, "output", "o", render.Table, "output format: table or json")
	f.BoolVarP(&app.Global.Yes, "yes", "y", false, "answer yes to confirmations")
	f.BoolVar(&app.Global.Verbose, "verbose", false, "log HTTP requests to stderr")

	root.AddCommand(
		newVersionCmd(app),
		newDeployCmd(app),
		newLoginCmd(app),
		newLogoutCmd(app),
		newWhoamiCmd(app),
		newOrgCmd(app),
		newAppsCmd(app),
		newAppCmd(app),
		newEnvCmd(app),
		newDBCmd(app),
		newDomainCmd(app),
		newVolumeCmd(app),
		newNodeCmd(app),
		newPlansCmd(app),
	)
	root.AddCommand(newStreamCmds(app)...)

	return root
}
