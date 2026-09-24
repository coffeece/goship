package cli

import (
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
)

func newEnvCmd(app *App) *cobra.Command {
	var appName string
	var noRestart bool

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage an app's environment variables",
	}
	cmd.PersistentFlags().StringVarP(&appName, "app", "a", "", "app name (required)")
	cmd.PersistentFlags().BoolVar(&noRestart, "no-restart", false, "apply without restarting the app")
	_ = cmd.MarkPersistentFlagRequired("app")

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List environment variables",
			Args:  cobra.NoArgs,
			RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
				envs, err := app.Portal().Env(cmd.Context(), org, appName)
				if err != nil {
					return err
				}
				return app.Renderer().Render(envs)
			}),
		},
		&cobra.Command{
			Use:   "set KEY=VALUE [KEY=VALUE ...]",
			Short: "Set environment variables",
			Args:  cobra.MinimumNArgs(1),
			RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
				envs := make([]portal.EnvVar, 0, len(args))
				for _, arg := range args {
					key, value, ok := strings.Cut(arg, "=")
					if !ok || key == "" {
						return fmt.Errorf("expected KEY=VALUE, got %q", arg)
					}
					envs = append(envs, portal.EnvVar{Name: key, Value: value})
				}
				if err := app.Portal().SetEnv(cmd.Context(), org, appName, envs, noRestart); err != nil {
					return err
				}
				return app.Renderer().Message("Set %d variable(s) on %s.", len(envs), appName)
			}),
		},
		&cobra.Command{
			Use:   "unset KEY [KEY ...]",
			Short: "Remove environment variables",
			Args:  cobra.MinimumNArgs(1),
			RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
				if err := app.Portal().UnsetEnv(cmd.Context(), org, appName, args, noRestart); err != nil {
					return err
				}
				return app.Renderer().Message("Removed %d variable(s) from %s.", len(args), appName)
			}),
		},
	)
	return cmd
}
