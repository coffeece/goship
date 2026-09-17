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

	// Takes the running subcommand, not the parent: cobra sets the context on
	// the command it executes, and the parent's would be a different one.
	resolve := func(cmd *cobra.Command) (string, string, error) {
		org, err := app.Org(cmd.Context())
		if err != nil {
			return "", "", err
		}
		if appName == "" {
			return "", "", fmt.Errorf("--app is required")
		}
		return org, appName, nil
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List environment variables",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				org, name, err := resolve(cmd)
				if err != nil {
					return err
				}
				envs, err := app.Portal().Env(cmd.Context(), org, name)
				if err != nil {
					return err
				}
				return app.Renderer().Render(envs)
			},
		},
		&cobra.Command{
			Use:   "set KEY=VALUE [KEY=VALUE ...]",
			Short: "Set environment variables",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				org, name, err := resolve(cmd)
				if err != nil {
					return err
				}
				envs := make([]portal.EnvVar, 0, len(args))
				for _, arg := range args {
					key, value, ok := strings.Cut(arg, "=")
					if !ok || key == "" {
						return fmt.Errorf("expected KEY=VALUE, got %q", arg)
					}
					envs = append(envs, portal.EnvVar{Name: key, Value: value})
				}
				if err := app.Portal().SetEnv(cmd.Context(), org, name, envs, noRestart); err != nil {
					return err
				}
				return app.Renderer().Message("Set %d variable(s) on %s.", len(envs), name)
			},
		},
		&cobra.Command{
			Use:   "unset KEY [KEY ...]",
			Short: "Remove environment variables",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				org, name, err := resolve(cmd)
				if err != nil {
					return err
				}
				if err := app.Portal().UnsetEnv(cmd.Context(), org, name, args, noRestart); err != nil {
					return err
				}
				return app.Renderer().Message("Removed %d variable(s) from %s.", len(args), name)
			},
		},
	)
	return cmd
}
