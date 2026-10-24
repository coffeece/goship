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
	ProjectArg
	Node string `json:"node" jsonschema:"node name or id"`
}

type CreateNodeArgs struct {
	ProjectArg
	Name           string `json:"name" jsonschema:"node name"`
	Host           string `json:"host,omitempty" jsonschema:"SSH host of a machine you already own; omit to create one on a connected cloud account"`
	Port           int    `json:"port,omitempty" jsonschema:"SSH port, default 22"`
	SSHUser        string `json:"ssh_user,omitempty" jsonschema:"SSH user, default root"`
	SSHKey         string `json:"ssh_key,omitempty" jsonschema:"private key with access to the host"`
	CloudAccountID string `json:"cloud_account_id,omitempty" jsonschema:"connected cloud account to create the machine in (goship_list_cloud_accounts)"`
	Region         string `json:"region,omitempty" jsonschema:"cloud region slug (goship_list_cloud_regions)"`
	Size           string `json:"size,omitempty" jsonschema:"cloud machine size slug (goship_list_cloud_sizes)"`
}

type NodeSizeList struct {
	Sizes []portal.ResizeOption `json:"sizes"`
}

type ResizeNodeArgs struct {
	NodeArg
	Size string `json:"size" jsonschema:"machine size slug from goship_list_node_sizes"`
}

type DeleteNodeArgs struct {
	NodeArg
	DestroyServer bool `json:"destroy_server,omitempty" jsonschema:"also destroy the cloud machine the node runs on"`
}

func (r *registry) nodes() {
	tool(r, "goship_list_nodes", "List the project's own nodes (BYON) and their status.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, _ ProjectArg) (NodeList, error) {
			n, err := c.Nodes(ctx, t.Org, t.Project)
			return NodeList{Nodes: n}, err
		})
	tool(r, "goship_get_node", "Show one node, including its provisioning stage and last error.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in NodeArg) (*portal.Node, error) {
			id, err := deploy.ResolveNode(ctx, c, t, in.Node)
			if err != nil {
				return nil, err
			}
			return c.Node(ctx, t.Org, t.Project, id)
		})
	tool(r, "goship_create_node", "Add a node: either a machine you already own over SSH, or a new machine in a connected cloud account. Provisioning continues in the background; goship_get_node shows its stage. Nodes are billed monthly.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in CreateNodeArgs) (*portal.Node, error) {
			return c.CreateNode(ctx, t.Org, t.Project, portal.CreateNodeRequest{
				Name: in.Name, Host: in.Host, Port: in.Port, SSHUser: in.SSHUser, SSHKey: in.SSHKey,
				CloudAccountID: in.CloudAccountID, Region: in.Region, Size: in.Size,
			})
		})
	tool(r, "goship_list_node_sizes", "List the machine sizes a node can be resized to, with prices; the current one comes first.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in NodeArg) (NodeSizeList, error) {
			id, err := deploy.ResolveNode(ctx, c, t, in.Node)
			if err != nil {
				return NodeSizeList{}, err
			}
			s, err := c.NodeSizes(ctx, t.Org, t.Project, id)
			return NodeSizeList{Sizes: s}, err
		})
	tool(r, "goship_resize_node", "Change a node's machine size in place. Every app on the node is down for a few minutes while the machine restarts.", destructive,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in ResizeNodeArgs) (Message, error) {
			id, err := deploy.ResolveNode(ctx, c, t, in.Node)
			if err != nil {
				return Message{}, err
			}
			if _, err := c.ResizeNode(ctx, t.Org, t.Project, id, in.Size); err != nil {
				return Message{}, err
			}
			return msgf("Node %s is resizing to %s. goship_get_node shows its stage.", in.Node, in.Size)
		})
	tool(r, "goship_delete_node", "Remove a node and every app on it; with destroy_server the cloud machine is destroyed too.", destructive,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in DeleteNodeArgs) (Message, error) {
			id, err := deploy.ResolveNode(ctx, c, t, in.Node)
			if err != nil {
				return Message{}, err
			}
			if err := c.DeleteNode(ctx, t.Org, t.Project, id, in.DestroyServer); err != nil {
				return Message{}, err
			}
			return msgf("Node %s removed.", in.Node)
		})
}
