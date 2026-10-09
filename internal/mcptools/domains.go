package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
)

type DomainList struct {
	Domains []portal.OrgDomain `json:"domains"`
}

type DomainArg struct {
	OrgArg
	Domain string `json:"domain" jsonschema:"domain name, e.g. example.com"`
}

type DomainIDArg struct {
	OrgArg
	ID string `json:"id" jsonschema:"domain id from goship_list_domains"`
}

type AppDomainArgs struct {
	AppArg
	Domain string `json:"domain" jsonschema:"hostname to serve the app at; its registered domain must be verified"`
}

func (r *registry) domains() {
	tool(r, "goship_list_domains", "List the domains registered to an organization and whether each is verified.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (DomainList, error) {
			d, err := c.OrgDomains(ctx, org)
			return DomainList{Domains: d}, err
		})
	tool(r, "goship_register_domain", "Register a domain to the organization. Returns the TXT token to publish for verification.", write,
		func(ctx context.Context, c *portal.Client, org string, in DomainArg) (*portal.OrgDomain, error) {
			return c.RegisterOrgDomain(ctx, org, in.Domain)
		})
	tool(r, "goship_verify_domain", "Check a registered domain's TXT record and mark it verified.", write,
		func(ctx context.Context, c *portal.Client, org string, in DomainIDArg) (Message, error) {
			if err := c.VerifyOrgDomain(ctx, org, in.ID); err != nil {
				return Message{}, err
			}
			return msgf("Domain verified.")
		})
	tool(r, "goship_delete_domain", "Remove a registered domain from the organization.", destructive,
		func(ctx context.Context, c *portal.Client, org string, in DomainIDArg) (Message, error) {
			if err := c.DeleteOrgDomain(ctx, org, in.ID); err != nil {
				return Message{}, err
			}
			return msgf("Domain removed.")
		})
	tool(r, "goship_add_app_domain", "Serve an app at a hostname under a verified domain.", write,
		func(ctx context.Context, c *portal.Client, org string, in AppDomainArgs) (Message, error) {
			if err := c.AddAppDomain(ctx, org, in.App, in.Domain); err != nil {
				return Message{}, err
			}
			return msgf("%s now serves %s.", in.App, in.Domain)
		})
	tool(r, "goship_remove_app_domain", "Stop serving an app at a hostname.", write,
		func(ctx context.Context, c *portal.Client, org string, in AppDomainArgs) (Message, error) {
			if err := c.RemoveAppDomain(ctx, org, in.App, in.Domain); err != nil {
				return Message{}, err
			}
			return msgf("%s no longer serves %s.", in.App, in.Domain)
		})
}
