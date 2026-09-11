package portal

import "context"

type Me struct {
	ID            string   `json:"id"`
	Email         string   `json:"email" table:"EMAIL"`
	Name          string   `json:"name" table:"NAME"`
	Groups        []string `json:"groups" table:"ORGS"`
	Provider      string   `json:"provider"`
	EmailVerified bool     `json:"email_verified"`
	IsAdmin       bool     `json:"is_admin"`
}

// Login exchanges credentials for a bearer token the API accepts.
func (c *Client) Login(ctx context.Context, email, password string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	err := c.post(ctx, "/login", map[string]string{"email": email, "password": password}, &out)
	return out.Token, err
}

func (c *Client) Me(ctx context.Context) (*Me, error) {
	var me Me
	return &me, c.get(ctx, "/auth/me", &me)
}
