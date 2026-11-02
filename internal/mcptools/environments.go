package mcptools

import (
	"cmp"
	"context"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type EnvironmentList struct {
	Environments []portal.Environment `json:"environments"`
}

type CreateEnvironmentArgs struct {
	OrgArg
	Slug     string `json:"slug" jsonschema:"environment slug: lowercase letters, digits and hyphens, e.g. staging"`
	Name     string `json:"name,omitempty" jsonschema:"display name; defaults to the slug"`
	Cluster  string `json:"cluster,omitempty" jsonschema:"cluster to run on, by name or id (goship_list_clusters); default: GoShip's shared cluster"`
	DBServer string `json:"db_server,omitempty" jsonschema:"database server new databases go on, by name or id (goship_list_database_servers); default: GoShip's PostgreSQL"`
}

type EnvironmentArg struct {
	OrgArg
	Environment string `json:"environment" jsonschema:"environment slug (goship_list_environments)"`
}

func (r *registry) environments() {
	orgTool(r, "goship_list_environments", "List the organization's environments (development, staging, production, ...) with the cluster each runs on and whether it is enabled (new work is refused in a disabled one); is_default marks the one a tool uses when neither the call nor the server selected an environment. Every project exists in every environment: pass a slug as environment to the project tools.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (EnvironmentList, error) {
			envs, err := c.Environments(ctx, org)
			return EnvironmentList{Environments: envs}, err
		})
	orgTool(r, "goship_create_environment", "Create an environment in the organization; every project gets it. It runs on GoShip's shared cluster and PostgreSQL unless cluster and db_server name ones of the organization's own.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateEnvironmentArgs) (*portal.Environment, error) {
			req := portal.CreateEnvironmentRequest{Slug: in.Slug, Name: cmp.Or(in.Name, in.Slug)}
			if in.Cluster != "" {
				id, err := deploy.ResolveCluster(ctx, c, org, in.Cluster)
				if err != nil {
					return nil, err
				}
				req.ClusterID = id
			}
			if in.DBServer != "" {
				s, err := deploy.ResolveDatabaseServer(ctx, c, deploy.Target{Org: org}, in.DBServer)
				if err != nil {
					return nil, err
				}
				req.DBServerID = s.ID
			}
			return c.CreateEnvironment(ctx, org, req)
		})
	orgTool(r, "goship_enable_environment", "Enable an environment (staging is off until enabled) so projects can create apps, databases and volumes in it. Needs an organization admin.", write,
		func(ctx context.Context, c *portal.Client, org string, in EnvironmentArg) (*portal.Environment, error) {
			return c.UpdateEnvironment(ctx, org, in.Environment, true)
		})
	orgTool(r, "goship_disable_environment", "Disable an environment so no new apps, databases or volumes are created in it. Existing resources keep running. Refused for the default environment and while it still holds anything; the refusal says why.", write,
		func(ctx context.Context, c *portal.Client, org string, in EnvironmentArg) (*portal.Environment, error) {
			return c.UpdateEnvironment(ctx, org, in.Environment, false)
		})
	orgTool(r, "goship_delete_environment", "Delete an environment from every project of the organization. Refused while any project still holds apps, databases or volumes in it; the refusal names them.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in EnvironmentArg) (Message, error) {
			// The refusal is the API's own words: it names what is left.
			if err := c.DeleteEnvironment(ctx, org, in.Environment); err != nil {
				return Message{}, err
			}
			return msgf("Environment %s deleted.", in.Environment)
		})
}
