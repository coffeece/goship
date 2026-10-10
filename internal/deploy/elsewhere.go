package deploy

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// FindApp lists every project the caller can reach, in org when given and
// otherwise in all their organizations, that holds an app called name.
func FindApp(ctx context.Context, client *portal.Client, org, name string) ([]Target, error) {
	var orgs []string
	if org != "" {
		orgs = []string{org}
	} else {
		list, err := client.Orgs(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range list {
			orgs = append(orgs, o.Slug)
		}
	}

	var found []Target
	for _, o := range orgs {
		projects, err := client.Projects(ctx, o)
		if err != nil {
			return nil, err
		}
		for _, p := range projects {
			_, err := client.App(ctx, o, p.Slug, name)
			switch {
			case err == nil:
				found = append(found, Target{Org: o, Project: p.Slug})
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
// somewhere else instead of deploying the one the user meant.
//
// Best effort: a failure here must not block a legitimate create, so errors
// return no matches rather than propagating.
func AppElsewhere(ctx context.Context, client *portal.Client, current Target, name string) []Target {
	found, err := FindApp(ctx, client, "", name)
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(found, func(t Target) bool { return t == current })
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
		return fmt.Errorf("%s, but %q exists in %s.\nDeploy that one with --org %s --project %s, or `goship org use %s` and `goship project use %s`",
			problem, name, found[0], found[0].Org, found[0].Project, found[0].Org, found[0].Project)
	}
	names := make([]string, len(found))
	for i, t := range found {
		names[i] = t.String()
	}
	return fmt.Errorf("%s, but %q exists in: %s.\nPick one with --org and --project, or `goship org use <slug>` and `goship project use <slug>`",
		problem, name, strings.Join(names, ", "))
}
