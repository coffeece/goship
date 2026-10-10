package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// newOrgListCmd is the plural lister for an org-scoped noun (`domains`). It
// lists the current org; --all spans every org you belong to, grouped under
// each slug — as does having no org selected, since demanding `org use`
// first for a read is friction.
func newOrgListCmd[T any](app *App, use, short string, fetch func(*portal.Client, context.Context, string) ([]T, error)) *cobra.Command {
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

// newTargetListCmd is the plural lister every project-scoped noun shares:
// `apps`, `dbs`, `volumes`, `nodes`. The listing shows the current project;
// --all spans every project you can reach in every org, grouped under
// org/project — as does having no project selected and several to choose
// from, since demanding `project use` first for a read is friction.
func newTargetListCmd[T any](app *App, use, short string, fetch func(*portal.Client, context.Context, string, string) ([]T, error), opts ...listOption) *cobra.Command {
	skipForbidden := slices.Contains(opts, skipForbiddenProjects)
	var all bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, client := cmd.Context(), app.Portal()
			// Nothing to choose from narrows the listing to the org that was
			// named or selected; with no org either, it spans them all.
			var org string
			if !all {
				t, err := app.Target(ctx)
				switch {
				case err == nil:
					items, err := fetch(client, ctx, t.Org, t.Project)
					if err != nil {
						return err
					}
					return app.Renderer().Render(items)
				case errors.Is(err, errNoProject):
					org = t.Org
				case !errors.Is(err, errNoOrg):
					return err
				}
			}
			var orgs []string
			if org != "" {
				orgs = []string{org}
			} else {
				list, err := client.Orgs(ctx)
				if err != nil {
					return err
				}
				for _, o := range list {
					orgs = append(orgs, o.Slug)
				}
			}
			var sections []section[T]
			for _, o := range orgs {
				projects, err := client.Projects(ctx, o)
				if err != nil {
					return err
				}
				for _, p := range projects {
					items, err := fetch(client, ctx, o, p.Slug)
					if skipForbidden && portal.IsForbidden(err) {
						fmt.Fprintf(cmd.ErrOrStderr(), "Skipping %s/%s: your role there cannot see this.\n", o, p.Slug)
						continue
					}
					if err != nil {
						return err
					}
					key := o + "/" + p.Slug
					sections = append(sections, section[T]{key: key, title: key, items: items})
				}
			}
			return renderSections(app, sections, func(items []T) any { return items })
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "A", false, "list across every project you can reach in every organization")
	return cmd
}

// listOption tunes newTargetListCmd for a noun that needs it.
type listOption int

// skipForbiddenProjects leaves a project that answers 403 out of an --all
// listing, with a note on stderr, instead of failing the whole listing on it.
const skipForbiddenProjects listOption = iota + 1

// section is one group of a listing that spans several places: an org, a
// project, or a placement. key names it in JSON, title above its table.
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
