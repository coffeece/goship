package portal

import (
	"context"
	"net/url"
	"time"
)

type AccessUser struct {
	Username    string     `json:"username" table:"USERNAME"`
	CreatedAt   time.Time  `json:"created_at" table:"CREATED"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty" table:"LAST LOGIN"`
}

// AccessUserCreated carries the password only when the server generated it.
type AccessUserCreated struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
}

type accessPassword struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func (c *Client) SetAppVisibility(ctx context.Context, org, project, app, visibility string) error {
	return c.put(ctx, appPath(org, project, app)+"/visibility", map[string]string{"visibility": visibility}, nil)
}

func (c *Client) ListAppAccess(ctx context.Context, org, project, app string) ([]AccessUser, error) {
	var out struct {
		Users []AccessUser `json:"users"`
	}
	err := c.get(ctx, appPath(org, project, app)+"/access/users", &out)
	return out.Users, err
}

func (c *Client) AddAppAccess(ctx context.Context, org, project, app, username, password string) (*AccessUserCreated, error) {
	var out AccessUserCreated
	return &out, c.post(ctx, appPath(org, project, app)+"/access/users", accessPassword{username, password}, &out)
}

func (c *Client) ResetAppAccess(ctx context.Context, org, project, app, username, password string) (*AccessUserCreated, error) {
	var out AccessUserCreated
	return &out, c.put(ctx, appPath(org, project, app)+"/access/users/"+esc(username), accessPassword{Password: password}, &out)
}

func (c *Client) RemoveAppAccess(ctx context.Context, org, project, app, username string) error {
	return c.delete(ctx, appPath(org, project, app)+"/access/users/"+esc(username), nil)
}

// AppAccessToken returns the token masked unless reveal is set.
func (c *Client) AppAccessToken(ctx context.Context, org, project, app string, reveal bool) (string, error) {
	path := appPath(org, project, app) + "/access/token"
	if reveal {
		path += "?" + url.Values{"reveal": {"1"}}.Encode()
	}
	var out struct {
		Token string `json:"token"`
	}
	err := c.get(ctx, path, &out)
	return out.Token, err
}

func (c *Client) RotateAppAccessToken(ctx context.Context, org, project, app string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	err := c.post(ctx, appPath(org, project, app)+"/access/token/rotate", nil, &out)
	return out.Token, err
}
