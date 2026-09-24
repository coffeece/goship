package portal

import (
	"context"
	"time"
)

type OrgDomain struct {
	ID                string     `json:"id" table:"ID"`
	Domain            string     `json:"domain" table:"DOMAIN"`
	VerificationToken string     `json:"verification_token,omitempty" table:"TXT TOKEN"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty" table:"VERIFIED"`
	IsDefault         bool       `json:"is_default" table:"DEFAULT"`
	CreatedAt         time.Time  `json:"created_at"`
}

func (c *Client) OrgDomains(ctx context.Context, org string) ([]OrgDomain, error) {
	var domains []OrgDomain
	return domains, c.get(ctx, orgDomainsPath(org), &domains)
}

func (c *Client) RegisterOrgDomain(ctx context.Context, org, domain string) (*OrgDomain, error) {
	var created OrgDomain
	return &created, c.post(ctx, orgDomainsPath(org), map[string]string{"domain": domain}, &created)
}

func (c *Client) VerifyOrgDomain(ctx context.Context, org, id string) error {
	return c.post(ctx, orgDomainsPath(org)+"/"+esc(id)+"/verify", nil, nil)
}

func (c *Client) DeleteOrgDomain(ctx context.Context, org, id string) error {
	return c.delete(ctx, orgDomainsPath(org)+"/"+esc(id), nil)
}

func (c *Client) AddAppDomain(ctx context.Context, org, app, domain string) error {
	return c.post(ctx, appPath(org, app)+"/domains", map[string]string{"domain": domain}, nil)
}

func (c *Client) RemoveAppDomain(ctx context.Context, org, app, domain string) error {
	return c.delete(ctx, appPath(org, app)+"/domains/"+esc(domain), nil)
}

func orgDomainsPath(org string) string { return orgPath(org, "/domains") }
