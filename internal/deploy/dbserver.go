package deploy

import (
	"context"
	"fmt"

	"github.com/coffeece/goship/internal/portal"
)

// GoShipDatabaseServer is the server a database is created on when the person
// asks for GoShip's own PostgreSQL by that word. The API resolves it, to
// whatever GoShip server the environment uses, so it is sent as it is and
// never looked up among the organization's own servers.
const GoShipDatabaseServer = "goship"

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

// DatabasePlacement says where a new database goes when the person named ref
// as its server: nil for GoShip's PostgreSQL, else the organization's server.
// "goship" is GoShip's by name. An empty ref is the environment's default
// server, which may be one of the organization's own; fromEnvironment
// reports that the answer came from there, in which case the API is sent no
// server and resolves the same default itself.
func DatabasePlacement(ctx context.Context, client *portal.Client, t Target, ref string) (server *portal.DatabaseServer, fromEnvironment bool, err error) {
	switch ref {
	case GoShipDatabaseServer:
		return nil, false, nil
	case "":
	default:
		s, err := ResolveDatabaseServer(ctx, client, t, ref)
		return s, false, err
	}
	envs, err := client.Environments(ctx, t.Org)
	if err != nil {
		return nil, true, fmt.Errorf("reading environment %s's database server: %w", t.Environment, err)
	}
	for _, e := range envs {
		if e.Slug != t.Environment {
			continue
		}
		if e.DBServerID == "" {
			return nil, true, nil
		}
		s, err := ResolveDatabaseServer(ctx, client, t, e.DBServerID)
		if err != nil {
			return nil, true, fmt.Errorf("environment %s's database server: %w", t.Environment, err)
		}
		return s, true, nil
	}
	return nil, true, fmt.Errorf("no environment %q in %s", t.Environment, scope(t))
}

func scope(t Target) string { return fmt.Sprintf("org %q", t.Org) }
