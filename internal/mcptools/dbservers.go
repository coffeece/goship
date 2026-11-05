package mcptools

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type DBServerList struct {
	Servers []portal.DatabaseServer `json:"servers"`
}

type DBServerTierList struct {
	Tiers []portal.DatabaseServerTier `json:"tiers"`
}

type DBServerArg struct {
	OrgArg
	Server string `json:"server" jsonschema:"database server name or id (goship_list_database_servers)"`
}

type CreateDBServerArgs struct {
	OrgArg
	CloudAccount string `json:"cloud_account" jsonschema:"connected Google Cloud account: id, label or provider (goship_list_cloud_accounts)"`
	Region       string `json:"region" jsonschema:"cloud region slug (goship_list_cloud_regions)"`
	Environment  string `json:"environment,omitempty" jsonschema:"environment slug the server is for (goship_list_environments); once ready it becomes that environment's default server, and a production server gets deletion protection. Omit for a server any environment may use"`
	Tier         string `json:"tier,omitempty" jsonschema:"machine size slug (goship_list_database_server_tiers); default db-f1-micro"`
	Name         string `json:"name,omitempty" jsonschema:"server name; default pg-<environment>, or pg-<account label> without an environment"`
	DiskLimitGB  *int   `json:"disk_limit_gb,omitempty" jsonschema:"cap in GB on how far the server's disk may auto-grow (minimum 10); 0 means no cap; default 100"`
}

type BindDBServerArgs struct {
	DBServerArg
	Environment string `json:"environment" jsonschema:"environment slug the server joins (goship_list_environments)"`
}

type SetDBServerDiskLimitArgs struct {
	DBServerArg
	DiskLimitGB int `json:"disk_limit_gb" jsonschema:"cap in GB on how far the server's disk may auto-grow (minimum 10, not below its current size); 0 removes the cap"`
}

type DeleteDBServerArgs struct {
	DBServerArg
	Force bool `json:"force,omitempty" jsonschema:"forget the server in GoShip without touching the cloud, when GoShip can no longer reach the instance; it keeps running and billing in the customer's project"`
}

func (r *registry) dbservers() {
	orgTool(r, "goship_list_database_servers", "List the organization's PostgreSQL servers in its own cloud. Any project can place databases on them (goship_create_database with server), and an environment can make one the default for its new databases. Databases on them are not billed by GoShip.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (DBServerList, error) {
			s, err := c.DatabaseServers(ctx, org)
			return DBServerList{Servers: s}, err
		})
	orgTool(r, "goship_list_database_server_tiers", "List the machine sizes a database server can have, with the cloud provider's price.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (DBServerTierList, error) {
			tiers, err := c.DatabaseServerTiers(ctx, org)
			return DBServerTierList{Tiers: tiers}, err
		})
	orgTool(r, "goship_create_database_server", "Create a PostgreSQL server (a Cloud SQL instance) in a connected Google Cloud account. The cloud provider bills the instance; GoShip bills nothing for it or the databases on it. Provisioning takes about ten minutes in the background: goship_list_database_servers shows the stage, and databases created on it meanwhile are queued until it is ready. The disk auto-grows up to disk_limit_gb (default 100 GB). One server per environment; a cloud account may hold several.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateDBServerArgs) (*portal.DatabaseServer, error) {
			acc, err := deploy.ResolveCloudAccount(ctx, c, org, in.CloudAccount)
			if err != nil {
				return nil, err
			}
			name, tier := in.Name, in.Tier
			if name == "" && in.Environment != "" {
				name = "pg-" + slugify(in.Environment)
			}
			if name == "" {
				name = "pg-" + slugify(acc.Label)
			}
			if tier == "" {
				tier = "db-f1-micro"
			}
			return c.CreateDatabaseServer(ctx, org, portal.CreateDatabaseServerRequest{
				CloudAccountID: acc.ID, Name: name, Region: in.Region, Tier: tier, Environment: in.Environment, DiskLimitGB: in.DiskLimitGB,
			})
		})
	orgTool(r, "goship_set_database_server_disk_limit", "Set or remove the disk cap of a database server: how far its disk may auto-grow (0 removes the cap). The cap must be at least 10 GB and not below the disk's current size; the cloud bill grows with the disk, so it is the customer's spending ceiling.", write,
		func(ctx context.Context, c *portal.Client, org string, in SetDBServerDiskLimitArgs) (Message, error) {
			if in.DiskLimitGB < 0 {
				return Message{}, errors.New("disk_limit_gb cannot be negative; 0 removes the cap")
			}
			s, err := deploy.ResolveDatabaseServer(ctx, c, deploy.Target{Org: org}, in.Server)
			if err != nil {
				return Message{}, err
			}
			if err := c.SetDatabaseServerDiskLimit(ctx, org, s.ID, in.DiskLimitGB); err != nil {
				return Message{}, err
			}
			if in.DiskLimitGB == 0 {
				return msgf("Disk cap removed from server %s.", s.Name)
			}
			return msgf("Disk cap of server %s set to %d GB.", s.Name, in.DiskLimitGB)
		})
	orgTool(r, "goship_retry_database_server", "Retry a database server that failed to come up, after fixing the cause its error_message names.", write,
		func(ctx context.Context, c *portal.Client, org string, in DBServerArg) (Message, error) {
			s, err := deploy.ResolveDatabaseServer(ctx, c, deploy.Target{Org: org}, in.Server)
			if err != nil {
				return Message{}, err
			}
			if err := c.RetryDatabaseServer(ctx, org, s.ID); err != nil {
				return Message{}, err
			}
			return msgf("Retrying server %s.", s.Name)
		})
	orgTool(r, "goship_bind_database_server", "Give a database server created for no environment to an environment, in place: its databases stay where they are and new databases in that environment go on it. Binding to production turns deletion protection on without a restart. Refused while other environments use the server or keep databases on it.", write,
		func(ctx context.Context, c *portal.Client, org string, in BindDBServerArgs) (*portal.DatabaseServer, error) {
			s, err := deploy.ResolveDatabaseServer(ctx, c, deploy.Target{Org: org}, in.Server)
			if err != nil {
				return nil, err
			}
			return c.BindDatabaseServer(ctx, org, s.ID, in.Environment)
		})
	orgTool(r, "goship_enable_database_server_pitr", "Turn on point-in-time recovery for a database server (seven days of transaction logs). Google restarts the instance to turn it on: its connections drop for about a minute. Does nothing when it is on already (point_in_time_recovery in goship_list_database_servers).", destructive,
		func(ctx context.Context, c *portal.Client, org string, in DBServerArg) (*portal.DatabaseServer, error) {
			s, err := deploy.ResolveDatabaseServer(ctx, c, deploy.Target{Org: org}, in.Server)
			if err != nil {
				return nil, err
			}
			if s.PointInTimeRecovery {
				return s, nil
			}
			return c.EnableDatabaseServerPITR(ctx, org, s.ID)
		})
	orgTool(r, "goship_delete_database_server", "Delete a database server: its databases must be deleted first, then GoShip removes the Cloud SQL instance from the customer's project. With force, GoShip only forgets the server and the instance stays.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in DeleteDBServerArgs) (Message, error) {
			s, err := deploy.ResolveDatabaseServer(ctx, c, deploy.Target{Org: org}, in.Server)
			if err != nil {
				return Message{}, err
			}
			if in.Force {
				if err := c.ForgetDatabaseServer(ctx, org, s.ID); err != nil {
					return Message{}, err
				}
				instance := s.CloudInstanceName
				if instance == "" {
					instance = "(none recorded)"
				}
				return msgf("Server %s forgotten. The Cloud SQL instance %s, if it still exists, keeps running in the customer's project: `gcloud sql instances delete %s --project <project id>` removes it.", s.Name, instance, instance)
			}
			if err := c.DeleteDatabaseServer(ctx, org, s.ID); err != nil {
				return Message{}, err
			}
			return msgf("Deleting server %s; the Cloud SQL instance is being removed from the customer's project.", s.Name)
		})
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "prod"
	}
	return s
}
