package cli

import (
	"cmp"
	"fmt"

	"github.com/spf13/cobra"
)

func newProjectsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "projects",
		Short: "List the projects of the current organization",
		Args:  cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			projects, err := app.Portal().Projects(cmd.Context(), org)
			if err != nil {
				return err
			}
			return app.Renderer().Render(projects)
		}),
	}
}

// newProjectCmd is the singular namespace: it holds the verbs, not the listing.
func newProjectCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Choose or create the project commands run against",
	}

	use := &cobra.Command{
		Use:   "use <slug>",
		Short: "Set the project for subsequent commands in the current organization",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			projects, err := app.Portal().Projects(cmd.Context(), org)
			if err != nil {
				return err
			}
			for _, p := range projects {
				if p.Slug != args[0] {
					continue
				}
				if app.Config.Projects == nil {
					app.Config.Projects = map[string]string{}
				}
				app.Config.Projects[org] = p.Slug
				if err := app.Config.Save(); err != nil {
					return err
				}
				return app.Renderer().Message("Now using %s/%s.", org, p.Slug)
			}
			return fmt.Errorf("no project %q in %s that you can reach; run `goship projects`", args[0], org)
		}),
	}

	var name string
	create := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a project in the current organization",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			p, err := app.Portal().CreateProject(cmd.Context(), org, cmp.Or(name, args[0]), args[0])
			if err != nil {
				return err
			}
			return app.Renderer().Render(p)
		}),
	}
	create.Flags().StringVar(&name, "name", "", "display name (defaults to the slug)")

	cmd.AddCommand(use, create)
	return cmd
}
