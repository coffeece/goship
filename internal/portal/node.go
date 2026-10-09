package portal

import "context"

type Node struct {
	ID            string `json:"id" table:"ID"`
	Name          string `json:"name" table:"NAME"`
	Type          string `json:"type" table:"TYPE"`
	Status        string `json:"status" table:"STATUS"`
	Host          string `json:"host" table:"HOST"`
	Port          int    `json:"port"`
	PoolName      string `json:"pool_name" table:"POOL"`
	ClusterName   string `json:"cluster_name"`
	ErrorMessage  string `json:"error_message,omitempty"`
	Stage         string `json:"stage,omitempty"`
	CloudProvider string `json:"cloud_provider,omitempty"`
	CloudServerID string `json:"cloud_server_id,omitempty"`
	CloudRegion   string `json:"cloud_region,omitempty"`
	CloudSize     string `json:"cloud_size,omitempty"`
	CloudAccount  *struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Label    string `json:"label"`
	} `json:"cloud_account,omitempty"`
}

type CreateNodeRequest struct {
	Name           string `json:"name"`
	Type           string `json:"type,omitempty"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
	SSHUser        string `json:"ssh_user,omitempty"`
	SSHKey         string `json:"ssh_key,omitempty"`
	CloudAccountID string `json:"cloud_account_id,omitempty"`
	Region         string `json:"region,omitempty"`
	Size           string `json:"size,omitempty"`
}

func (c *Client) Nodes(ctx context.Context, org string) ([]Node, error) {
	var nodes []Node
	return nodes, c.get(ctx, nodesPath(org), &nodes)
}

func (c *Client) Node(ctx context.Context, org, id string) (*Node, error) {
	var n Node
	return &n, c.get(ctx, nodesPath(org)+"/"+esc(id), &n)
}

func (c *Client) CreateNode(ctx context.Context, org string, req CreateNodeRequest) (*Node, error) {
	var n Node
	return &n, c.post(ctx, nodesPath(org), req, &n)
}

func (c *Client) DeleteNode(ctx context.Context, org, id string, destroyServer bool) error {
	return c.delete(ctx, nodesPath(org)+"/"+esc(id), map[string]any{"destroy_server": destroyServer})
}

// SharedPlacement is the node_id that asks for GoShip's servers explicitly,
// overriding the organization's default placement.
const SharedPlacement = "shared"

// Placement is where an organization's new apps go when the create request
// names no node. Target is nil when that is GoShip's servers.
type Placement struct {
	Mode   string           `json:"mode"`
	NodeID string           `json:"node_id,omitempty"`
	Target *PlacementTarget `json:"target"`
}

type PlacementTarget struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}

func (c *Client) Placement(ctx context.Context, org string) (*Placement, error) {
	var p Placement
	return &p, c.get(ctx, orgPath(org, "/placement"), &p)
}

func nodesPath(org string) string { return orgPath(org, "/nodes") }
