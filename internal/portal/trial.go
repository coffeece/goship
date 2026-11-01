package portal

import (
	"errors"
	"io"
	"net/url"
	"time"
)

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

// madeChange is err for a create or a plan change that got a 2xx: an answer
// with an empty body leaves the Change empty instead of turning a change that
// was made into an error somebody retries. An EOF of the transport itself
// (a *url.Error) says nothing about the change and stays an error.
func madeChange(err error) error {
	var transport *url.Error
	if errors.Is(err, io.EOF) && !errors.As(err, &transport) {
		return nil
	}
	return err
}
