package mcptools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type ClusterList struct {
	Clusters []portal.Cluster `json:"clusters"`
}

type ClusterArg struct {
	OrgArg
	Cluster string `json:"cluster" jsonschema:"cluster name or id (goship_list_clusters)"`
}

type CreateClusterArgs struct {
	OrgArg
	Name         string `json:"name" jsonschema:"cluster name"`
	CloudAccount string `json:"cloud_account,omitempty" jsonschema:"provision a new machine on this connected cloud account: id, label or provider (goship_list_cloud_accounts); needs region and size"`
	Region       string `json:"region,omitempty" jsonschema:"cloud region slug (goship_list_cloud_regions)"`
	Size         string `json:"size,omitempty" jsonschema:"cloud machine size slug (goship_list_cloud_sizes)"`
	Host         string `json:"host,omitempty" jsonschema:"connect a machine the customer already runs, over SSH: its hostname or IP; needs ssh_key"`
	Port         int    `json:"port,omitempty" jsonschema:"SSH port, default 22"`
	SSHUser      string `json:"ssh_user,omitempty" jsonschema:"SSH user, default root"`
	SSHKey       string `json:"ssh_key,omitempty" jsonschema:"the private key itself (not a path) with access to the host"`
	Kubeconfig   string `json:"kubeconfig,omitempty" jsonschema:"connect a Kubernetes cluster the customer already runs: the kubeconfig itself (not a path)"`
}

type ClusterSizeList struct {
	Sizes []portal.ResizeOption `json:"sizes"`
}

type ResizeClusterArgs struct {
	ClusterArg
	Size string `json:"size" jsonschema:"machine size slug from goship_list_cluster_sizes"`
}

type DeleteClusterArgs struct {
	ClusterArg
	DestroyServer bool `json:"destroy_server,omitempty" jsonschema:"also destroy the machine at the cloud provider"`
}

func (r *registry) clusters() {
	orgTool(r, "goship_list_clusters", "List the clusters the organization can run environments on, with their status: its own (machines and Kubernetes clusters it created or connected) and GoShip's shared cluster (shared=true).", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (ClusterList, error) {
			cl, err := c.Clusters(ctx, org)
			return ClusterList{Clusters: cl}, err
		})
	orgTool(r, "goship_get_cluster", "Show one cluster, including its provisioning or resize stage and last error.", read,
		func(ctx context.Context, c *portal.Client, org string, in ClusterArg) (*portal.Cluster, error) {
			id, err := deploy.ResolveCluster(ctx, c, org, in.Cluster)
			if err != nil {
				return nil, err
			}
			return c.Cluster(ctx, org, id)
		})
	orgTool(r, "goship_create_cluster", "Create a cluster an environment can run on, in one of three ways: provision a new machine on a connected cloud account (cloud_account, region, size), connect a machine the customer already runs over SSH (host, ssh_key), or connect a Kubernetes cluster the customer already runs (kubeconfig). Pass exactly one. Provisioning continues in the background; goship_get_cluster shows its stage. Clusters are billed monthly. An environment runs on it once goship_create_environment names it.", write,
		func(ctx context.Context, c *portal.Client, org string, in CreateClusterArgs) (*portal.Cluster, error) {
			req, err := clusterRequest(in)
			if err != nil {
				return nil, err
			}
			if in.CloudAccount != "" {
				acc, err := resolveCloudAccount(ctx, c, org, in.CloudAccount)
				if err != nil {
					return nil, err
				}
				req.CloudAccountID = acc.ID
			}
			return c.CreateCluster(ctx, org, req)
		})
	orgTool(r, "goship_list_cluster_sizes", "List the machine sizes a cluster can be resized to, with prices; current=true marks the one it runs on.", read,
		func(ctx context.Context, c *portal.Client, org string, in ClusterArg) (ClusterSizeList, error) {
			id, err := deploy.ResolveCluster(ctx, c, org, in.Cluster)
			if err != nil {
				return ClusterSizeList{}, err
			}
			s, err := c.ClusterSizes(ctx, org, id)
			return ClusterSizeList{Sizes: s}, err
		})
	orgTool(r, "goship_resize_cluster", "Change a cluster's machine size in place. Every app on it is down for a few minutes while the machine restarts.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in ResizeClusterArgs) (Message, error) {
			id, err := deploy.ResolveCluster(ctx, c, org, in.Cluster)
			if err != nil {
				return Message{}, err
			}
			if _, err := c.ResizeCluster(ctx, org, id, in.Size); err != nil {
				return Message{}, deploy.ResizeReconnectHint(c.DashboardURL(), org, id, err)
			}
			return msgf("Cluster %s is resizing to %s. goship_get_cluster shows its stage.", in.Cluster, in.Size)
		})
	orgTool(r, "goship_delete_cluster", "Disconnect a cluster and every app on it; with destroy_server the machine at the cloud provider is destroyed too.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in DeleteClusterArgs) (Message, error) {
			id, err := deploy.ResolveCluster(ctx, c, org, in.Cluster)
			if err != nil {
				return Message{}, err
			}
			if err := c.DeleteCluster(ctx, org, id, in.DestroyServer); err != nil {
				return Message{}, err
			}
			// Destroying the machine runs on after the API answers.
			if in.DestroyServer {
				return msgf("Cluster %s is being removed and its machine destroyed; goship_list_clusters shows when it is gone.", in.Cluster)
			}
			return msgf("Cluster %s disconnected.", in.Cluster)
		})
}

// clusterRequest is the request for the one way in that in chose, as `goship
// cluster create` builds it. An argument of another way is refused rather
// than ignored. The cloud account is left for the caller to resolve.
func clusterRequest(in CreateClusterArgs) (portal.CreateClusterRequest, error) {
	req := portal.CreateClusterRequest{Name: in.Name}
	type arg struct {
		name string
		set  bool
	}
	ways := []struct {
		arg
		only []arg
	}{
		{arg{"cloud_account", in.CloudAccount != ""}, []arg{{"region", in.Region != ""}, {"size", in.Size != ""}}},
		{arg{"host", in.Host != ""}, []arg{{"port", in.Port != 0}, {"ssh_user", in.SSHUser != ""}, {"ssh_key", in.SSHKey != ""}}},
		{arg{"kubeconfig", in.Kubeconfig != ""}, nil},
	}
	var chosen []string
	for _, w := range ways {
		if w.set {
			chosen = append(chosen, w.name)
		}
	}
	if len(chosen) != 1 {
		return req, errors.New("pass exactly one of cloud_account, host or kubeconfig")
	}
	for _, w := range ways {
		for _, a := range w.only {
			if a.set && w.name != chosen[0] {
				return req, fmt.Errorf("%s only applies with %s", a.name, w.name)
			}
		}
	}

	switch chosen[0] {
	case "cloud_account":
		var missing []string
		if in.Region == "" {
			missing = append(missing, "region is required with cloud_account (goship_list_cloud_regions)")
		}
		if in.Size == "" {
			missing = append(missing, "size is required with cloud_account (goship_list_cloud_sizes)")
		}
		if len(missing) > 0 {
			return req, errors.New(strings.Join(missing, "; "))
		}
		req.Region, req.Size = in.Region, in.Size
	case "host":
		if in.SSHKey == "" {
			return req, errors.New("ssh_key is required with host")
		}
		req.Type, req.Host, req.SSHKey = "vps", in.Host, in.SSHKey
		req.Port, req.SSHUser = cmp.Or(in.Port, 22), cmp.Or(in.SSHUser, "root")
	case "kubeconfig":
		req.Type, req.Kubeconfig = "kubernetes", in.Kubeconfig
	}
	return req, nil
}
