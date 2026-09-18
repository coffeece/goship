package portal

import (
	"context"
	"fmt"
	"net/url"
	"sort"
)

type Plan struct {
	Slug        string `json:"slug" table:"SLUG"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name" table:"NAME"`
	CPUMilli    int    `json:"cpu_milli,omitempty" table:"CPU"`
	MemoryMB    int    `json:"memory_mb,omitempty" table:"MEMORY"`
	PriceCents  int    `json:"price_cents"`
	IsFree      bool   `json:"is_free"`
	IsActive    bool   `json:"is_active"`
	SortOrder   int    `json:"sort_order"`
	// Default is the plan the placement's pool applies when none is named;
	// Billed is false on a customer's own node, where nothing is charged.
	Default bool `json:"default"`
	Billed  bool `json:"billed"`

	// Rendered columns; the table shows these instead of raw fields.
	Monthly string `json:"-" table:"PRICE"`
	Mark    string `json:"-" table:" "`
}

// Price renders the monthly price the way an invoice would.
func (p Plan) Price() string {
	switch {
	case p.IsFree:
		return "free"
	case !p.Billed:
		return "included"
	default:
		return fmt.Sprintf("R$ %d.%02d/mo", p.PriceCents/100, p.PriceCents%100)
	}
}

// AvailablePlans lists what this organization may actually select for a
// placement. Without a node it is the shared cluster: the public catalogue,
// plus a free plan only for an org holding a grant. With one it is the
// node's own pool, read from the platform, in the order the platform applies.
func (c *Client) AvailablePlans(ctx context.Context, org, kind, node string) ([]Plan, error) {
	path := "/orgs/" + esc(org) + "/available-plans"
	if node != "" {
		path += "?node=" + url.QueryEscape(node)
	}
	var plans []Plan
	if err := c.get(ctx, path, &plans); err != nil {
		return nil, err
	}
	out := plans[:0]
	for _, p := range plans {
		if !p.IsActive || (kind != "" && p.Kind != kind) {
			continue
		}
		p.Monthly = p.Price()
		if p.Default {
			p.Mark = "default"
		}
		out = append(out, p)
	}
	// The pool's order is meaningful (first = default); only the shared
	// catalogue carries a sort order worth applying.
	if node == "" {
		sort.SliceStable(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
	}
	return out, nil
}
