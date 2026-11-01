package deploy

import (
	"context"
	"fmt"

	"github.com/coffeece/goship/internal/portal"
)

// ResolveDatabaseServer turns what a person types — the server's name,
// usually — into the server, which carries the id the API wants. Servers
// belong to the organization. An id always wins; a name must be unique.
func ResolveDatabaseServer(ctx context.Context, client *portal.Client, t Target, ref string) (*portal.DatabaseServer, error) {
	servers, err := client.DatabaseServers(ctx, t.Org)
	if err != nil {
		return nil, err
	}
	for i := range servers {
		if servers[i].ID == ref {
			return &servers[i], nil
		}
	}
	var match *portal.DatabaseServer
	for i := range servers {
		if servers[i].Name != ref {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("more than one database server named %q in %s; pass the id", ref, scope(t))
		}
		match = &servers[i]
	}
	if match != nil {
		return match, nil
	}
	return nil, fmt.Errorf("no database server %q in %s; `goship db server list` (goship_list_database_servers) shows them", ref, scope(t))
}

func scope(t Target) string { return fmt.Sprintf("org %q", t.Org) }
