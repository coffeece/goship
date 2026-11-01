package cli

import (
	"fmt"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

func newPlansCmd(app *App) *cobra.Command {
	var kind string

	cmd := &cobra.Command{
		Use:   "plans",
		Short: "List the plans the current environment can choose",
		Long: "The plans come from the cluster the environment runs on. On GoShip's shared\n" +
			"cluster they carry a price. On a cluster of your own they are sizes: nothing\n" +
			"there is billed, and the cluster decides which are available and which\n" +
			"applies by default.",
		Args: cobra.NoArgs,
		RunE: readTargetRunE(app, func(cmd *cobra.Command, t Target, _ []string) error {
			k, err := planKind(kind)
			if err != nil {
				return err
			}
			plans, err := app.Portal().AvailablePlans(cmd.Context(), t.Org, t.Project, t.Environment, k)
			if err != nil {
				return err
			}
			return app.Renderer().Render(app.planView(plans))
		}),
	}
	cmd.Flags().StringVar(&kind, "kind", "app", `plan kind: "app" or "database"`)

	return cmd
}

// planKind is the portal's name for a --kind; "db" is what it was called
// before.
func planKind(kind string) (string, error) {
	switch kind {
	case "app", "database":
		return kind, nil
	case "db":
		return "database", nil
	}
	return "", fmt.Errorf(`unknown --kind %q: use "app" or "database"`, kind)
}

// planRow is a plan as the table shows it: priced, and marked when it is what
// the cluster applies by default.
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
