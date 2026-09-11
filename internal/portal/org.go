package portal

import (
	"context"
	"time"
)

type Org struct {
	ID        string    `json:"id" table:"ID"`
	Name      string    `json:"name" table:"NAME"`
	Slug      string    `json:"slug" table:"SLUG"`
	CreatedAt time.Time `json:"created_at"`
}

func (c *Client) Orgs(ctx context.Context) ([]Org, error) {
	var orgs []Org
	return orgs, c.get(ctx, "/orgs", &orgs)
}
