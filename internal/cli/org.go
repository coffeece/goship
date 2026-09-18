package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newOrgsCmd is the plural lister, matching `apps`: plural nouns list.
func newOrgsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "orgs",
		Short: "List organizations you belong to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			orgs, err := app.Portal().Orgs(cmd.Context())
			if err != nil {
				return err
			}
			return app.Renderer().Render(orgs)
		},
	}
}

// newOrgCmd is the singular namespace: it holds the verbs, not the listing.
func newOrgCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Choose the organization commands run against",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "use <slug>",
		Short: "Set the organization for subsequent commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			orgs, err := app.Portal().Orgs(cmd.Context())
			if err != nil {
				return err
			}
			for _, o := range orgs {
				if o.Slug == slug {
					app.Config.Org = slug
					if err := app.Config.Save(); err != nil {
						return err
					}
					return app.Renderer().Message("Now using %s.", slug)
				}
			}
			return fmt.Errorf("you are not a member of %q; run `goship orgs`", slug)
		},
	})
	return cmd
}
