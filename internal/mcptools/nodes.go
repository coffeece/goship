package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type NodeList struct {
	Nodes []portal.Node `json:"nodes"`
}

type NodeArg struct {
	OrgArg
	Node string `json:"node" jsonschema:"node name or id"`
}

type CreateNodeArgs struct {
	OrgArg
	Name           string `json:"name" jsonschema:"node name"`
	Host           string `json:"host,omitempty" jsonschema:"SSH host of a machine you already own; omit to create one on a connected cloud account"`
	Port           int    `json:"port,omitempty" jsonschema:"SSH port, default 22"`
	SSHUser        string `json:"ssh_user,omitempty" jsonschema:"SSH user, default root"`
	SSHKey         string `json:"ssh_key,omitempty" jsonschema:"private key with access to the host"`
	CloudAccountID string `json:"cloud_account_id,omitempty" jsonschema:"connected cloud account to create the machine in (goship_list_cloud_accounts)"`
	Region         string `json:"region,omitempty" jsonschema:"cloud region slug (goship_list_cloud_regions)"`
	Size           string `json:"size,omitempty" jsonschema:"cloud machine size slug (goship_list_cloud_sizes)"`
}

type DeleteNodeArgs struct {
	NodeArg
	DestroyServer bool `json:"destroy_server,omitempty" jsonschema:"also destroy the cloud machine the node runs on"`
}

func (r *registry) nodes() {
	tool(r, "goship_list_nodes", "List the organization's own nodes (BYON) and their status.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (NodeList, error) {
			n, err := c.Nodes(ctx, org)
			return NodeList{Nodes: n}, err
		})
	tool(r, "goship_get_node", "Show one node, including its provisioning stage and last error.", read,
		func(ctx context.Context, c *portal.Client, org string, in NodeArg) (*portal.Node, error) {
			id, err := deploy.ResolveNode(ctx, c, org, in.Node)
			if err != nil {
				return nil, err
			}
			return c.Node(ctx, org, id)
		})
	tool(r, "goship_create_node", "Add a node: either a machine you already own over SSH, or a new machine in a connected cloud account. Provisioning continues in the background; goship_get_node shows its stage. Nodes are billed monthly.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateNodeArgs) (*portal.Node, error) {
			return c.CreateNode(ctx, org, portal.CreateNodeRequest{
				Name: in.Name, Host: in.Host, Port: in.Port, SSHUser: in.SSHUser, SSHKey: in.SSHKey,
				CloudAccountID: in.CloudAccountID, Region: in.Region, Size: in.Size,
			})
		})
	tool(r, "goship_delete_node", "Remove a node and every app on it; with destroy_server the cloud machine is destroyed too.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in DeleteNodeArgs) (Message, error) {
			id, err := deploy.ResolveNode(ctx, c, org, in.Node)
			if err != nil {
				return Message{}, err
			}
			if err := c.DeleteNode(ctx, org, id, in.DestroyServer); err != nil {
				return Message{}, err
			}
			return msgf("Node %s removed.", in.Node)
		})
}
