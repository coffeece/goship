package portal

import (
	"context"
	"time"
)

type Org struct {
	ID   string `json:"id" table:"ID"`
	Name string `json:"name" table:"NAME"`
	Slug string `json:"slug" table:"SLUG"`
	// DefaultEnvironment is the slug of the environment the organization's
	// commands use when none is selected.
	DefaultEnvironment string    `json:"default_environment,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	// Trial is the invited trial of the organization's owner; nil when the
	// owner is not on an active one.
	Trial *Trial `json:"trial,omitempty"`
}

func (c *Client) Orgs(ctx context.Context) ([]Org, error) {
	var orgs []Org
	return orgs, c.get(ctx, "/orgs", &orgs)
}
