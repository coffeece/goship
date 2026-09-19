package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// noJSON refuses --output json on commands whose output is a live stream of
// text: there is nothing to format, and ignoring the flag would hide a mistake.
func noJSON(app *App, cmd *cobra.Command) error {
	if app.Global.Output == render.JSON {
		return fmt.Errorf("%s streams its output; --output json is not supported", cmd.Name())
	}
	return nil
}

// interruptible returns a context that ends on Ctrl-C, so a followed log or a
// long command stops cleanly instead of being killed mid-line.
func interruptible(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

func newLogsCmd(app *App) *cobra.Command {
	var (
		appName string
		opts    portal.LogOptions
		noDate  bool
	)
	cmd := &cobra.Command{
		Use:   "logs --app <app>",
		Short: "Show an app's logs",
		Args:  cobra.NoArgs,
	}
	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (required)")
	f.IntVarP(&opts.Lines, "lines", "l", 100, "how many past lines to show")
	f.BoolVarP(&opts.Follow, "follow", "f", false, "keep printing new lines until interrupted")
	f.StringVarP(&opts.Source, "source", "s", "", "only lines from this source, e.g. web")
	f.StringSliceVarP(&opts.Units, "unit", "u", nil, "only lines from this unit (repeatable)")
	f.BoolVar(&noDate, "no-date", false, "leave the timestamp out")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		ctx, stop := interruptible(cmd.Context())
		defer stop()

		out := cmd.OutOrStdout()
		err := app.Portal().Logs(ctx, org, appName, opts, func(l portal.LogEntry) error {
			prefix := fmt.Sprintf("[%s][%s]", l.Source, l.Unit)
			if !noDate {
				prefix = l.Date.Local().Format("2006-01-02 15:04:05") + " " + prefix
			}
			_, err := fmt.Fprintf(out, "%s: %s\n", prefix, strings.TrimRight(l.Message, "\n"))
			return err
		})
		// Ctrl-C is how a follow is meant to end.
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return explainStreamError(err, "log stream", false)
	})
	return cmd
}

func newRunCmd(app *App) *cobra.Command {
	var (
		appName        string
		once, isolated bool
	)
	cmd := &cobra.Command{
		Use:   "run --app <app> -- <command> [args...]",
		Short: "Run a one-off command in the app's image",
		Long: "Runs a command where the app's code and environment are. By default it\n" +
			"runs in every unit; --once picks one, --isolated starts a fresh container.",
		Args: cobra.MinimumNArgs(1),
	}
	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (required)")
	f.BoolVar(&once, "once", false, "run in a single unit instead of all of them")
	f.BoolVar(&isolated, "isolated", false, "run in a new container rather than a serving unit")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		ctx, stop := interruptible(cmd.Context())
		defer stop()
		err := app.Portal().Run(ctx, org, appName, strings.Join(args, " "), once, isolated, cmd.OutOrStdout())
		return explainStreamError(err, "command", false)
	})
	return cmd
}

func newReleasesCmd(app *App) *cobra.Command {
	var (
		appName string
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "releases --app <app>",
		Short: "List an app's releases",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVarP(&appName, "app", "a", "", "app name (required)")
	cmd.Flags().IntVar(&limit, "limit", 20, "how many releases to show")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
		deploys, err := app.Portal().Deploys(cmd.Context(), org, appName, limit)
		if err != nil {
			return err
		}
		return app.Renderer().Render(deploys)
	})
	return cmd
}

func newRollbackCmd(app *App) *cobra.Command {
	var appName string
	cmd := &cobra.Command{
		Use:   "rollback <version> --app <app>",
		Short: "Release a previous version again",
		Long:  "Releases a version from `goship releases` again, e.g. `goship rollback v12 -a api`.",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringVarP(&appName, "app", "a", "", "app name (required)")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		version := args[0]
		// `goship releases` prints 12; the API speaks v12.
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		err := app.Portal().Rollback(cmd.Context(), org, appName, version, cmd.OutOrStdout())
		return explainStreamError(err, "rollback of "+appName, true)
	})
	return cmd
}

// newShellCmd keeps the command visible while it has no backend: an
// interactive shell needs a websocket the GoShip API does not proxy yet.
func newShellCmd(_ *App) *cobra.Command {
	return &cobra.Command{
		Use:   "shell --app <app>",
		Short: "Open a shell in a running unit (not available yet)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error {
			return errors.New("an interactive shell is not available yet. `goship run -a <app> -- <command>` runs a command where the app's code and environment are")
		},
		DisableFlagParsing: true,
	}
}

func newStreamCmds(app *App) []*cobra.Command {
	return []*cobra.Command{
		newLogsCmd(app),
		newRunCmd(app),
		newShellCmd(app),
		newReleasesCmd(app),
		newRollbackCmd(app),
	}
}
