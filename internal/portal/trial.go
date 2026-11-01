package portal

import "time"

// Trial is an invited trial: apps and databases on GoShip's cluster within
// its allowance need no card, and are billed from EndsAt. Only the owner of
// the organization sees the counts and limits; everyone else gets EndsAt.
type Trial struct {
	EndsAt         time.Time `json:"ends_at"`
	HeldApps       *int      `json:"held_apps,omitempty"`
	HeldDatabases  *int      `json:"held_databases,omitempty"`
	AppsLimit      *int      `json:"apps_limit,omitempty"`
	DatabasesLimit *int      `json:"databases_limit,omitempty"`
	// MaxUnits and MaxPriceCents cap each held resource: its units, and its
	// plan's monthly price in cents.
	MaxUnits      *int `json:"max_units,omitempty"`
	MaxPriceCents *int `json:"max_price_cents,omitempty"`
}

// Change is the answer of a create or a plan change. BillingStartsAt is set
// only while an invited trial holds the resource unbilled: it is when the
// trial ends and billing starts.
type Change struct {
	Status          string     `json:"status"`
	BillingStartsAt *time.Time `json:"billing_starts_at,omitempty"`
}
