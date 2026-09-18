package cli

import (
	"bufio"
	"fmt"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

func newAppsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "apps",
		Short: "List your apps",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			apps, err := app.Portal().Apps(cmd.Context(), org)
			if err != nil {
				return err
			}
			return app.Renderer().Render(apps)
		},
	}
}

func newAppCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage a single app",
	}
	cmd.AddCommand(
		appCreateCmd(app),
		appInfoCmd(app),
		appRemoveCmd(app),
		appLifecycleCmd(app, "start", "Start a stopped app"),
		appLifecycleCmd(app, "stop", "Stop an app"),
		appLifecycleCmd(app, "restart", "Restart an app"),
		appScaleCmd(app),
		appPlanCmd(app),
	)
	return cmd
}

func appCreateCmd(app *App) *cobra.Command {
	var req portal.CreateAppRequest
	var node string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			req.Name = args[0]
			if node != "" {
				id, err := resolveNode(cmd.Context(), app.Portal(), org, node)
				if err != nil {
					return err
				}
				req.NodeID = &id
			}
			created, err := app.Portal().CreateApp(cmd.Context(), org, req)
			if err != nil {
				return err
			}
			return app.Renderer().Render(created)
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.Platform, "platform", "", "platform: go, python, nodejs or static (required)")
	f.StringVar(&req.Plan, "plan", "", "plan slug; the org default when omitted")
	f.StringVar(&req.Description, "description", "", "human description")
	f.StringVar(&node, "node", "", "run on one of your own nodes, by name (BYON)")
	_ = cmd.MarkFlagRequired("platform")

	return cmd
}

func appInfoCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show an app and its units",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			found, err := app.Portal().App(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			r := app.Renderer()
			if err := r.Render(found); err != nil {
				return err
			}
			if app.Global.Output == render.JSON || len(found.Units) == 0 {
				return nil
			}
			if err := r.Message(""); err != nil {
				return err
			}
			return r.Render(found.Units)
		},
	}
}

func appRemoveCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete an app and everything it runs",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			if err := confirm(cmd, app.Global.Yes, "Delete app %q? This cannot be undone.", args[0]); err != nil {
				return err
			}
			if err := app.Portal().DeleteApp(cmd.Context(), org, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("App %s deleted.", args[0])
		},
	}
}

func appLifecycleCmd(app *App, action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " <name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			if err := app.Portal().Lifecycle(cmd.Context(), org, args[0], action); err != nil {
				return err
			}
			return app.Renderer().Message("App %s: %s requested.", args[0], action)
		},
	}
}

func appScaleCmd(app *App) *cobra.Command {
	var units int

	cmd := &cobra.Command{
		Use:   "scale <name> --units <n>",
		Short: "Set how many units the app runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			if err := app.Portal().ScaleApp(cmd.Context(), org, args[0], units); err != nil {
				return err
			}
			return app.Renderer().Message("App %s scaled to %d unit(s).", args[0], units)
		},
	}
	cmd.Flags().IntVar(&units, "units", 0, "number of units (required)")
	_ = cmd.MarkFlagRequired("units")

	return cmd
}

func appPlanCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "plan <name> <plan>",
		Short: "Move the app to another plan",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			if err := app.Portal().SetAppPlan(cmd.Context(), org, args[0], args[1]); err != nil {
				return err
			}
			return app.Renderer().Message("App %s moved to plan %s.", args[0], args[1])
		},
	}
}

func confirm(cmd *cobra.Command, yes bool, format string, args ...any) error {
	if yes {
		return nil
	}
	answer, err := prompt(bufio.NewReader(cmd.InOrStdin()), cmd.ErrOrStderr(), fmt.Sprintf(format, args...)+" [y/N] ")
	if err != nil {
		return err
	}
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("aborted")
	}
	return nil
}
