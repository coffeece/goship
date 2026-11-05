package cli

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/coffeece/goship/internal/deploy"
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
	var plan, description, server, sizeLimit string
	create.Flags().StringVar(&plan, "plan", "", "database plan (required when the database goes on GoShip's PostgreSQL)")
	create.Flags().StringVar(&description, "description", "", "human description")
	create.Flags().StringVar(&server, "server", "", "place the database on one of your own database servers (name or id), or \"goship\" for GoShip's PostgreSQL; default: the environment's database server")
	create.Flags().StringVar(&sizeLimit, "size-limit", "", "size limit for a database on your own server, like 500MB or 2GB (default: none)")
	create.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		// Without --server the database goes on the environment's database
		// server, which may be one of yours: then it has no plan either. With
		// neither --server nor --plan that is looked up; with a plan the API
		// places it, and ignores the plan on a server of yours.
		var srv *portal.DatabaseServer
		var fromEnv bool
		if server != "" || plan == "" {
			var err error
			if srv, fromEnv, err = deploy.DatabasePlacement(cmd.Context(), app.Portal(), t, server); err != nil {
				return err
			}
		}
		onGoShip := srv == nil
		if onGoShip && plan == "" {
			if server == "" {
				return fmt.Errorf("--plan is required: new databases in %s go on GoShip's PostgreSQL (or pass --server to use one of yours)", t.Environment)
			}
			return errors.New("--plan is required on GoShip's PostgreSQL")
		}
		if onGoShip && sizeLimit != "" {
			return errors.New("--size-limit is only for databases on your own server")
		}
		var limit *int
		if sizeLimit != "" {
			var err error
			if limit, err = parseSizeMB(sizeLimit); err != nil {
				return err
			}
		}
		var serverID, serverName string
		switch {
		case server == deploy.GoShipDatabaseServer:
			serverID = server
		case srv != nil:
			serverName = srv.Name
			if !fromEnv {
				serverID = srv.ID
			}
			if plan != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "note: databases on your own server have no plan; --plan ignored")
				plan = ""
			}
		}
		ch, err := app.Portal().CreateDatabase(cmd.Context(), t.Org, t.Project, t.Environment, args[0], plan, description, serverID, limit)
		if err != nil {
			return err
		}
		if !onGoShip {
			return reportChange(app, ch, "Database %s requested on server %s.", args[0], serverName)
		}
		return reportChange(app, ch, "Database %s requested on plan %s.", args[0], plan)
	})

	sizeLimitCmd := &cobra.Command{
		Use:   "size-limit <name> <size|none>",
		Short: "Set or clear the size limit of a database on your own server (500MB, 2GB, none)",
		Args:  cobra.ExactArgs(2),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			limit, err := parseSizeMB(args[1])
			if err != nil {
				return err
			}
			msg, err := app.Portal().SetDatabaseSizeLimit(cmd.Context(), t.Org, t.Project, t.Environment, args[0], limit)
			if err != nil {
				return err
			}
			if msg == "" {
				msg = "Size limit saved; it applies within 5 minutes."
			}
			return app.Renderer().Message("%s", msg)
		}),
	}

	list := deprecatedList(newDBsCmd(app))

	info := &cobra.Command{
		Use:   "info <name>",
		Short: "Show a database and its usage",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			db, err := app.Portal().Database(cmd.Context(), t.Org, t.Project, t.Environment, args[0])
			if err != nil {
				return err
			}
			r := app.Renderer()
			if err := r.Render(db); err != nil {
				return err
			}
			stats, err := app.Portal().DatabaseStats(cmd.Context(), t.Org, t.Project, t.Environment, args[0])
			if err != nil {
				return err
			}
			if err := r.Render(stats); err != nil {
				return err
			}
			if db.ServerID == "" || app.Global.Output == render.JSON {
				return nil
			}
			limit := "none"
			if stats.MaxSizeMB > 0 {
				limit = fmt.Sprintf("%d MB", stats.MaxSizeMB)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "size limit: %s\nserver connections: %d of %d\n", limit, stats.ServerConnections, stats.MaxConns)
			return nil
		}),
	}

	users := &cobra.Command{
		Use:   "users <name>",
		Short: "List database users",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			list, err := app.Portal().DatabaseUsers(cmd.Context(), t.Org, t.Project, t.Environment, args[0])
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
		created, err := app.Portal().CreateDatabaseUser(cmd.Context(), t.Org, t.Project, t.Environment, args[0], args[1], accessMode)
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

	extensions := &cobra.Command{
		Use:   "extensions <name>",
		Short: "List database extensions: enabled, enabling and available",
		Args:  cobra.ExactArgs(1),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			exts, err := app.Portal().DatabaseExtensions(cmd.Context(), t.Org, t.Project, t.Environment, args[0])
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(exts)
			}
			return app.Renderer().Render(exts.Rows())
		}),
	}

	extensionAdd := &cobra.Command{
		Use:   "extension-add <name> <extension>",
		Short: "Enable a PostgreSQL extension (postgis, pg_trgm, ...)",
		Args:  cobra.ExactArgs(2),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			ext, err := app.Portal().EnableDatabaseExtension(cmd.Context(), t.Org, t.Project, t.Environment, args[0], args[1])
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(ext)
			}
			if ext.Status == "enabling" {
				return waitForExtension(cmd, app, t, args[0], args[1])
			}
			return app.Renderer().Message("Extension %s %s enabled on %s.", ext.Name, ext.Version, args[0])
		}),
	}

	extensionRm := &cobra.Command{
		Use:   "extension-rm <name> <extension>",
		Short: "Disable a PostgreSQL extension (fails while tables still use it)",
		Args:  cobra.ExactArgs(2),
		RunE: targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			if err := app.Portal().DisableDatabaseExtension(cmd.Context(), t.Org, t.Project, t.Environment, args[0], args[1]); err != nil {
				return err
			}
			return app.Renderer().Message("Extension %s disabled on %s.", args[1], args[0])
		}),
	}

	bind := &cobra.Command{
		Use:   "bind <name> --app <app>",
		Short: "Bind a database to an app, injecting its credentials",
		Args:  cobra.ExactArgs(1),
	}
	var bindApp string
	bind.Flags().StringVarP(&bindApp, "app", "a", "", "app to bind (required)")
	_ = bind.MarkFlagRequired("app")
	bind.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := app.Portal().BindDatabase(cmd.Context(), t.Org, t.Project, t.Environment, args[0], bindApp); err != nil {
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
		if err := app.Portal().UnbindDatabase(cmd.Context(), t.Org, t.Project, t.Environment, args[0], unbindApp); err != nil {
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
			if err := confirm(cmd, app.Global.Yes, "Delete database %q in %s and all its data?", args[0], t); err != nil {
				return err
			}
			if err := app.Portal().DeleteDatabase(cmd.Context(), t.Org, t.Project, t.Environment, args[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Database %s deleted from %s.", args[0], t)
		}),
	}

	cmd.AddCommand(create, sizeLimitCmd, list, info, users, userAdd, extensions, extensionAdd, extensionRm, bind, unbind, remove, newDBServerCmd(app))
	return cmd
}

func newDBServerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run PostgreSQL servers in your own cloud",
		Long: "Run PostgreSQL servers in your own cloud.\n\n" +
			"A server is a managed PostgreSQL instance GoShip creates in a connected Google Cloud project, for one\n" +
			"environment: once ready it is where that environment's new databases go. The production server is\n" +
			"protected against deletion. Databases on a server are not billed by GoShip.",
	}

	list := newOrgListCmd(app, "list", "List the database servers of the current organization, or --all of them", (*portal.Client).DatabaseServers, skipForbiddenPlaces)
	list.RunE = refuseProjectFlag(list.RunE)

	create := &cobra.Command{
		Use:   "create --account <account> --region <region> [--environment <slug>] [--tier <tier>] [--name <name>]",
		Short: "Create a PostgreSQL server in a connected cloud account, for an environment",
		Args:  cobra.NoArgs,
	}
	var account, region, environment, tier, name, diskLimit string
	create.Flags().StringVar(&account, "account", "", "cloud account (id, label or provider) (required)")
	create.Flags().StringVar(&region, "region", "", "cloud region (required); see: goship cloud regions <account>")
	create.Flags().StringVar(&environment, "environment", "", "environment the server is for (see: goship environments); it becomes that environment's default server. Omit for a server any environment may use")
	create.Flags().StringVar(&tier, "tier", "db-f1-micro", "machine size; see: goship db server tiers")
	create.Flags().StringVar(&name, "name", "", "server name (default pg-<environment>, or pg-<account label> without --environment)")
	create.Flags().StringVar(&diskLimit, "disk-limit", "", "most the disk may grow to, in GB, or none (default 100)")
	_ = create.MarkFlagRequired("account")
	_ = create.MarkFlagRequired("region")
	create.RunE = cloudRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
		var diskCap *int
		if cmd.Flags().Changed("disk-limit") {
			gb, err := parseDiskGB(diskLimit)
			if err != nil {
				return err
			}
			diskCap = &gb
		}
		acc, err := deploy.ResolveCloudAccount(cmd.Context(), app.Portal(), org, account)
		if err != nil {
			return err
		}
		if name == "" {
			name = "pg-" + slugify(cmp.Or(environment, acc.Label))
		}
		s, err := app.Portal().CreateDatabaseServer(cmd.Context(), org, portal.CreateDatabaseServerRequest{
			CloudAccountID: acc.ID, Name: name, Region: region, Tier: tier, Environment: environment, DiskLimitGB: diskCap,
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
		RunE: cloudRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			list, err := app.Portal().DatabaseServerTiers(cmd.Context(), org)
			if err != nil {
				return err
			}
			return app.Renderer().Render(list)
		}),
	}

	diskLimitCmd := &cobra.Command{
		Use:   "disk-limit <server> <GB|none>",
		Short: "Set how far a server's disk may grow (none removes the cap)",
		Args:  cobra.ExactArgs(2),
		RunE: cloudRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			gb, err := parseDiskGB(args[1])
			if err != nil {
				return err
			}
			s, err := deploy.ResolveDatabaseServer(cmd.Context(), app.Portal(), Target{Org: org}, args[0])
			if err != nil {
				return err
			}
			if err := app.Portal().SetDatabaseServerDiskLimit(cmd.Context(), org, s.ID, gb); err != nil {
				return err
			}
			if gb == 0 {
				return app.Renderer().Message("Disk cap removed from server %s.", s.Name)
			}
			return app.Renderer().Message("Disk cap of server %s set to %d GB.", s.Name, gb)
		}),
	}

	retry := &cobra.Command{
		Use:   "retry <server>",
		Short: "Retry a server that failed to come up",
		Args:  cobra.ExactArgs(1),
		RunE: cloudRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			s, err := deploy.ResolveDatabaseServer(cmd.Context(), app.Portal(), Target{Org: org}, args[0])
			if err != nil {
				return err
			}
			if err := app.Portal().RetryDatabaseServer(cmd.Context(), org, s.ID); err != nil {
				return err
			}
			return app.Renderer().Message("Retrying server %s.", s.Name)
		}),
	}

	var bindEnv string
	bind := &cobra.Command{
		Use:   "bind <server> --environment <slug>",
		Short: "Give a server created for no environment to an environment, in place",
		Long: "Gives a server created for no environment to an environment. Its databases stay where they are;\n" +
			"new databases in that environment go on it. Binding to production turns deletion protection on,\n" +
			"without a restart. Refused while other environments use the server or keep databases on it.",
		Args: cobra.ExactArgs(1),
		RunE: cloudRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			s, err := deploy.ResolveDatabaseServer(cmd.Context(), app.Portal(), Target{Org: org}, args[0])
			if err != nil {
				return err
			}
			out, err := app.Portal().BindDatabaseServer(cmd.Context(), org, s.ID, bindEnv)
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(out)
			}
			if out.Protected {
				return app.Renderer().Message("Server %s joined %s and is protected against deletion.", s.Name, bindEnv)
			}
			return app.Renderer().Message("Server %s joined %s.", s.Name, bindEnv)
		}),
	}
	bind.Flags().StringVar(&bindEnv, "environment", "", "environment the server joins (required); see: goship environments")
	_ = bind.MarkFlagRequired("environment")

	pitr := &cobra.Command{
		Use:   "pitr <server>",
		Short: "Turn on point-in-time recovery (Google restarts the instance once)",
		Long: "Turns on point-in-time recovery, keeping seven days of transaction logs. Google restarts the\n" +
			"instance to turn it on: connections drop for about a minute. Nothing happens when it is on already.",
		Args: cobra.ExactArgs(1),
		RunE: cloudRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			s, err := deploy.ResolveDatabaseServer(cmd.Context(), app.Portal(), Target{Org: org}, args[0])
			if err != nil {
				return err
			}
			if s.PointInTimeRecovery {
				return app.Renderer().Message("Point-in-time recovery is on already for server %s.", s.Name)
			}
			if err := confirm(cmd, app.Global.Yes, "Google restarts server %s's instance to turn on point-in-time recovery; connections drop for about a minute. Continue?", s.Name); err != nil {
				return err
			}
			if _, err := app.Portal().EnableDatabaseServerPITR(cmd.Context(), org, s.ID); err != nil {
				return err
			}
			return app.Renderer().Message("Point-in-time recovery is being turned on for server %s; the instance restarts once.", s.Name)
		}),
	}

	var force bool
	del := &cobra.Command{
		Use:   "delete <server>",
		Short: "Delete a server (its databases must be deleted first)",
		Long: `Delete a server. Its databases must be deleted first; GoShip then removes the
Cloud SQL instance from your project.

--force drops the server from GoShip without touching your cloud: use it when
GoShip can no longer reach the instance (the cloud account was disconnected,
the project is gone, or every delete fails). The instance, if it still exists,
keeps running and billing in your project until you delete it there.`,
		Args: cobra.ExactArgs(1),
		RunE: cloudRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			s, err := deploy.ResolveDatabaseServer(cmd.Context(), app.Portal(), Target{Org: org}, args[0])
			if err != nil {
				return err
			}
			if !force {
				if err := confirm(cmd, app.Global.Yes, "Delete database server %q? The Cloud SQL instance is removed from your project.", s.Name); err != nil {
					return err
				}
				if err := app.Portal().DeleteDatabaseServer(cmd.Context(), org, s.ID); err != nil {
					return err
				}
				return app.Renderer().Message("Deleting server %s; the Cloud SQL instance is being removed from your project.", s.Name)
			}
			instance, project := s.CloudInstanceName, "<project id>"
			if instance == "" {
				instance = "(none recorded)"
			}
			if s.CloudAccount != nil && s.CloudAccount.Label != "" {
				project = s.CloudAccount.Label
			}
			if err := confirm(cmd, app.Global.Yes, "Forget database server %q? GoShip drops it without touching your cloud; Cloud SQL instance %s stays in your project if it still exists.", s.Name, instance); err != nil {
				return err
			}
			if err := app.Portal().ForgetDatabaseServer(cmd.Context(), org, s.ID); err != nil {
				return err
			}
			return app.Renderer().Message("Server %s forgotten. Cloud SQL instance %s was not touched; if it still exists, delete it in your project: gcloud sql instances delete %s --project %s", s.Name, instance, instance, project)
		}),
	}
	del.Flags().BoolVar(&force, "force", false, "drop the server from GoShip without touching the cloud (the instance stays in your project)")

	cmd.AddCommand(list, create, tiers, diskLimitCmd, retry, bind, pitr, del)
	return cmd
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "prod"
	}
	return s
}

// A slow CREATE EXTENSION (PostGIS on a small server) keeps running after
// the API answers "enabling"; the command waits for it to show as enabled.
var (
	extensionPollInterval = 3 * time.Second
	extensionPollDeadline = 3 * time.Minute
)

func waitForExtension(cmd *cobra.Command, app *App, t Target, db, ext string) error {
	deadline := time.Now().Add(extensionPollDeadline)
	for {
		exts, err := app.Portal().DatabaseExtensions(cmd.Context(), t.Org, t.Project, t.Environment, db)
		if err != nil {
			return err
		}
		if e, ok := exts.IsEnabled(ext); ok {
			return app.Renderer().Message("Extension %s %s enabled on %s.", e.Name, e.Version, db)
		}
		// Gone from both lists: the server gave up on the install (it is
		// rolled back, nothing is half-done). Enabling again reruns it.
		if !slices.Contains(exts.Pending, ext) {
			return fmt.Errorf("enabling %s did not complete on the server; run the command again", ext)
		}
		if time.Now().After(deadline) {
			return app.Renderer().Message("Still enabling; run `goship db extensions %s` later.", db)
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-time.After(extensionPollInterval):
		}
	}
}
