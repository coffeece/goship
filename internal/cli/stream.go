package cli

import (
	"fmt"

	"github.com/coffeece/goship/internal/render"
	"github.com/coffeece/goship/internal/tsuru"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	tsuruclient "github.com/tsuru/tsuru-client/tsuru/client"
	tsurucmd "github.com/tsuru/tsuru-client/tsuru/cmd"
)

// streamCmd wraps a tsuru-client command. These write to stdout themselves, so
// they are the one place --output has nothing to format; asking for JSON is a
// mistake worth reporting rather than ignoring.
func streamCmd(app *App, use, short string, inner tsurucmd.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Global.Output == render.JSON {
				return fmt.Errorf("%s streams its output; --output json is not supported", cmd.Name())
			}
			if err := tsuru.Setup(app.Config.Tsuru, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			defer tsuru.Flush()
			return tsuru.Run(inner, args, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	if flagged, ok := inner.(tsurucmd.FlaggedCommand); ok {
		cmd.Flags().SortFlags = false
		addFlagSet(cmd, flagged.Flags())
	}
	return cmd
}

// reservedShorthands are taken by the root's persistent flags. tsuru-client
// reuses some of them (`app run` has -o for --once), and cobra panics when it
// merges the two, so the inner command keeps the long flag and loses the
// letter.
var reservedShorthands = map[string]bool{"o": true, "y": true, "h": true}

func addFlagSet(cmd *cobra.Command, flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		if reservedShorthands[f.Shorthand] {
			f.Shorthand = ""
		}
		cmd.Flags().AddFlag(f)
	})
}

func newStreamCmds(app *App) []*cobra.Command {
	releases := streamCmd(app, "releases", "List an app's deploys", &tsuruclient.AppDeployList{})
	rollback := streamCmd(app, "rollback", "Redeploy a previous image", &tsuruclient.AppDeployRollback{})

	return []*cobra.Command{
		streamCmd(app, "logs", "Stream an app's logs", &tsuruclient.AppLog{}),
		streamCmd(app, "run", "Run a command inside the app's containers", &tsuruclient.AppRun{}),
		streamCmd(app, "shell", "Open a shell in a running unit", &tsuruclient.ShellToContainerCmd{}),
		releases,
		rollback,
	}
}
