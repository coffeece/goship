package cli

import (
	"github.com/coffeece/goship/internal/deploy"
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
				id, err := deploy.ResolveNode(ctx, client, org, node)
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
		rows[i] = planRow{Slug: p.Slug, Name: p.DisplayName, CPU: p.CPUMilli, Memory: p.MemoryMB, Price: deploy.Price(p)}
		if p.Default {
			rows[i].Mark = "default"
		}
	}
	return rows
}
