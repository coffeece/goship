package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// Price is a plan's monthly price as the catalogue shows it.
func Price(p portal.Plan) string {
	switch {
	case p.IsFree:
		return "free"
	case !p.Billed:
		return "included"
	default:
		return fmt.Sprintf("R$ %d.%02d/mo", p.PriceCents/100, p.PriceCents%100)
	}
}

// ChoosePlan picks the plan for an app being created on the shared cluster. A
// free plan, where the organization has one, is the only thing safe to select
// on the user's behalf; otherwise the choice costs money and is theirs.
func ChoosePlan(ctx context.Context, client *portal.Client, org string) (portal.Plan, error) {
	plans, err := client.AvailablePlans(ctx, org, "app", "")
	if err != nil {
		return portal.Plan{}, err
	}
	for _, p := range plans {
		if !p.Billed {
			return p, nil
		}
	}
	return portal.Plan{}, fmt.Errorf("a plan is required to create an app:\n%s\nPass --plan <slug>, or set plan: in goship.yml", PlanTable(plans))
}

// PlanTable lists plans one per line, for an error that asks the user to pick.
func PlanTable(plans []portal.Plan) string {
	if len(plans) == 0 {
		return "  (none available — add a payment method at https://goship.sh/billing)"
	}
	var b strings.Builder
	for _, p := range plans {
		fmt.Fprintf(&b, "  %-22s %4dm CPU  %5d MB  %s\n", p.Slug, p.CPUMilli, p.MemoryMB, Price(p))
	}
	return strings.TrimRight(b.String(), "\n")
}
