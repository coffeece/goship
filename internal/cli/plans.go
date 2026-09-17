package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
)

func newPlansCmd(app *App) *cobra.Command {
	var kind string

	cmd := &cobra.Command{
		Use:   "plans",
		Short: "List the plans your organization can choose",
		Args:  cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			plans, err := app.Portal().AvailablePlans(cmd.Context(), org, kind)
			if err != nil {
				return err
			}
			return app.Renderer().Render(plans)
		}),
	}
	cmd.Flags().StringVar(&kind, "kind", "app", `plan kind: "app", "db", or "" for all`)

	return cmd
}

// choosePlan picks the plan for an app being created. A free plan, where the
// organization has one, is the only thing safe to select unprompted —
// everything else costs money, so the user names it.
func choosePlan(ctx context.Context, client *portal.Client, org string) (portal.Plan, error) {
	plans, err := client.AvailablePlans(ctx, org, "app")
	if err != nil {
		return portal.Plan{}, err
	}
	for _, p := range plans {
		if p.IsFree {
			return p, nil
		}
	}
	return portal.Plan{}, fmt.Errorf("a plan is required to create an app:\n%s\nPass --plan <slug>, or set plan: in goship.yaml", planTable(plans))
}

func planTable(plans []portal.Plan) string {
	if len(plans) == 0 {
		return "  (none available — add a payment method at https://goship.sh/billing)"
	}
	var b strings.Builder
	for _, p := range plans {
		fmt.Fprintf(&b, "  %-22s %4dm CPU  %5d MB  %s\n", p.Slug, p.CPUMilli, p.MemoryMB, p.Price())
	}
	return strings.TrimRight(b.String(), "\n")
}
