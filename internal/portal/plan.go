package portal

import (
	"context"
	"net/url"
	"slices"
)

type Plan struct {
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	CPUMilli    int    `json:"cpu_milli,omitempty"`
	MemoryMB    int    `json:"memory_mb,omitempty"`
	PriceCents  int    `json:"price_cents"`
	IsFree      bool   `json:"is_free"`
	IsActive    bool   `json:"is_active"`
	SortOrder   int    `json:"sort_order"`
	// Default is the plan the environment's cluster applies when none is named;
	// Billed is false on a customer's own cluster, where nothing is charged.
	Default bool `json:"default"`
	Billed  bool `json:"billed"`
}

// AvailablePlans lists what this organization may actually select in an
// environment, for an app (kind "app", also the default) or a database (kind
// "database"). The environment's cluster decides: on the shared cluster it is
// the public catalogue, plus a free plan only for an org holding a grant; on
// the organization's own cluster it is the cluster's pool, in the order the
// platform applies. The server filters by kind and orders the list, so the
// client only drops what is inactive.
func (c *Client) AvailablePlans(ctx context.Context, org, project, env, kind string) ([]Plan, error) {
	path := envPath(org, project, env, "/available-plans")
	if kind != "" {
		path += "?" + url.Values{"kind": {kind}}.Encode()
	}
	var plans []Plan
	if err := c.get(ctx, path, &plans); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(plans, func(p Plan) bool { return !p.IsActive }), nil
}
