package portal

import "context"

type Node struct {
	ID           string `json:"id" table:"ID"`
	Name         string `json:"name" table:"NAME"`
	Type         string `json:"type" table:"TYPE"`
	Status       string `json:"status" table:"STATUS"`
	Host         string `json:"host" table:"HOST"`
	Port         int    `json:"port"`
	PoolName     string `json:"pool_name" table:"POOL"`
	ClusterName  string `json:"cluster_name"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type CreateNodeRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
	SSHUser string `json:"ssh_user,omitempty"`
	SSHKey  string `json:"ssh_key,omitempty"`
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

func nodesPath(org string) string { return orgPath(org, "/nodes") }
