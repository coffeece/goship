package mcptools

import (
	"context"

	"github.com/coffeece/goship/internal/portal"
)

type TokenList struct {
	Tokens []portal.APIToken `json:"tokens"`
}

type CreateTokenArgs struct {
	Name          string `json:"name" jsonschema:"what the token is for, e.g. ci"`
	ExpiresInDays int    `json:"expires_in_days,omitempty" jsonschema:"days until it expires; 0 for never"`
}

type TokenIDArg struct {
	ID string `json:"id" jsonschema:"token id from goship_list_api_tokens"`
}

func (r *registry) tokens() {
	orgless(r, "goship_list_api_tokens", "List the user's API tokens (for CI and scripts), without their secrets.", read,
		func(ctx context.Context, c *portal.Client, _ struct{}) (TokenList, error) {
			t, err := c.APITokens(ctx)
			return TokenList{Tokens: t}, err
		})
	orgless(r, "goship_create_api_token", "Create an API token. The secret is returned once, here only; it is used as GOSHIP_TOKEN or a Bearer header.", write,
		func(ctx context.Context, c *portal.Client, in CreateTokenArgs) (*portal.APIToken, error) {
			return c.CreateAPIToken(ctx, in.Name, in.ExpiresInDays)
		})
	orgless(r, "goship_revoke_api_token", "Revoke an API token so it stops working everywhere.", destructive,
		func(ctx context.Context, c *portal.Client, in TokenIDArg) (Message, error) {
			if err := c.RevokeAPIToken(ctx, in.ID); err != nil {
				return Message{}, err
			}
			return msgf("Token revoked.")
		})
}
