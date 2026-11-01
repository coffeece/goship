package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type PlansArgs struct {
	ProjectArg
	Kind string `json:"kind,omitempty" jsonschema:"app (default), database or volume"`
}

type PlanList struct {
	Plans []portal.Plan `json:"plans"`
}

func (r *registry) plans() {
	tool(r, "goship_list_plans", "List the plans the project can choose, with CPU, memory, price and whether each is billed. A plan with billed=false is free to pick.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in PlansArgs) (PlanList, error) {
			kind := in.Kind
			if kind == "" {
				kind = "app"
			}
			p, err := c.AvailablePlans(ctx, t.Org, t.Project, t.Environment, kind)
			return PlanList{Plans: p}, err
		})
}
