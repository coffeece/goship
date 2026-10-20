package cli

import (
	"errors"

	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

func appVisibilityCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "visibility <name> public|private",
		Short: "Make an app public, or put a GoShip login page in front of it",
		Args:  cobra.ExactArgs(2),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			name, value := args[0], args[1]
			if value != "public" && value != "private" {
				return errors.New("visibility must be public or private")
			}
			if err := app.Portal().SetAppVisibility(cmd.Context(), t.Org, t.Project, name, value); err != nil {
				return err
			}
			msg := "App %s is now %s."
			if a, err := app.Portal().App(cmd.Context(), t.Org, t.Project, name); err == nil && a.Deploys == 0 && value == "private" {
				msg += " It takes effect on the first deploy."
			}
			return app.Renderer().Message(msg, name, value)
		}),
	}
}

func appAccessCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Manage who can open a private app",
	}

	list := &cobra.Command{
		Use:   "list <name>",
		Short: "List the users who can open the app",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			users, err := app.Portal().ListAppAccess(cmd.Context(), t.Org, t.Project, args[0])
			if err != nil {
				return err
			}
			return app.Renderer().Render(users)
		}),
	}

	var addPassword string
	add := &cobra.Command{
		Use:   "add <name> <username>",
		Short: "Create an access user; a password is generated unless --password is given",
		Args:  cobra.ExactArgs(2),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			created, err := app.Portal().AddAppAccess(cmd.Context(), t.Org, t.Project, args[0], args[1], addPassword)
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(created)
			}
			if created.Password == "" {
				return app.Renderer().Message("Access user %s added.", args[1])
			}
			// The generated password is returned once and never again.
			return app.Renderer().Render(struct {
				Username string `table:"USERNAME"`
				Password string `table:"PASSWORD"`
			}{created.Username, created.Password})
		}),
	}
	add.Flags().StringVar(&addPassword, "password", "", "set this password instead of generating one")

	var resetPassword string
	reset := &cobra.Command{
		Use:   "reset <name> <username>",
		Short: "Set a new password for an access user; one is generated unless --password is given",
		Args:  cobra.ExactArgs(2),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			res, err := app.Portal().ResetAppAccess(cmd.Context(), t.Org, t.Project, args[0], args[1], resetPassword)
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(res)
			}
			if res.Password == "" {
				return app.Renderer().Message("Password for %s reset.", args[1])
			}
			return app.Renderer().Render(struct {
				Username string `table:"USERNAME"`
				Password string `table:"PASSWORD"`
			}{res.Username, res.Password})
		}),
	}
	reset.Flags().StringVar(&resetPassword, "password", "", "set this password instead of generating one")

	remove := &cobra.Command{
		Use:     "rm <name> <username>",
		Aliases: []string{"remove"},
		Short:   "Remove an access user",
		Args:    cobra.ExactArgs(2),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			if err := confirm(cmd, app.Global.Yes, "Remove %q from %s?", args[1], args[0]); err != nil {
				return err
			}
			if err := app.Portal().RemoveAppAccess(cmd.Context(), t.Org, t.Project, args[0], args[1]); err != nil {
				return err
			}
			return app.Renderer().Message("Access user %s removed.", args[1])
		}),
	}

	var reveal, rotate bool
	token := &cobra.Command{
		Use:   "token <name>",
		Short: "Show the access token monitors use to get past the login page",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			var tok string
			var err error
			if rotate {
				if err = confirm(cmd, app.Global.Yes, "Rotate the access token for %s? Monitors using the old one stop getting in.", args[0]); err != nil {
					return err
				}
				tok, err = app.Portal().RotateAppAccessToken(cmd.Context(), t.Org, t.Project, args[0])
			} else {
				tok, err = app.Portal().AppAccessToken(cmd.Context(), t.Org, t.Project, args[0], reveal)
			}
			if err != nil {
				return err
			}
			return app.Renderer().Render(struct {
				Token string `json:"token" table:"TOKEN"`
			}{tok})
		}),
	}
	token.Flags().BoolVar(&reveal, "reveal", false, "show the whole token instead of a masked one")
	token.Flags().BoolVar(&rotate, "rotate", false, "issue a new token; the old one stops working")

	cmd.AddCommand(list, add, reset, remove, token)
	return cmd
}
