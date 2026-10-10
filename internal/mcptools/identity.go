package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
)

type ProjectList struct {
	Projects []portal.Project `json:"projects"`
}

type OrgList struct {
	Orgs []portal.Org `json:"orgs"`
}

func (r *registry) identity() {
	orgless(r, "goship_whoami", "Show the authenticated GoShip user and the organizations they belong to.", read,
		func(ctx context.Context, c *portal.Client, _ struct{}) (*portal.Me, error) {
			return c.WhoAmI(ctx)
		})
	orgless(r, "goship_list_orgs", "List the organizations the user belongs to. Pass one's slug as org to other tools.", read,
		func(ctx context.Context, c *portal.Client, _ struct{}) (OrgList, error) {
			orgs, err := c.Orgs(ctx)
			return OrgList{Orgs: orgs}, err
		})
	orgTool(r, "goship_list_projects", "List the projects of an organization that the user can reach, with their role in each. Pass one's slug as project to other tools.", read,
		func(ctx context.Context, c *portal.Client, org string, _ OrgArg) (ProjectList, error) {
			projects, err := c.Projects(ctx, org)
			return ProjectList{Projects: projects}, err
		})
}
