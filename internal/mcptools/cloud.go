package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type CloudAccountList struct {
	Accounts []portal.CloudAccount `json:"accounts"`
}

type CloudAccountArg struct {
	OrgArg
	AccountID string `json:"account_id" jsonschema:"cloud account id from goship_list_cloud_accounts"`
	Project   string `json:"project,omitempty" jsonschema:"act through this project instead of the organization; for an account connected to a project"`
}

type CloudSizesArgs struct {
	CloudAccountArg
	Region string `json:"region" jsonschema:"region slug from goship_list_cloud_regions"`
}

type MoveCloudAccountArgs struct {
	OrgArg
	AccountID string `json:"account_id" jsonschema:"cloud account id from goship_list_cloud_accounts"`
	ToProject string `json:"to_project,omitempty" jsonschema:"project slug the account moves to; omit to share it with the whole organization"`
}

type CloudRegionList struct {
	Regions []portal.CloudRegion `json:"regions"`
}

type CloudSizeList struct {
	Sizes []portal.CloudSize `json:"sizes"`
}

func (r *registry) cloud() {
	tool(r, "goship_list_cloud_accounts", "List the cloud accounts (AWS, Google Cloud, DigitalOcean…) the project can use, its own and the organization's (scope says which). Nodes are created in them, and a Google Cloud account can also host a database server. Connecting one needs a browser: tell the user to run `goship cloud connect <provider>`.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, _ ProjectArg) (CloudAccountList, error) {
			a, err := c.CloudAccounts(ctx, t.Org, t.Project)
			return CloudAccountList{Accounts: a}, err
		})
	orgTool(r, "goship_list_cloud_regions", "List the regions a connected cloud account can create nodes in.", read,
		func(ctx context.Context, c *portal.Client, org string, in CloudAccountArg) (CloudRegionList, error) {
			rg, err := c.CloudRegions(ctx, org, in.Project, in.AccountID)
			return CloudRegionList{Regions: rg}, err
		})
	orgTool(r, "goship_list_cloud_sizes", "List the machine sizes and prices available in a region of a connected cloud account.", read,
		func(ctx context.Context, c *portal.Client, org string, in CloudSizesArgs) (CloudSizeList, error) {
			s, err := c.CloudSizes(ctx, org, in.Project, in.AccountID, in.Region)
			return CloudSizeList{Sizes: s}, err
		})
	orgTool(r, "goship_move_cloud_account", "Move a cloud account to a project, or share it with the whole organization (no to_project). An organization's account is inherited by every project; a project's is used by that project only. Refused while the target already has an account of that provider, or while a node or database server of another project runs on it.", write,
		func(ctx context.Context, c *portal.Client, org string, in MoveCloudAccountArgs) (Message, error) {
			if err := c.MoveCloudAccount(ctx, org, in.AccountID, in.ToProject); err != nil {
				return Message{}, err
			}
			if in.ToProject == "" {
				return msgf("Cloud account %s is now shared with the organization.", in.AccountID)
			}
			return msgf("Cloud account %s moved to project %s.", in.AccountID, in.ToProject)
		})
	orgTool(r, "goship_delete_cloud_account", "Disconnect a cloud account. Nodes created from it keep running but can no longer be managed through it.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in CloudAccountArg) (Message, error) {
			if err := c.DeleteCloudAccount(ctx, org, in.Project, in.AccountID); err != nil {
				return Message{}, err
			}
			return msgf("Cloud account disconnected.")
		})
}
