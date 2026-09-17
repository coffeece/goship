package portal

import (
	"context"
	"fmt"
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

	// Monthly is the rendered price; the table shows it instead of raw cents.
	Monthly string `json:"-" table:"PRICE"`
}

// Price renders the monthly price the way an invoice would.
func (p Plan) Price() string {
	if p.IsFree || p.PriceCents == 0 {
		return "free"
	}
	return fmt.Sprintf("R$ %d.%02d/mo", p.PriceCents/100, p.PriceCents%100)
}

// AvailablePlans lists what this organization may actually select, which is
// not the public catalogue: a free plan appears only for an org holding a
// grant for it.
func (c *Client) AvailablePlans(ctx context.Context, org, kind string) ([]Plan, error) {
	var plans []Plan
	if err := c.get(ctx, "/orgs/"+esc(org)+"/available-plans", &plans); err != nil {
		return nil, err
	}
	out := plans[:0]
	for _, p := range plans {
		if p.IsActive && (kind == "" || p.Kind == kind) {
			p.Monthly = p.Price()
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
	return out, nil
}
