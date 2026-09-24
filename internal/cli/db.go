package cli

import (
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// newDBsCmd is the plural lister, matching `apps`.
func newDBsCmd(app *App) *cobra.Command {
	return newOrgListCmd(app, "dbs", "List databases in the current organization, or --all of them", (*portal.Client).Databases)
}

func newDBCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "db",
		Aliases: []string{"database"},
		Short:   "Manage managed PostgreSQL databases",
	}

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Provision a database",
		Args:  cobra.ExactArgs(1),
	}
	var plan, description string
	create.Flags().StringVar(&plan, "plan", "", "database plan (required)")
	create.Flags().StringVar(&description, "description", "", "human description")
	_ = create.MarkFlagRequired("plan")
	create.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := app.Portal().CreateDatabase(cmd.Context(), org, args[0], plan, description); err != nil {
			return err
		}
		return app.Renderer().Message("Database %s requested on plan %s.", args[0], plan)
	})

	list := deprecatedList(newDBsCmd(app))

	info := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a database and its usage",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			db, err := app.Portal().Database(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			r := app.Renderer()
			if err := r.Render(db); err != nil {
				return err
			}
			stats, err := app.Portal().DatabaseStats(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			return r.Render(stats)
		}),
	}

	users := &cobra.Command{
		Use:   "users <name>",
		Short: "List database users",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			list, err := app.Portal().DatabaseUsers(cmd.Context(), org, args[0])
			if err != nil {
				return err
			}
			return app.Renderer().Render(list)
		}),
	}

	userAdd := &cobra.Command{
		Use:   "user-add <name> <username>",
		Short: "Create a database user",
		Args:  cobra.ExactArgs(2),
	}
	var accessMode string
	userAdd.Flags().StringVar(&accessMode, "access", "rw", "access mode: rw or ro")
	userAdd.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		created, err := app.Portal().CreateDatabaseUser(cmd.Context(), org, args[0], args[1], accessMode)
		if err != nil {
			return err
		}
		if app.Global.Output == render.JSON {
			return app.Renderer().Render(created)
		}
		// The password is returned once and never again.
		return app.Renderer().Render(struct {
			Username string `table:"USERNAME"`
			Access   string `table:"ACCESS"`
			Password string `table:"PASSWORD"`
		}{created.User.Username, created.User.AccessMode, created.Password})
	})

	bind := &cobra.Command{
		Use:   "bind <name> --app <app>",
		Short: "Bind a database to an app, injecting its credentials",
		Args:  cobra.ExactArgs(1),
	}
	var bindApp string
	bind.Flags().StringVarP(&bindApp, "app", "a", "", "app to bind (required)")
	_ = bind.MarkFlagRequired("app")
	bind.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := app.Portal().BindDatabase(cmd.Context(), org, args[0], bindApp); err != nil {
			return err
		}
		return app.Renderer().Message("Database %s bound to %s.", args[0], bindApp)
	})

	unbind := &cobra.Command{
		Use:   "unbind <name> --app <app>",
		Short: "Unbind a database from an app",
		Args:  cobra.ExactArgs(1),
	}
	var unbindApp string
	unbind.Flags().StringVarP(&unbindApp, "app", "a", "", "app to unbind (required)")
	_ = unbind.MarkFlagRequired("app")
	unbind.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		if err := app.Portal().UnbindDatabase(cmd.Context(), org, args[0], unbindApp); err != nil {
			return err
		}
		return app.Renderer().Message("Database %s unbound from %s.", args[0], unbindApp)
	})

	remove := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete a database and its data",
		Args:    cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			if err := confirm(cmd, app.Global.Yes, "Delete database %q and all its data?", args[0]); err != nil {
				return err
			}
			if err := app.Portal().DeleteDatabase(cmd.Context(), org, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Database %s deleted.", args[0])
		}),
	}

	cmd.AddCommand(create, list, info, users, userAdd, bind, unbind, remove)
	return cmd
}
