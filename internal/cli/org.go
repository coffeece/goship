package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newOrgCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage the organization commands run against",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List organizations you belong to",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				orgs, err := app.Portal().Orgs(cmd.Context())
				if err != nil {
					return err
				}
				return app.Renderer().Render(orgs)
			},
		},
		&cobra.Command{
			Use:   "use <slug>",
			Short: "Set the organization for subsequent commands",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				slug := args[0]

				orgs, err := app.Portal().Orgs(cmd.Context())
				if err != nil {
					return err
				}
				found := false
				for _, o := range orgs {
					if o.Slug == slug {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("you are not a member of %q; run `goship org list`", slug)
				}

				app.Config.Org = slug
				if err := app.Config.Save(); err != nil {
					return err
				}
				return app.Renderer().Message("Now using %s.", slug)
			},
		},
	)
	return cmd
}
