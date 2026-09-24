package portal

import (
	"context"
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
