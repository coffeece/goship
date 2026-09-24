package cli

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
)

// newOrgListCmd is the plural lister every org-scoped noun shares: `apps`,
// `dbs`, `domains`, `volumes`, `nodes`. They are all org-scoped, so the
// listing shows the current org; --all spans every org you belong to, grouped
// under each slug — as does having no org selected, since demanding
// `org use` first for a read is friction.
func newOrgListCmd[T any](app *App, use, short string, fetch func(*portal.Client, context.Context, string) ([]T, error)) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := app.Portal()
			if !all {
				if org, err := app.Org(cmd.Context()); err == nil {
					items, err := fetch(client, cmd.Context(), org)
					if err != nil {
						return err
					}
					return app.Renderer().Render(items)
				}
			}
			orgs, err := client.Orgs(cmd.Context())
			if err != nil {
				return err
			}
			return renderAcrossOrgs(cmd, app, client, orgs, fetch)
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "A", false, "list across every organization you belong to")
	return cmd
}

func renderAcrossOrgs[T any](cmd *cobra.Command, app *App, client *portal.Client, orgs []portal.Org, fetch func(*portal.Client, context.Context, string) ([]T, error)) error {
	r := app.Renderer()

	if app.Global.Output == "json" {
		out := map[string][]T{}
		for _, o := range orgs {
			items, err := fetch(client, cmd.Context(), o.Slug)
			if err != nil {
				return err
			}
			out[o.Slug] = items
		}
		return r.Render(out)
	}

	for i, o := range orgs {
		items, err := fetch(client, cmd.Context(), o.Slug)
		if err != nil {
			return err
		}
		lead := "%s"
		if i > 0 {
			lead = "\n%s"
		}
		if err := r.Message(lead, o.Slug); err != nil {
			return err
		}
		if err := r.Render(items); err != nil {
			return err
		}
	}
	return nil
}

// deprecatedList keeps `<noun> list` working for scripts written before the
// plural lister existed, hidden from help and pointing at its replacement.
func deprecatedList(plural *cobra.Command) *cobra.Command {
	replacement := plural.Name()
	plural.Use = "list"
	plural.Deprecated = "use `goship " + replacement + "`"
	return plural
}
