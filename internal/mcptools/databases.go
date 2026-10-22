package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type DBArg struct {
	ProjectArg
	Database string `json:"database" jsonschema:"database name"`
}

type DBList struct {
	Databases []portal.Database `json:"databases"`
}

type DBUserList struct {
	Users []portal.DatabaseUser `json:"users"`
}

type CreateDBArgs struct {
	ProjectArg
	Name        string `json:"name" jsonschema:"database name"`
	Plan        string `json:"plan" jsonschema:"database plan slug (goship_list_plans with kind=database)"`
	Description string `json:"description,omitempty"`
	Server      string `json:"server,omitempty" jsonschema:"place it on one of the customer's own database servers, by name or id (goship_list_database_servers); default: GoShip's managed server"`
}

type CreateDBUserArgs struct {
	DBArg
	Username   string `json:"username"`
	AccessMode string `json:"access_mode,omitempty" jsonschema:"readwrite (default) or readonly"`
}

type EnableDBExtensionArgs struct {
	DBArg
	Extension string `json:"extension" jsonschema:"extension name from the available list, e.g. postgis"`
}

type DBBindArgs struct {
	DBArg
	App string `json:"app" jsonschema:"app to bind; it gets DATABASE_URL and the PG* variables"`
}

func (r *registry) databases() {
	tool(r, "goship_list_databases", "List the managed PostgreSQL databases in a project.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, _ ProjectArg) (DBList, error) {
			dbs, err := c.Databases(ctx, t.Org, t.Project)
			return DBList{Databases: dbs}, err
		})
	tool(r, "goship_get_database", "Show one database, including its connection details.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBArg) (*portal.Database, error) {
			return c.Database(ctx, t.Org, t.Project, in.Database)
		})
	tool(r, "goship_database_stats", "Show a database's size and connection count against its plan's limits.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBArg) (*portal.DatabaseStats, error) {
			return c.DatabaseStats(ctx, t.Org, t.Project, in.Database)
		})
	tool(r, "goship_list_database_users", "List the extra users of a database.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBArg) (DBUserList, error) {
			users, err := c.DatabaseUsers(ctx, t.Org, t.Project, in.Database)
			return DBUserList{Users: users}, err
		})
	tool(r, "goship_create_database_user", "Create a database user. The password is returned once, here only.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in CreateDBUserArgs) (*portal.NewDatabaseUser, error) {
			return c.CreateDatabaseUser(ctx, t.Org, t.Project, in.Database, in.Username, in.AccessMode)
		})
	tool(r, "goship_list_database_extensions", "List a database's PostgreSQL extensions: enabled, still enabling, and available to enable.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBArg) (*portal.DatabaseExtensions, error) {
			return c.DatabaseExtensions(ctx, t.Org, t.Project, in.Database)
		})
	tool(r, "goship_enable_database_extension", "Enable a PostgreSQL extension (postgis, pg_trgm, citext, ...) on a database. Status enabling means the server is still installing it; list the extensions again until it shows as enabled.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in EnableDBExtensionArgs) (*portal.DatabaseExtension, error) {
			return c.EnableDatabaseExtension(ctx, t.Org, t.Project, in.Database, in.Extension)
		})
	tool(r, "goship_disable_database_extension", "Disable a PostgreSQL extension on a database; refused while tables still use it. vector (pgvector) is always on.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in EnableDBExtensionArgs) (Message, error) {
			if err := c.DisableDatabaseExtension(ctx, t.Org, t.Project, in.Database, in.Extension); err != nil {
				return Message{}, err
			}
			return msgf("Extension %s disabled on %s.", in.Extension, in.Database)
		})
	tool(r, "goship_create_database", "Create a managed PostgreSQL database. On GoShip's server the plan is billed; on one of the customer's own database servers (server) the same plan sets the quotas but nothing is billed by GoShip, and a server still provisioning queues the database until it is ready (status pending, then ready).", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in CreateDBArgs) (Message, error) {
			var serverID string
			if in.Server != "" {
				s, err := deploy.ResolveDatabaseServer(ctx, c, t, in.Server)
				if err != nil {
					return Message{}, err
				}
				serverID = s.ID
			}
			if err := c.CreateDatabase(ctx, t.Org, t.Project, in.Name, in.Plan, in.Description, serverID, nil); err != nil {
				return Message{}, err
			}
			if serverID != "" {
				return msgf("Database %s is being created on server %s; it is queued while the server is still provisioning.", in.Name, in.Server)
			}
			return msgf("Database %s is being created.", in.Name)
		})
	tool(r, "goship_delete_database", "Delete a database and all of its data.", destructive,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBArg) (Message, error) {
			if err := c.DeleteDatabase(ctx, t.Org, t.Project, in.Database); err != nil {
				return Message{}, err
			}
			return msgf("Database %s deleted.", in.Database)
		})
	tool(r, "goship_bind_database", "Bind a database to an app, injecting its connection variables.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBBindArgs) (Message, error) {
			if err := c.BindDatabase(ctx, t.Org, t.Project, in.Database, in.App); err != nil {
				return Message{}, err
			}
			return msgf("Database %s bound to %s.", in.Database, in.App)
		})
	tool(r, "goship_unbind_database", "Unbind a database from an app, removing its connection variables. The data stays.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBBindArgs) (Message, error) {
			if err := c.UnbindDatabase(ctx, t.Org, t.Project, in.Database, in.App); err != nil {
				return Message{}, err
			}
			return msgf("Database %s unbound from %s.", in.Database, in.App)
		})
}
