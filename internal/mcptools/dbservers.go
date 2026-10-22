package mcptools

import (
	"context"
	"errors"
	"fmt"
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
	ProjectArg
	Server string `json:"server" jsonschema:"database server name or id (goship_list_database_servers)"`
}

type CreateDBServerArgs struct {
	ProjectArg
	CloudAccount string `json:"cloud_account" jsonschema:"connected Google Cloud account: id, label or provider (goship_list_cloud_accounts)"`
	Region       string `json:"region" jsonschema:"cloud region slug (goship_list_cloud_regions)"`
	Tier         string `json:"tier,omitempty" jsonschema:"machine size slug (goship_list_database_server_tiers); default db-f1-micro"`
	Name         string `json:"name,omitempty" jsonschema:"server name; default pg-<account label>"`
	DiskLimitGB  *int   `json:"disk_limit_gb,omitempty" jsonschema:"cap in GB on how far the server's disk may auto-grow (minimum 10); 0 means no cap; default 100"`
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
	tool(r, "goship_list_database_servers", "List the PostgreSQL servers in the customer's own cloud that the project can place databases on: its own and the organization's (scope says which). Databases on them are not billed by GoShip.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, _ ProjectArg) (DBServerList, error) {
			s, err := c.DatabaseServers(ctx, t.Org, t.Project)
			return DBServerList{Servers: s}, err
		})
	tool(r, "goship_list_database_server_tiers", "List the machine sizes a database server can have, with the cloud provider's price.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, _ ProjectArg) (DBServerTierList, error) {
			tiers, err := c.DatabaseServerTiers(ctx, t.Org, t.Project)
			return DBServerTierList{Tiers: tiers}, err
		})
	tool(r, "goship_create_database_server", "Create a PostgreSQL server (a Cloud SQL instance) in a connected Google Cloud account. The cloud provider bills the instance; GoShip bills nothing for it or the databases on it. Provisioning takes about ten minutes in the background: goship_list_database_servers shows the stage, and databases created on it meanwhile are queued until it is ready. The disk auto-grows up to disk_limit_gb (default 100 GB). One server per cloud account.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in CreateDBServerArgs) (*portal.DatabaseServer, error) {
			acc, err := resolveCloudAccount(ctx, c, t, in.CloudAccount)
			if err != nil {
				return nil, err
			}
			scope := t
			if acc.Scope == "org" {
				scope.Project = ""
			}
			name, tier := in.Name, in.Tier
			if name == "" {
				name = "pg-" + slugify(acc.Label)
			}
			if tier == "" {
				tier = "db-f1-micro"
			}
			return c.CreateDatabaseServer(ctx, scope.Org, scope.Project, portal.CreateDatabaseServerRequest{
				CloudAccountID: acc.ID, Name: name, Region: in.Region, Tier: tier, DiskLimitGB: in.DiskLimitGB,
			})
		})
	tool(r, "goship_set_database_server_disk_limit", "Set or remove the disk cap of a database server: how far its disk may auto-grow (0 removes the cap). The cap must be at least 10 GB and not below the disk's current size; the cloud bill grows with the disk, so it is the customer's spending ceiling.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in SetDBServerDiskLimitArgs) (Message, error) {
			if in.DiskLimitGB < 0 {
				return Message{}, errors.New("disk_limit_gb cannot be negative; 0 removes the cap")
			}
			s, err := deploy.ResolveDatabaseServer(ctx, c, t, in.Server)
			if err != nil {
				return Message{}, err
			}
			st := deploy.ServerTarget(s, t)
			if err := c.SetDatabaseServerDiskLimit(ctx, st.Org, st.Project, s.ID, in.DiskLimitGB); err != nil {
				return Message{}, err
			}
			if in.DiskLimitGB == 0 {
				return msgf("Disk cap removed from server %s.", s.Name)
			}
			return msgf("Disk cap of server %s set to %d GB.", s.Name, in.DiskLimitGB)
		})
	tool(r, "goship_retry_database_server", "Retry a database server that failed to come up, after fixing the cause its error_message names.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DBServerArg) (Message, error) {
			s, err := deploy.ResolveDatabaseServer(ctx, c, t, in.Server)
			if err != nil {
				return Message{}, err
			}
			st := deploy.ServerTarget(s, t)
			if err := c.RetryDatabaseServer(ctx, st.Org, st.Project, s.ID); err != nil {
				return Message{}, err
			}
			return msgf("Retrying server %s.", s.Name)
		})
	tool(r, "goship_delete_database_server", "Delete a database server: its databases must be deleted first, then GoShip removes the Cloud SQL instance from the customer's project. With force, GoShip only forgets the server and the instance stays.", destructive,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DeleteDBServerArgs) (Message, error) {
			s, err := deploy.ResolveDatabaseServer(ctx, c, t, in.Server)
			if err != nil {
				return Message{}, err
			}
			st := deploy.ServerTarget(s, t)
			if in.Force {
				if err := c.ForgetDatabaseServer(ctx, st.Org, st.Project, s.ID); err != nil {
					return Message{}, err
				}
				instance := s.CloudInstanceName
				if instance == "" {
					instance = "(none recorded)"
				}
				return msgf("Server %s forgotten. The Cloud SQL instance %s, if it still exists, keeps running in the customer's project: `gcloud sql instances delete %s --project <project id>` removes it.", s.Name, instance, instance)
			}
			if err := c.DeleteDatabaseServer(ctx, st.Org, st.Project, s.ID); err != nil {
				return Message{}, err
			}
			return msgf("Deleting server %s; the Cloud SQL instance is being removed from the customer's project.", s.Name)
		})
}

// resolveCloudAccount matches what the caller passed against the project's
// view of the cloud accounts: the id, then an exact label, then the provider
// when exactly one account uses it.
func resolveCloudAccount(ctx context.Context, c *portal.Client, t deploy.Target, ref string) (*portal.CloudAccount, error) {
	accounts, err := c.CloudAccounts(ctx, t.Org, t.Project)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == ref {
			return &accounts[i], nil
		}
	}
	for i := range accounts {
		if accounts[i].Label == ref {
			return &accounts[i], nil
		}
	}
	var byProvider []*portal.CloudAccount
	for i := range accounts {
		if accounts[i].Provider == ref {
			byProvider = append(byProvider, &accounts[i])
		}
	}
	switch len(byProvider) {
	case 1:
		return byProvider[0], nil
	case 0:
		return nil, fmt.Errorf("no cloud account %q in %s; goship_list_cloud_accounts shows them", ref, t)
	default:
		return nil, fmt.Errorf("more than one %s account in %s; pass the id", ref, t)
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "prod"
	}
	return s
}
