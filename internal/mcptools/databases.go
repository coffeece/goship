package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
)

type DBArg struct {
	OrgArg
	Database string `json:"database" jsonschema:"database name"`
}

type DBList struct {
	Databases []portal.Database `json:"databases"`
}

type DBUserList struct {
	Users []portal.DatabaseUser `json:"users"`
}

type CreateDBArgs struct {
	OrgArg
	Name        string `json:"name" jsonschema:"database name"`
	Plan        string `json:"plan" jsonschema:"database plan slug (goship_list_plans with kind=database)"`
	Description string `json:"description,omitempty"`
}

type CreateDBUserArgs struct {
	DBArg
	Username   string `json:"username"`
	AccessMode string `json:"access_mode,omitempty" jsonschema:"readwrite (default) or readonly"`
}

type DBBindArgs struct {
	DBArg
	App string `json:"app" jsonschema:"app to bind; it gets DATABASE_URL and the PG* variables"`
}

func (r *registry) databases() {
	tool(r, "goship_list_databases", "List the managed PostgreSQL databases in an organization.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (DBList, error) {
			dbs, err := c.Databases(ctx, org)
			return DBList{Databases: dbs}, err
		})
	tool(r, "goship_get_database", "Show one database, including its connection details.", read,
		func(ctx context.Context, c *portal.Client, org string, in DBArg) (*portal.Database, error) {
			return c.Database(ctx, org, in.Database)
		})
	tool(r, "goship_database_stats", "Show a database's size and connection count against its plan's limits.", read,
		func(ctx context.Context, c *portal.Client, org string, in DBArg) (*portal.DatabaseStats, error) {
			return c.DatabaseStats(ctx, org, in.Database)
		})
	tool(r, "goship_list_database_users", "List the extra users of a database.", read,
		func(ctx context.Context, c *portal.Client, org string, in DBArg) (DBUserList, error) {
			users, err := c.DatabaseUsers(ctx, org, in.Database)
			return DBUserList{Users: users}, err
		})
	tool(r, "goship_create_database_user", "Create a database user. The password is returned once, here only.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateDBUserArgs) (*portal.NewDatabaseUser, error) {
			return c.CreateDatabaseUser(ctx, org, in.Database, in.Username, in.AccessMode)
		})
	tool(r, "goship_create_database", "Create a managed PostgreSQL database.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateDBArgs) (Message, error) {
			if err := c.CreateDatabase(ctx, org, in.Name, in.Plan, in.Description); err != nil {
				return Message{}, err
			}
			return msgf("Database %s is being created.", in.Name)
		})
	tool(r, "goship_delete_database", "Delete a database and all of its data.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in DBArg) (Message, error) {
			if err := c.DeleteDatabase(ctx, org, in.Database); err != nil {
				return Message{}, err
			}
			return msgf("Database %s deleted.", in.Database)
		})
	tool(r, "goship_bind_database", "Bind a database to an app, injecting its connection variables.", write,
		func(ctx context.Context, c *portal.Client, org string, in DBBindArgs) (Message, error) {
			if err := c.BindDatabase(ctx, org, in.Database, in.App); err != nil {
				return Message{}, err
			}
			return msgf("Database %s bound to %s.", in.Database, in.App)
		})
	tool(r, "goship_unbind_database", "Unbind a database from an app, removing its connection variables. The data stays.", write,
		func(ctx context.Context, c *portal.Client, org string, in DBBindArgs) (Message, error) {
			if err := c.UnbindDatabase(ctx, org, in.Database, in.App); err != nil {
				return Message{}, err
			}
			return msgf("Database %s unbound from %s.", in.Database, in.App)
		})
}
