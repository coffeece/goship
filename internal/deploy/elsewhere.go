package deploy

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// FindApp lists every place the caller can reach that holds an app called
// name: a project of org (of all their organizations, from orgs, when org is
// empty) in an environment. An env narrows the search to that environment; an
// empty one searches every environment of the organization. A project slug
// narrows it to projects of that name: a project or environment someone asked
// for is never swapped for another.
func FindApp(ctx context.Context, client *portal.Client, orgs OrgLister, org, project, env, name string) ([]Target, error) {
	var slugs []string
	if org != "" {
		slugs = []string{org}
	} else {
		list, err := orgs(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range list {
			slugs = append(slugs, o.Slug)
		}
	}

	var found []Target
	for _, o := range slugs {
		envs := []string{env}
		if env == "" {
			var err error
			if envs, err = EnvironmentSlugs(ctx, client, o); err != nil {
				return nil, err
			}
		}
		projects, err := client.Projects(ctx, o)
		if err != nil {
			return nil, err
		}
		for _, p := range projects {
			if project != "" && p.Slug != project {
				continue
			}
			for _, e := range envs {
				_, err := client.App(ctx, o, p.Slug, e, name)
				switch {
				case err == nil:
					found = append(found, Target{Org: o, Project: p.Slug, Environment: e})
				case portal.IsNotFound(err), portal.IsForbidden(err):
				default:
					return nil, err
				}
			}
		}
	}
	return found, nil
}

// EnvironmentSlugs are the slugs of org's environments, in the portal's order.
// Every project exists in every one of them.
func EnvironmentSlugs(ctx context.Context, client *portal.Client, org string) ([]string, error) {
	envs, err := client.Environments(ctx, org)
	if err != nil {
		return nil, err
	}
	slugs := make([]string, len(envs))
	for i, e := range envs {
		slugs[i] = e.Slug
	}
	return slugs, nil
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
		return fmt.Errorf("%s, but %q exists in %s.\nDeploy that one with %s, or %s",
			problem, name, f, targetFlags(f), useCommands(f))
	}
	names := make([]string, len(found))
	for i, t := range found {
		names[i] = t.String()
	}
	return fmt.Errorf("%s, but %q exists in: %s.\nPick one with --org, --project and --environment, or %s",
		problem, name, strings.Join(names, ", "), useCommands(Target{Org: "<slug>", Project: "<slug>", Environment: "<slug>"}))
}

// useCommands are the commands that select t for the commands that follow.
func useCommands(t Target) string {
	if t.Environment == "" {
		return fmt.Sprintf("`goship org use %s` and `goship project use %s`", t.Org, t.Project)
	}
	return fmt.Sprintf("`goship org use %s`, `goship project use %s` and `goship environment use %s`", t.Org, t.Project, t.Environment)
}

// targetFlags are the flags that point a command at t.
func targetFlags(t Target) string {
	flags := fmt.Sprintf("--org %s --project %s", t.Org, t.Project)
	if t.Environment != "" {
		flags += " --environment " + t.Environment
	}
	return flags
}
