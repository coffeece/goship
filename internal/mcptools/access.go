package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type VisibilityArgs struct {
	AppArg
	Visibility string `json:"visibility" jsonschema:"public or private"`
}

type AccessUserArgs struct {
	AppArg
	Username string `json:"username" jsonschema:"login name of the person"`
	Password string `json:"password,omitempty" jsonschema:"password to set; omit to have one generated"`
}

type AccessUserRef struct {
	AppArg
	Username string `json:"username" jsonschema:"login name from goship_list_app_access"`
}

type AccessTokenArgs struct {
	AppArg
	Reveal bool `json:"reveal,omitempty" jsonschema:"true returns the whole token; false (default) returns it masked and unusable as a header"`
}

type AccessUserList struct {
	Users []portal.AccessUser `json:"users"`
}

type AccessToken struct {
	Token string `json:"token"`
}

func (r *registry) access() {
	tool(r, "goship_set_app_visibility", "Make an app private (GoShip login page on every address, people from goship_add_app_access can get in) or public. Takes effect at once; on a never-deployed app, at the first deploy.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in VisibilityArgs) (Message, error) {
			if err := c.SetAppVisibility(ctx, t.Org, t.Project, in.App, in.Visibility); err != nil {
				return Message{}, err
			}
			return msgf("App %s is now %s.", in.App, in.Visibility)
		})
	tool(r, "goship_list_app_access", "List the people who can get past a private app's login page, with their last login. Passwords are never shown.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in AppArg) (AccessUserList, error) {
			users, err := c.ListAppAccess(ctx, t.Org, t.Project, in.App)
			return AccessUserList{Users: users}, err
		})
	tool(r, "goship_add_app_access", "Let a person past a private app's login page with a username and password. Omit the password to have one generated. The password is returned once, here only.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in AccessUserArgs) (*portal.AccessUserCreated, error) {
			return c.AddAppAccess(ctx, t.Org, t.Project, in.App, in.Username, in.Password)
		})
	tool(r, "goship_reset_app_access", "Set a new password for a person with access to a private app. Omit the password to have one generated. The password is returned once, here only.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in AccessUserArgs) (*portal.AccessUserCreated, error) {
			return c.ResetAppAccess(ctx, t.Org, t.Project, in.App, in.Username, in.Password)
		})
	tool(r, "goship_remove_app_access", "Take a person's access to a private app away; their next request meets the login page.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in AccessUserRef) (Message, error) {
			if err := c.RemoveAppAccess(ctx, t.Org, t.Project, in.App, in.Username); err != nil {
				return Message{}, err
			}
			return msgf("%s can no longer open app %s.", in.Username, in.App)
		})
	tool(r, "goship_get_app_access_token", "Show the token that lets automation (tests, monitors) past a private app's login page, sent as the X-GoShip-Access header. Masked unless reveal is true.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in AccessTokenArgs) (AccessToken, error) {
			tok, err := c.AppAccessToken(ctx, t.Org, t.Project, in.App, in.Reveal)
			return AccessToken{Token: tok}, err
		})
	tool(r, "goship_rotate_app_access_token", "Replace a private app's automation token; the old one stops working at once. The new token is returned once, here only.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in AppArg) (AccessToken, error) {
			tok, err := c.RotateAppAccessToken(ctx, t.Org, t.Project, in.App)
			return AccessToken{Token: tok}, err
		})
}
