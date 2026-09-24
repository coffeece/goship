package portal

import (
	"cmp"
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
	// Default is the plan the placement's pool applies when none is named;
	// Billed is false on a customer's own node, where nothing is charged.
	Default bool `json:"default"`
	Billed  bool `json:"billed"`
}

// AvailablePlans lists what this organization may actually select for a
// placement. Without a node it is the shared cluster: the public catalogue,
// plus a free plan only for an org holding a grant. With one it is the
// node's own pool, read from the platform, in the order the platform applies.
func (c *Client) AvailablePlans(ctx context.Context, org, kind, node string) ([]Plan, error) {
	path := orgPath(org, "/available-plans")
	if node != "" {
		path += "?node=" + url.QueryEscape(node)
	}
	var plans []Plan
	if err := c.get(ctx, path, &plans); err != nil {
		return nil, err
	}
	plans = slices.DeleteFunc(plans, func(p Plan) bool {
		return !p.IsActive || (kind != "" && p.Kind != kind)
	})
	// The pool's order is meaningful (first = default); only the shared
	// catalogue carries a sort order worth applying.
	if node == "" {
		slices.SortStableFunc(plans, func(a, b Plan) int { return cmp.Compare(a.SortOrder, b.SortOrder) })
	}
	return plans, nil
}
