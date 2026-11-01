package deploy

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// FindApp lists every project the caller can reach, in org when given and
// otherwise in all their organizations (from orgs), that holds an app called
// name in env, or in the organization's default environment when env is
// empty. A project slug narrows the search to projects of that name: a
// project someone asked for is never swapped for another.
func FindApp(ctx context.Context, client *portal.Client, orgs OrgLister, org, project, env, name string) ([]Target, error) {
	var places []portal.Org
	if org != "" {
		in := ResolveEnvironment(ctx, Target{Org: org}, EnvironmentChoice{Explicit: env}, orgs)
		places = []portal.Org{{Slug: org, DefaultEnvironment: in}}
	} else {
		list, err := orgs(ctx)
		if err != nil {
			return nil, err
		}
		places = list
	}

	var found []Target
	for _, o := range places {
		in := cmp.Or(env, defaultEnvironment(o))
		projects, err := client.Projects(ctx, o.Slug)
		if err != nil {
			return nil, err
		}
		for _, p := range projects {
			if project != "" && p.Slug != project {
				continue
			}
			_, err := client.App(ctx, o.Slug, p.Slug, in, name)
			switch {
			case err == nil:
				found = append(found, Target{Org: o.Slug, Project: p.Slug, Environment: in})
			case portal.IsNotFound(err), portal.IsForbidden(err):
			default:
				return nil, err
			}
		}
	}
	return found, nil
}

// AppElsewhere looks for an app of the same name in the caller's other
// projects. Deploy creates whatever it cannot find, so without this a wrong
// --org or --project silently produces a second app of the same name
// somewhere else instead of deploying the one the user meant. The same name
// in another environment of the current project is that environment's app,
// which is what environments are for, so it does not count.
//
// Best effort: a failure here must not block a legitimate create, so errors
// return no matches rather than propagating.
func AppElsewhere(ctx context.Context, client *portal.Client, orgs OrgLister, current Target, name string) []Target {
	found, err := FindApp(ctx, client, orgs, "", "", "", name)
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(found, func(t Target) bool { return t.Org == current.Org && t.Project == current.Project })
}

// ElsewhereError says where the app was found instead. current has an empty
// Org when nothing was selected, and an empty Project when only the project
// is missing.
func ElsewhereError(name string, current Target, found []Target, forbidden bool) error {
	problem := fmt.Sprintf("no app %q in %s", name, current)
	switch {
	case current.Org == "":
		problem = "no organization selected"
	case current.Project == "":
		problem = "no project selected"
	case forbidden:
		problem = fmt.Sprintf("you cannot reach %s", current)
	}
	if len(found) == 1 {
		f := found[0]
		return fmt.Errorf("%s, but %q exists in %s.\nDeploy that one with %s, or `goship org use %s` and `goship project use %s`",
			problem, name, f, targetFlags(f), f.Org, f.Project)
	}
	names := make([]string, len(found))
	for i, t := range found {
		names[i] = t.String()
	}
	return fmt.Errorf("%s, but %q exists in: %s.\nPick one with --org, --project and --environment, or `goship org use <slug>` and `goship project use <slug>`",
		problem, name, strings.Join(names, ", "))
}

// targetFlags are the flags that point a command at t.
func targetFlags(t Target) string {
	flags := fmt.Sprintf("--org %s --project %s", t.Org, t.Project)
	if t.Environment != "" {
		flags += " --environment " + t.Environment
	}
	return flags
}
