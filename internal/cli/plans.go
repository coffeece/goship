package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

func newPlansCmd(app *App) *cobra.Command {
	var kind, node string

	cmd := &cobra.Command{
		Use:   "plans",
		Short: "List the plans your organization can choose, per placement",
		Long: "Plans on GoShip's shared cluster carry a price. On a node you own they\n" +
			"are sizes: nothing there is billed, and the node's pool decides which are\n" +
			"available and which applies by default.",
		Args: cobra.NoArgs,
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, _ []string) error {
			client, r := app.Portal(), app.Renderer()

			if node != "" {
				id, err := resolveNode(cmd.Context(), client, org, node)
				if err != nil {
					return err
				}
				plans, err := client.AvailablePlans(cmd.Context(), org, kind, id)
				if err != nil {
					return err
				}
				return r.Render(plans)
			}

			shared, err := client.AvailablePlans(cmd.Context(), org, kind, "")
			if err != nil {
				return err
			}
			nodes, err := client.Nodes(cmd.Context(), org)
			if err != nil {
				return err
			}

			if app.Global.Output == render.JSON {
				out := map[string]any{"shared": shared}
				for _, n := range nodes {
					plans, err := client.AvailablePlans(cmd.Context(), org, kind, n.ID)
					if err != nil {
						return err
					}
					out[n.Name] = plans
				}
				return r.Render(out)
			}

			if err := r.Message("GoShip (shared)"); err != nil {
				return err
			}
			if err := r.Render(shared); err != nil {
				return err
			}
			for _, n := range nodes {
				plans, err := client.AvailablePlans(cmd.Context(), org, kind, n.ID)
				if err != nil {
					return err
				}
				if err := r.Message("\n%s (your hardware — not billed)", n.Name); err != nil {
					return err
				}
				if err := r.Render(plans); err != nil {
					return err
				}
			}
			return nil
		}),
	}
	cmd.Flags().StringVar(&kind, "kind", "app", `plan kind: "app", "db", or "" for all`)
	cmd.Flags().StringVar(&node, "node", "", "show only what this node's pool admits (name or id)")

	return cmd
}

// choosePlan picks the plan for an app being created on the shared cluster. A
// free plan, where the organization has one, is the only thing safe to select
// unprompted — everything else costs money, so the user names it.
func choosePlan(ctx context.Context, client *portal.Client, org string) (portal.Plan, error) {
	plans, err := client.AvailablePlans(ctx, org, "app", "")
	if err != nil {
		return portal.Plan{}, err
	}
	for _, p := range plans {
		if !p.Billed {
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
