package portal

import "context"

// ClusterRef names the cluster something runs on. Shared is true for GoShip's
// own cluster, which every organization may use.
type ClusterRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Shared bool   `json:"shared"`
}

// Cluster is a machine or Kubernetes cluster an environment can run on. For a
// shared cluster owned by another organization only ID, Name, Type, Status,
// Shared and Namespaced are filled in.
type Cluster struct {
	ID     string `json:"id" table:"ID"`
	Name   string `json:"name" table:"NAME"`
	Type   string `json:"type" table:"TYPE"`
	Status string `json:"status" table:"STATUS"`
	Host   string `json:"host" table:"HOST"`
	Port   int    `json:"port"`
	// Shared marks GoShip's own cluster; Namespaced that apps from different
	// projects are kept apart by namespace rather than by machine.
	Shared        bool   `json:"shared" table:"SHARED"`
	Namespaced    bool   `json:"namespaced"`
	ClusterName   string `json:"cluster_name"`
	ErrorMessage  string `json:"error_message,omitempty"`
	Stage         string `json:"stage,omitempty"`
	CloudProvider string `json:"cloud_provider,omitempty"`
	CloudServerID string `json:"cloud_server_id,omitempty"`
	CloudRegion   string `json:"cloud_region,omitempty"`
	CloudSize     string `json:"cloud_size,omitempty"`
	ResizeFrom    string `json:"resize_from,omitempty"`
	ResizeTarget  string `json:"resize_target,omitempty"`
	ResizeMessage string `json:"resize_message,omitempty"`
	CloudAccount  *struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Label    string `json:"label"`
	} `json:"cloud_account,omitempty"`
}

type CreateClusterRequest struct {
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

func (c *Client) Clusters(ctx context.Context, org string) ([]Cluster, error) {
	var clusters []Cluster
	return clusters, c.get(ctx, clustersPath(org, ""), &clusters)
}

func (c *Client) Cluster(ctx context.Context, org, id string) (*Cluster, error) {
	var cl Cluster
	return &cl, c.get(ctx, clustersPath(org, "/"+esc(id)), &cl)
}

func (c *Client) CreateCluster(ctx context.Context, org string, req CreateClusterRequest) (*Cluster, error) {
	var cl Cluster
	return &cl, c.post(ctx, clustersPath(org, ""), req, &cl)
}

func (c *Client) DeleteCluster(ctx context.Context, org, id string, destroyServer bool) error {
	return c.delete(ctx, clustersPath(org, "/"+esc(id)), map[string]any{"destroy_server": destroyServer})
}

// ResizeOption is a size a cluster can be resized to; Current marks the one it runs on.
type ResizeOption struct {
	CloudSize
	Current bool `json:"current"`
}

func (c *Client) ClusterSizes(ctx context.Context, org, id string) ([]ResizeOption, error) {
	var sizes []ResizeOption
	return sizes, c.get(ctx, clustersPath(org, "/"+esc(id)+"/sizes"), &sizes)
}

func (c *Client) ResizeCluster(ctx context.Context, org, id, size string) (*Cluster, error) {
	var cl Cluster
	return &cl, c.post(ctx, clustersPath(org, "/"+esc(id)+"/resize"), map[string]string{"size": size}, &cl)
}
