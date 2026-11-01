package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
)

// newEnvironmentsCmd lists the organization's environments. `goship env` is
// an app's variables, so this noun is always spelled out.
func newEnvironmentsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "environments",
		Short: "List the environments of the current organization and the cluster each runs on",
		Args:  cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			envs, err := app.Portal().Environments(cmd.Context(), org)
			if err != nil {
				return err
			}
			return app.Renderer().Render(envs)
		}),
	}
}

// newEnvironmentCmd is the singular namespace: it holds the verbs, not the
// listing.
func newEnvironmentCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "environment",
		Short: "Choose, create or remove the environments projects deploy to (production, staging, ...)",
		Long: "Every project exists in each environment of its organization. Commands act\n" +
			"in the environment chosen by --environment, the environment: of goship.yaml,\n" +
			"GOSHIP_ENVIRONMENT or `goship environment use`, in that order, else in the\n" +
			"organization's default (production unless changed).",
	}

	use := &cobra.Command{
		Use:   "use <slug>",
		Short: "Set the environment for subsequent commands in the current project",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			project, err := app.projectIn(cmd.Context(), org)
			if err != nil {
				return err
			}
			envs, err := app.Portal().Environments(cmd.Context(), org)
			if err != nil {
				return err
			}
			slug := args[0]
			if !slices.ContainsFunc(envs, func(e portal.Environment) bool { return e.Slug == slug }) {
				return fmt.Errorf("no environment %q in %s; run `goship environments`", slug, org)
			}
			app.Config.UseEnvironment(org, project, slug)
			if err := app.Config.Save(); err != nil {
				return err
			}
			r := app.Renderer()
			if err := r.Message("Now using %s.", Target{Org: org, Project: project, Environment: slug}); err != nil {
				return err
			}
			// Said closer to the command than the saved choice, these win.
			file := deploy.EnvironmentChoice{File: app.environmentChoice().File}
			pinned := file.Chosen(Target{Org: org, Project: project})
			switch env := os.Getenv("GOSHIP_ENVIRONMENT"); {
			case pinned != "" && pinned != slug:
				return r.Message("Note: the goship.yml in this directory pins environment %s and overrides this here.", pinned)
			case env != "" && env != slug:
				return r.Message("Note: GOSHIP_ENVIRONMENT=%s is set and overrides this.", env)
			}
			return nil
		}),
	}

	var name, cluster, dbServer string
	create := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create an environment in the current organization",
		Long: "Creates an environment every project of the organization gets. It runs on\n" +
			"GoShip's shared cluster and PostgreSQL unless --cluster and --db-server name\n" +
			"one of yours (`goship clusters`, `goship db server list`).",
		Args: cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			ctx, client := cmd.Context(), app.Portal()
			req := portal.CreateEnvironmentRequest{Slug: args[0], Name: cmp.Or(name, args[0])}
			if cluster != "" {
				id, err := deploy.ResolveCluster(ctx, client, org, cluster)
				if err != nil {
					return err
				}
				req.ClusterID = id
			}
			if dbServer != "" {
				s, err := deploy.ResolveDatabaseServer(ctx, client, Target{Org: org}, dbServer)
				if err != nil {
					return err
				}
				req.DBServerID = s.ID
			}
			created, err := client.CreateEnvironment(ctx, org, req)
			if err != nil {
				return err
			}
			return app.Renderer().Render(created)
		}),
	}
	create.Flags().StringVar(&name, "name", "", "display name (defaults to the slug)")
	create.Flags().StringVar(&cluster, "cluster", "", "cluster to run on, by name or id (default: GoShip's shared cluster)")
	create.Flags().StringVar(&dbServer, "db-server", "", "database server new databases go on, by name or id (default: GoShip's PostgreSQL)")

	remove := &cobra.Command{
		Use:     "rm <slug>",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete an environment no project holds anything in",
		Args:    cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			if err := confirm(cmd, app.Global.Yes, "Delete environment %q from every project of %s?", args[0], org); err != nil {
				return err
			}
			// A refusal names the projects that still hold something there;
			// it is returned as the API wrote it.
			if err := app.Portal().DeleteEnvironment(cmd.Context(), org, args[0]); err != nil {
				return err
			}
			// A selection of the removed environment would fail every later
			// command; forgetting it is best effort.
			if app.Config.ForgetEnvironment(org, args[0]) {
				if err := app.Config.Save(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "could not forget the saved environment %s: %v\n", args[0], err)
				}
			}
			return app.Renderer().Message("Environment %s deleted.", args[0])
		}),
	}

	cmd.AddCommand(use, create, remove)
	return cmd
}
