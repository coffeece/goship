package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
)

type PlansArgs struct {
	OrgArg
	Kind string `json:"kind,omitempty" jsonschema:"app (default), database or volume"`
	Node string `json:"node,omitempty" jsonschema:"list the sizes available on this node of your own instead of the shared cluster"`
}

type PlanList struct {
	Plans []portal.Plan `json:"plans"`
}

func (r *registry) plans() {
	tool(r, "goship_list_plans", "List the plans the organization can choose, with CPU, memory, price and whether each is billed. A plan with billed=false is free to pick.", read,
		func(ctx context.Context, c *portal.Client, org string, in PlansArgs) (PlanList, error) {
			kind := in.Kind
			if kind == "" {
				kind = "app"
			}
			p, err := c.AvailablePlans(ctx, org, kind, in.Node)
			return PlanList{Plans: p}, err
		})
}
