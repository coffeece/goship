package portal

import (
	"context"
	"time"
)

// Environment is a stage of an organization (production, staging, ...). Every
// project exists in every environment; the environment says which cluster new
// project environments land on and which database server new databases use.
type Environment struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug" table:"SLUG"`
	Name        string    `json:"name" table:"NAME"`
	ClusterID   string    `json:"cluster_id"`
	ClusterName string    `json:"cluster_name" table:"CLUSTER"`
	DBServerID  string    `json:"db_server_id"`
	IsDefault   bool      `json:"is_default" table:"DEFAULT"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectEnvironment is one project in one environment: the cluster it runs
// on there (Pinned when the project chose it, rather than inheriting the
// environment's) and what it holds.
type ProjectEnvironment struct {
	Environment Environment `json:"environment"`
	Cluster     ClusterRef  `json:"cluster"`
	Pinned      bool        `json:"pinned"`
	Apps        int         `json:"apps"`
	Databases   int         `json:"databases"`
	Volumes     int         `json:"volumes"`
}

// CreateEnvironmentRequest leaves ClusterID and DBServerID empty to use
// GoShip's shared cluster and PostgreSQL.
type CreateEnvironmentRequest struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	ClusterID  string `json:"cluster_id,omitempty"`
	DBServerID string `json:"db_server_id,omitempty"`
}

func (c *Client) Environments(ctx context.Context, org string) ([]Environment, error) {
	var envs []Environment
	return envs, c.get(ctx, orgPath(org, "/environments"), &envs)
}

func (c *Client) CreateEnvironment(ctx context.Context, org string, req CreateEnvironmentRequest) (*Environment, error) {
	var env Environment
	return &env, c.post(ctx, orgPath(org, "/environments"), req, &env)
}

func (c *Client) DeleteEnvironment(ctx context.Context, org, slug string) error {
	return c.delete(ctx, orgPath(org, "/environments/"+esc(slug)), nil)
}

// ProjectEnvironments lists the project's environments with the cluster each
// one runs on and what it holds.
func (c *Client) ProjectEnvironments(ctx context.Context, org, project string) ([]ProjectEnvironment, error) {
	var out []ProjectEnvironment
	return out, c.get(ctx, projectPath(org, project, "/environments"), &out)
}
