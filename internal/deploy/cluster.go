package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// ResolveCluster turns what a person types into the id of a cluster org can
// use: its own or a shared one. An id always wins; a name must be unique
// among them, and two clusters of that name are refused, naming both.
func ResolveCluster(ctx context.Context, client *portal.Client, org, ref string) (string, error) {
	clusters, err := client.Clusters(ctx, org)
	if err != nil {
		return "", err
	}
	var named []portal.Cluster
	for _, c := range clusters {
		if c.ID == ref {
			return c.ID, nil
		}
		if c.Name == ref {
			named = append(named, c)
		}
	}
	switch len(named) {
	case 1:
		return named[0].ID, nil
	case 0:
		return "", fmt.Errorf("no cluster %q in org %q; `goship clusters` (goship_list_clusters) lists them", ref, org)
	}
	ids := make([]string, len(named))
	for i, c := range named {
		ids[i] = c.ID
		if c.Shared {
			ids[i] += " (shared)"
		}
	}
	return "", fmt.Errorf("more than one cluster named %q in org %q: %s; pass the id", ref, org, strings.Join(ids, ", "))
}
