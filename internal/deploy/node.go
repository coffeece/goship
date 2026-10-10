package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
)

// ResolveNode turns what a person types — the node's name, usually — into
// the node's ID, which is what the API wants.
func ResolveNode(ctx context.Context, client *portal.Client, t Target, ref string) (string, error) {
	nodes, err := client.Nodes(ctx, t.Org, t.Project)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.ID == ref || n.Name == ref {
			return n.ID, nil
		}
		names = append(names, n.Name)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no node %q: %s has no nodes yet — `goship node create` makes one", ref, t)
	}
	return "", fmt.Errorf("no node %q in %s; you have: %s", ref, t, strings.Join(names, ", "))
}

// ResolvePlacement is ResolveNode for where an app runs: "goship" also names
// GoShip's servers, unless one of the organization's nodes is called that.
func ResolvePlacement(ctx context.Context, client *portal.Client, t Target, ref string) (string, error) {
	if ref != "goship" {
		return ResolveNode(ctx, client, t, ref)
	}
	nodes, err := client.Nodes(ctx, t.Org, t.Project)
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if n.Name == ref {
			return n.ID, nil
		}
	}
	return portal.GoShipPlacement, nil
}

// PlaceNewApp decides where a new app goes and how to say so. A node the
// caller named wins; otherwise the project's default placement does,
// pinned in the request so the app lands where the output says. A portal
// that cannot answer leaves the choice to the API. An empty nodeID or
// portal.GoShipPlacement means GoShip's servers, which need a plan.
func PlaceNewApp(ctx context.Context, client *portal.Client, t Target, node string) (nodeID, where string, err error) {
	if node != "" {
		id, err := ResolvePlacement(ctx, client, t, node)
		if err != nil {
			return "", "", err
		}
		if id == portal.GoShipPlacement {
			return id, "on GoShip's servers", nil
		}
		return id, "on your node " + node, nil
	}
	p, err := client.Placement(ctx, t.Org, t.Project)
	if err != nil || p.Target == nil {
		return "", "", nil
	}
	return p.Target.NodeID, "on your node " + p.Target.NodeName + " (project default)", nil
}

// OnNode reports whether a PlaceNewApp result is one of the organization's
// own nodes rather than GoShip's servers.
func OnNode(nodeID string) bool {
	return nodeID != "" && nodeID != portal.GoShipPlacement
}
