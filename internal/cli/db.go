package cli

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// newDBsCmd is the plural lister, matching `apps`.
func newDBsCmd(app *App) *cobra.Command {
	return newTargetListCmd(app, "dbs", "List databases in the current project, or --all of them", (*portal.Client).Databases)
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
	var plan, description, server string
	create.Flags().StringVar(&plan, "plan", "", "database plan (required)")
	create.Flags().StringVar(&description, "description", "", "human description")
	create.Flags().StringVar(&server, "server", "", "place the database on one of your own database servers (name or id); default: GoShip")
	_ = create.MarkFlagRequired("plan")
	create.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		var serverID string
		if server != "" {
			s, err := resolveDatabaseServer(cmd.Context(), app.Portal(), t, server)
			if err != nil {
				return err
			}
			serverID = s.ID
		}
		if err := app.Portal().CreateDatabase(cmd.Context(), t.Org, t.Project, args[0], plan, description, serverID); err != nil {
			return err
		}
		return app.Renderer().Message("Database %s requested on plan %s.", args[0], plan)
	})

	list := deprecatedList(newDBsCmd(app))

	info := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a database and its usage",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			db, err := app.Portal().Database(cmd.Context(), t.Org, t.Project, args[0])
			if err != nil {
				return err
			}
			r := app.Renderer()
			if err := r.Render(db); err != nil {
				return err
			}
			stats, err := app.Portal().DatabaseStats(cmd.Context(), t.Org, t.Project, args[0])
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
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			list, err := app.Portal().DatabaseUsers(cmd.Context(), t.Org, t.Project, args[0])
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
	userAdd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		created, err := app.Portal().CreateDatabaseUser(cmd.Context(), t.Org, t.Project, args[0], args[1], accessMode)
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
	bind.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := app.Portal().BindDatabase(cmd.Context(), t.Org, t.Project, args[0], bindApp); err != nil {
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
	unbind.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := app.Portal().UnbindDatabase(cmd.Context(), t.Org, t.Project, args[0], unbindApp); err != nil {
			return err
		}
		return app.Renderer().Message("Database %s unbound from %s.", args[0], unbindApp)
	})

	remove := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove", "delete"},
		Short:   "Delete a database and its data",
		Args:    cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			if err := confirm(cmd, app.Global.Yes, "Delete database %q and all its data?", args[0]); err != nil {
				return err
			}
			if err := app.Portal().DeleteDatabase(cmd.Context(), t.Org, t.Project, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Database %s deleted.", args[0])
		}),
	}

	cmd.AddCommand(create, list, info, users, userAdd, bind, unbind, remove, newDBServerCmd(app))
	return cmd
}

func newDBServerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run PostgreSQL servers in your own cloud",
		Long: "Run PostgreSQL servers in your own cloud.\n\n" +
			"A server is a managed PostgreSQL instance GoShip creates in a connected Google Cloud project. Databases on it are not billed by GoShip.",
	}

	list := newTargetListCmd(app, "list", "List the database servers the current project can use, or --all of them", (*portal.Client).DatabaseServers, skipForbiddenProjects)

	create := &cobra.Command{
		Use:   "create --account <account> --region <region> [--tier <tier>] [--name <name>]",
		Short: "Create a PostgreSQL server in a connected cloud account",
		Args:  cobra.NoArgs,
	}
	var account, region, tier, name string
	create.Flags().StringVar(&account, "account", "", "cloud account (id, label or provider) (required)")
	create.Flags().StringVar(&region, "region", "", "cloud region (required); see `goship cloud regions <account>`")
	create.Flags().StringVar(&tier, "tier", "db-f1-micro", "machine size; see `goship db server tiers`")
	create.Flags().StringVar(&name, "name", "", "server name (default pg-<account label>)")
	_ = create.MarkFlagRequired("account")
	_ = create.MarkFlagRequired("region")
	create.RunE = cloudRunE(app, func(cmd *cobra.Command, t Target, _ []string) error {
		acc, scope, err := app.resolveCloudScoped(cmd.Context(), app.projectView(cmd.Context(), t), account)
		if err != nil {
			return err
		}
		if acc.Scope == "org" {
			scope.Project = ""
		}
		if name == "" {
			name = "pg-" + slugify(acc.Label)
		}
		s, err := app.Portal().CreateDatabaseServer(cmd.Context(), scope.Org, scope.Project, portal.CreateDatabaseServerRequest{
			CloudAccountID: acc.ID, Name: name, Region: region, Tier: tier,
		})
		if err != nil {
			return err
		}
		if app.Global.Output == render.JSON {
			return app.Renderer().Render(s)
		}
		return app.Renderer().Message("Server %s is %s. Google takes about ten minutes; watch it with `goship db server list`.", s.Name, s.Status)
	})

	tiers := &cobra.Command{
		Use:   "tiers",
		Short: "List the machine sizes a server can have",
		Args:  cobra.NoArgs,
		RunE: cloudRunE(app, func(cmd *cobra.Command, t Target, _ []string) error {
			list, err := app.Portal().DatabaseServerTiers(cmd.Context(), t.Org, t.Project)
			if err != nil {
				return err
			}
			return app.Renderer().Render(list)
		}),
	}

	retry := &cobra.Command{
		Use:   "retry <server>",
		Short: "Retry a server that failed to come up",
		Args:  cobra.ExactArgs(1),
		RunE: cloudRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			s, scope, err := app.resolveDatabaseServerScoped(cmd.Context(), t, args[0])
			if err != nil {
				return err
			}
			if err := app.Portal().RetryDatabaseServer(cmd.Context(), scope.Org, scope.Project, s.ID); err != nil {
				return err
			}
			return app.Renderer().Message("Retrying server %s.", s.Name)
		}),
	}

	del := &cobra.Command{
		Use:   "delete <server>",
		Short: "Delete a server (its databases must be deleted first)",
		Args:  cobra.ExactArgs(1),
		RunE: cloudRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			s, scope, err := app.resolveDatabaseServerScoped(cmd.Context(), t, args[0])
			if err != nil {
				return err
			}
			if err := confirm(cmd, app.Global.Yes, "Delete database server %q? The Cloud SQL instance is removed from your project.", s.Name); err != nil {
				return err
			}
			if err := app.Portal().DeleteDatabaseServer(cmd.Context(), scope.Org, scope.Project, s.ID); err != nil {
				return err
			}
			return app.Renderer().Message("Deleting server %s; the Cloud SQL instance is being removed from your project.", s.Name)
		}),
	}

	cmd.AddCommand(list, create, tiers, retry, del)
	return cmd
}

// resolveDatabaseServer matches a server by id, then by exact name. Names are
// unique per organization, but a project sees its own servers and the
// organization's, so a name shared by two is refused rather than guessed.
func resolveDatabaseServer(ctx context.Context, client *portal.Client, t Target, ref string) (*portal.DatabaseServer, error) {
	servers, err := client.DatabaseServers(ctx, t.Org, t.Project)
	if err != nil {
		return nil, err
	}
	for i := range servers {
		if servers[i].ID == ref {
			return &servers[i], nil
		}
	}
	var match *portal.DatabaseServer
	for i := range servers {
		if servers[i].Name != ref {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("more than one database server named %q in %s; pass the id", ref, cloudScope(t))
		}
		match = &servers[i]
	}
	if match != nil {
		return match, nil
	}
	return nil, fmt.Errorf("no database server %q in %s; run `goship db server list`", ref, cloudScope(t))
}

// resolveDatabaseServerScoped also returns the target to act through: an
// organization's server is changed on the organization routes, which the API
// requires.
func (a *App) resolveDatabaseServerScoped(ctx context.Context, t Target, ref string) (*portal.DatabaseServer, Target, error) {
	view := a.projectView(ctx, t)
	s, err := resolveDatabaseServer(ctx, a.Portal(), view, ref)
	if err != nil {
		return nil, t, err
	}
	if s.Scope == "org" {
		return s, Target{Org: t.Org}, nil
	}
	return s, view, nil
}

// projectView is the target a person sees in `db server list`: the selected
// project, which includes what the organization shares with it. With no
// project to resolve it stays at the organization.
func (a *App) projectView(ctx context.Context, t Target) Target {
	if t.Project != "" {
		return t
	}
	if project, err := a.projectIn(ctx, t.Org); err == nil {
		return Target{Org: t.Org, Project: project}
	}
	return t
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "prod"
	}
	return s
}
