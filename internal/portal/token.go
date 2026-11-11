package portal

import (
	"context"
	"net/url"
	"time"
)

// APIToken is a long-lived credential for CI and scripts. Token carries the
// secret value and is only ever set on the response that created it.
type APIToken struct {
	ID         string     `json:"id" table:"ID"`
	Name       string     `json:"name" table:"NAME"`
	Prefix     string     `json:"prefix" table:"PREFIX"`
	CreatedAt  time.Time  `json:"created_at" table:"CREATED"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" table:"LAST USED"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" table:"EXPIRES"`
	Token      string     `json:"token,omitempty"`
}

func (c *Client) APITokens(ctx context.Context) ([]APIToken, error) {
	var out []APIToken
	return out, c.get(ctx, "/auth/tokens", &out)
}

// CreateAPIToken mints a token. expiresInDays 0 means it never expires.
func (c *Client) CreateAPIToken(ctx context.Context, name string, expiresInDays int) (*APIToken, error) {
	var out APIToken
	body := map[string]any{"name": name, "expires_in_days": expiresInDays}
	return &out, c.post(ctx, "/auth/tokens", body, &out)
}

func (c *Client) RevokeAPIToken(ctx context.Context, id string) error {
	return c.delete(ctx, "/auth/tokens/"+esc(id), nil)
}

// DeployToken is a token that belongs to an organization rather than a
// person: a pipeline's credential, which can only deploy and do what a
// pipeline needs, inside its scope. An empty Project or Environment means
// every project, or every environment, of the organization.
type DeployToken struct {
	ID          string     `json:"id" table:"ID"`
	Name        string     `json:"name" table:"NAME"`
	Project     string     `json:"project,omitempty" table:"PROJECT"`
	Environment string     `json:"environment,omitempty" table:"ENVIRONMENT"`
	Prefix      string     `json:"prefix" table:"PREFIX"`
	CreatedBy   string     `json:"created_by,omitempty" table:"CREATED BY"`
	CreatedAt   time.Time  `json:"created_at" table:"CREATED"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty" table:"LAST USED"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty" table:"EXPIRES"`
	Token       string     `json:"token,omitempty"`
}

// DeployTokens lists the organization's deploy tokens. A non-empty project
// narrows the list to the ones confined to it, which is all a project admin
// who is not an admin of the organization may read.
func (c *Client) DeployTokens(ctx context.Context, org, project string) ([]DeployToken, error) {
	path := orgPath(org, "/tokens")
	if project != "" {
		path += "?project=" + url.QueryEscape(project)
	}
	var out []DeployToken
	return out, c.get(ctx, path, &out)
}

// CreateDeployToken mints a deploy token. An empty project or environment
// does not confine it on that axis; expiresInDays 0 means it never expires.
func (c *Client) CreateDeployToken(ctx context.Context, org, name, project, environment string, expiresInDays int) (*DeployToken, error) {
	body := map[string]any{"name": name, "expires_in_days": expiresInDays}
	if project != "" {
		body["project"] = project
	}
	if environment != "" {
		body["environment"] = environment
	}
	var out DeployToken
	return &out, c.post(ctx, orgPath(org, "/tokens"), body, &out)
}

func (c *Client) RevokeDeployToken(ctx context.Context, org, id string) error {
	return c.delete(ctx, orgPath(org, "/tokens/"+esc(id)), nil)
}
