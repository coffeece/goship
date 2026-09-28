package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// newOrgListCmd is the plural lister every org-scoped noun shares: `apps`,
// `dbs`, `domains`, `volumes`, `nodes`. They are all org-scoped, so the
// listing shows the current org; --all spans every org you belong to, grouped
// under each slug — as does having no org selected, since demanding
// `org use` first for a read is friction.
func newOrgListCmd[T any](app *App, use, short string, fetch func(*portal.Client, context.Context, string) ([]T, error), opts ...orgListOption) *cobra.Command {
	skipForbidden := slices.Contains(opts, skipForbiddenOrgs)
	var all bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, client := cmd.Context(), app.Portal()
			if !all {
				if org, err := app.Org(ctx); err == nil {
					items, err := fetch(client, ctx, org)
					if err != nil {
						return err
					}
					return app.Renderer().Render(items)
				}
			}
			orgs, err := client.Orgs(ctx)
			if err != nil {
				return err
			}
			sections := make([]section[T], 0, len(orgs))
			for _, o := range orgs {
				items, err := fetch(client, ctx, o.Slug)
				if skipForbidden && portal.IsForbidden(err) {
					fmt.Fprintf(cmd.ErrOrStderr(), "Skipping org %s: your role there cannot see this.\n", o.Slug)
					continue
				}
				if err != nil {
					return err
				}
				sections = append(sections, section[T]{key: o.Slug, title: o.Slug, items: items})
			}
			return renderSections(app, sections, func(items []T) any { return items })
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "A", false, "list across every organization you belong to")
	return cmd
}

// orgListOption tunes newOrgListCmd for a noun that needs it.
type orgListOption int

// skipForbiddenOrgs leaves an org that answers 403 out of an --all listing,
// with a note on stderr, instead of failing the whole listing on it.
const skipForbiddenOrgs orgListOption = iota + 1

// section is one group of a listing that spans several places: an org, or a
// placement. key names it in JSON, title above its table.
type section[T any] struct {
	key, title string
	items      []T
}

// renderSections writes the sections as one JSON object keyed by section, or
// as a titled table each. view turns a section's items into what is rendered.
func renderSections[T any](app *App, sections []section[T], view func([]T) any) error {
	r := app.Renderer()
	if app.Global.Output == render.JSON {
		out := make(map[string]any, len(sections))
		for _, s := range sections {
			out[s.key] = view(s.items)
		}
		return r.Render(out)
	}
	for i, s := range sections {
		lead := "%s"
		if i > 0 {
			lead = "\n%s"
		}
		if err := r.Message(lead, s.title); err != nil {
			return err
		}
		if err := r.Render(view(s.items)); err != nil {
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
