package mcptools

import (
	"context"
	"fmt"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type PlansArgs struct {
	ProjectArg
	Kind string `json:"kind,omitempty" jsonschema:"app (default) or database"`
}

type PlanList struct {
	Plans []portal.Plan `json:"plans"`
}

func (r *registry) plans() {
	tool(r, "goship_list_plans", "List the plans an app or a database can have in the environment, with CPU, memory, price and whether each is billed. The environment's cluster decides: GoShip's shared cluster offers the public catalogue, the organization's own cluster its own plans. A plan with billed=false is free to pick; default=true marks the one used when none is named.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in PlansArgs) (PlanList, error) {
			kind := in.Kind
			switch kind {
			case "":
				kind = "app"
			case "app", "database":
			default:
				return PlanList{}, fmt.Errorf(`unknown kind %q: use "app" or "database"`, kind)
			}
			p, err := c.AvailablePlans(ctx, t.Org, t.Project, t.Environment, kind)
			return PlanList{Plans: p}, err
		})
}
