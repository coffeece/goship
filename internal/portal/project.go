package portal

import "context"

type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name" table:"NAME"`
	Slug      string `json:"slug" table:"SLUG"`
	IsDefault bool   `json:"is_default" table:"DEFAULT"`
	// Role is the caller's effective role in the project.
	Role string `json:"role" table:"ROLE"`
}

// Projects lists the projects of an organization the caller can reach.
func (c *Client) Projects(ctx context.Context, org string) ([]Project, error) {
	var out []Project
	return out, c.get(ctx, orgPath(org, "/projects"), &out)
}

func (c *Client) CreateProject(ctx context.Context, org, name, slug string) (*Project, error) {
	var out Project
	return &out, c.post(ctx, orgPath(org, "/projects"), map[string]string{"name": name, "slug": slug}, &out)
}
