package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
)

type CloudAccountList struct {
	Accounts []portal.CloudAccount `json:"accounts"`
}

type CloudAccountArg struct {
	OrgArg
	AccountID string `json:"account_id" jsonschema:"cloud account id from goship_list_cloud_accounts"`
}

type CloudSizesArgs struct {
	CloudAccountArg
	Region string `json:"region" jsonschema:"region slug from goship_list_cloud_regions"`
}

type CloudRegionList struct {
	Regions []portal.CloudRegion `json:"regions"`
}

type CloudSizeList struct {
	Sizes []portal.CloudSize `json:"sizes"`
}

func (r *registry) cloud() {
	tool(r, "goship_list_cloud_accounts", "List the cloud accounts (AWS, Google Cloud, DigitalOcean…) connected to the organization. Connecting one needs a browser: tell the user to run `goship cloud connect <provider>`.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (CloudAccountList, error) {
			a, err := c.CloudAccounts(ctx, org)
			return CloudAccountList{Accounts: a}, err
		})
	tool(r, "goship_list_cloud_regions", "List the regions a connected cloud account can create nodes in.", read,
		func(ctx context.Context, c *portal.Client, org string, in CloudAccountArg) (CloudRegionList, error) {
			rg, err := c.CloudRegions(ctx, org, in.AccountID)
			return CloudRegionList{Regions: rg}, err
		})
	tool(r, "goship_list_cloud_sizes", "List the machine sizes and prices available in a region of a connected cloud account.", read,
		func(ctx context.Context, c *portal.Client, org string, in CloudSizesArgs) (CloudSizeList, error) {
			s, err := c.CloudSizes(ctx, org, in.AccountID, in.Region)
			return CloudSizeList{Sizes: s}, err
		})
	tool(r, "goship_delete_cloud_account", "Disconnect a cloud account. Nodes created from it keep running but can no longer be managed through it.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in CloudAccountArg) (Message, error) {
			if err := c.DeleteCloudAccount(ctx, org, in.AccountID); err != nil {
				return Message{}, err
			}
			return msgf("Cloud account disconnected.")
		})
}
