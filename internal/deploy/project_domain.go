package deploy

import (
	"context"
	"fmt"

	"github.com/coffeece/goship/internal/portal"
)

// ResolveProjectDomain turns what a person types, the domain name or its id,
// into the project domain, which carries the id the API wants.
func ResolveProjectDomain(ctx context.Context, client *portal.Client, t Target, ref string) (*portal.ProjectDomain, error) {
	domains, err := client.ProjectDomains(ctx, t.Org, t.Project)
	if err != nil {
		return nil, err
	}
	for i := range domains {
		if domains[i].ID == ref || domains[i].Domain == ref {
			return &domains[i], nil
		}
	}
	return nil, fmt.Errorf("no domain %q attached to project %q", ref, t.Project)
}
