package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// appInOtherOrgs looks for an app of the same name in the caller's other
// organizations. Deploy creates whatever it cannot find, so without this a
// wrong --org silently produces a second app of the same name somewhere else
// instead of deploying the one the user meant.
//
// Best effort: a failure here must not block a legitimate create, so errors
// return no matches rather than propagating.
func appInOtherOrgs(ctx context.Context, client *portal.Client, current, name string) []string {
	orgs, err := client.Orgs(ctx)
	if err != nil || len(orgs) < 2 {
		return nil
	}

	var found []string
	for _, o := range orgs {
		if o.Slug == current {
			continue
		}
		if _, err := client.App(ctx, o.Slug, name); err == nil {
			found = append(found, o.Slug)
		}
	}
	return found
}

func elsewhereError(name, current string, orgs []string) error {
	if len(orgs) == 1 {
		return fmt.Errorf(
			"no app %q in org %q, but one exists in org %q.\n"+
				"Deploy that one with --org %s, or pass --app <name> to create a different app here",
			name, current, orgs[0], orgs[0])
	}
	return fmt.Errorf(
		"no app %q in org %q, but apps with that name exist in: %s.\n"+
			"Pick one with --org, or pass --app <name> to create a different app here",
		name, current, strings.Join(orgs, ", "))
}
