package portal

import (
	"context"
	"time"
)

type DatabaseServer struct {
	ID                 string     `json:"id" table:"ID"`
	Name               string     `json:"name" table:"NAME"`
	Provider           string     `json:"provider" table:"PROVIDER"`
	Region             string     `json:"region" table:"REGION"`
	Tier               string     `json:"tier" table:"SIZE"`
	Status             string     `json:"status" table:"STATUS"`
	Stage              string     `json:"stage,omitempty"`
	Host               string     `json:"host,omitempty" table:"HOST"`
	DatabaseCount      int        `json:"database_count" table:"DATABASES"`
	CloudInstanceName  string     `json:"cloud_instance_name,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	Scope              string     `json:"scope"`
	CloudAccountID     string     `json:"cloud_account_id"`
	CreatedAt          time.Time  `json:"created_at"`
	BrokerRegisteredAt *time.Time `json:"broker_registered_at,omitempty"`
}

type DatabaseServerTier struct {
	Slug    string `json:"slug" table:"SLUG"`
	Label   string `json:"label" table:"SIZE"`
	Price   string `json:"price" table:"GOOGLE PRICE"`
	Default bool   `json:"default" table:"DEFAULT"`
}

type CreateDatabaseServerRequest struct {
	CloudAccountID string `json:"cloud_account_id"`
	Name           string `json:"name"`
	Region         string `json:"region"`
	Tier           string `json:"tier"`
}

func databaseServersPath(org, project string) string {
	if project == "" {
		return orgPath(org, "/database-servers")
	}
	return projectPath(org, project, "/database-servers")
}

func (c *Client) DatabaseServers(ctx context.Context, org, project string) ([]DatabaseServer, error) {
	var out []DatabaseServer
	return out, c.get(ctx, databaseServersPath(org, project), &out)
}

func (c *Client) CreateDatabaseServer(ctx context.Context, org, project string, req CreateDatabaseServerRequest) (*DatabaseServer, error) {
	var out DatabaseServer
	return &out, c.post(ctx, databaseServersPath(org, project), req, &out)
}

func (c *Client) RetryDatabaseServer(ctx context.Context, org, project, id string) error {
	return c.post(ctx, databaseServersPath(org, project)+"/"+esc(id)+"/retry", nil, nil)
}

func (c *Client) DeleteDatabaseServer(ctx context.Context, org, project, id string) error {
	return c.delete(ctx, databaseServersPath(org, project)+"/"+esc(id), nil)
}

func (c *Client) DatabaseServerTiers(ctx context.Context, org, project string) ([]DatabaseServerTier, error) {
	var out []DatabaseServerTier
	path := orgPath(org, "/database-server-tiers")
	if project != "" {
		path = projectPath(org, project, "/database-server-tiers")
	}
	return out, c.get(ctx, path, &out)
}
