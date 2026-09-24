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
			ctx, client, r := cmd.Context(), app.Portal(), app.Renderer()

			if node != "" {
				id, err := resolveNode(ctx, client, org, node)
				if err != nil {
					return err
				}
				plans, err := client.AvailablePlans(ctx, org, kind, id)
				if err != nil {
					return err
				}
				return r.Render(app.planView(plans))
			}

			shared, err := client.AvailablePlans(ctx, org, kind, "")
			if err != nil {
				return err
			}
			nodes, err := client.Nodes(ctx, org)
			if err != nil {
				return err
			}
			sections := []section[portal.Plan]{{key: "shared", title: "GoShip (shared)", items: shared}}
			for _, n := range nodes {
				plans, err := client.AvailablePlans(ctx, org, kind, n.ID)
				if err != nil {
					return err
				}
				sections = append(sections, section[portal.Plan]{key: n.Name, title: n.Name + " (your hardware — not billed)", items: plans})
			}
			return renderSections(app, sections, app.planView)
		}),
	}
	cmd.Flags().StringVar(&kind, "kind", "app", `plan kind: "app", "db", or "" for all`)
	cmd.Flags().StringVar(&node, "node", "", "show only what this node's pool admits (name or id)")

	return cmd
}

// planRow is a plan as the table shows it: priced, and marked when it is what
// the placement applies by default.
type planRow struct {
	Slug   string `table:"SLUG"`
	Name   string `table:"NAME"`
	CPU    int    `table:"CPU"`
	Memory int    `table:"MEMORY"`
	Price  string `table:"PRICE"`
	Mark   string `table:" "`
}

// planView is what to render for plans: the plans themselves as JSON, rows
// for a table.
func (a *App) planView(plans []portal.Plan) any {
	if a.Global.Output == render.JSON {
		return plans
	}
	rows := make([]planRow, len(plans))
	for i, p := range plans {
		rows[i] = planRow{Slug: p.Slug, Name: p.DisplayName, CPU: p.CPUMilli, Memory: p.MemoryMB, Price: price(p)}
		if p.Default {
			rows[i].Mark = "default"
		}
	}
	return rows
}

// price renders the monthly price the way an invoice would.
func price(p portal.Plan) string {
	switch {
	case p.IsFree:
		return "free"
	case !p.Billed:
		return "included"
	default:
		return fmt.Sprintf("R$ %d.%02d/mo", p.PriceCents/100, p.PriceCents%100)
	}
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
	return portal.Plan{}, fmt.Errorf("a plan is required to create an app:\n%s\nPass --plan <slug>, or set plan: in goship.yml", planTable(plans))
}

func planTable(plans []portal.Plan) string {
	if len(plans) == 0 {
		return "  (none available — add a payment method at https://goship.sh/billing)"
	}
	var b strings.Builder
	for _, p := range plans {
		fmt.Fprintf(&b, "  %-22s %4dm CPU  %5d MB  %s\n", p.Slug, p.CPUMilli, p.MemoryMB, price(p))
	}
	return strings.TrimRight(b.String(), "\n")
}
