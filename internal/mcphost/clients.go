package mcphost

import (
	"context"
	"errors"
	"fmt"

	"github.com/coffeece/goship/internal/portal"
)

// tokenClients serves each tool call with the token of the request it came
// in on, which requireToken put in the context. Nothing is selected the way
// `goship org use` selects on a machine, so an organization or project is
// whatever the tool was given, or the only one the user has.
type tokenClients struct {
	api     string
	version string
}

type tokenKey struct{}

func withToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey{}, token)
}

func (t tokenClients) Client(ctx context.Context) (*portal.Client, error) {
	token, _ := ctx.Value(tokenKey{}).(string)
	if token == "" {
		// requireToken refuses unauthenticated requests before any tool runs.
		return nil, errors.New("not signed in: reconnect the GoShip connector")
	}
	return portal.New(t.api, token, portal.WithVersion(t.version)), nil
}

func (t tokenClients) Org(ctx context.Context, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	c, err := t.Client(ctx)
	if err != nil {
		return "", err
	}
	orgs, err := c.Orgs(ctx)
	if err != nil {
		return "", err
	}
	switch len(orgs) {
	case 1:
		return orgs[0].Slug, nil
	case 0:
		return "", errors.New("you do not belong to any organization: create one in the GoShip dashboard, or ask to be invited to one")
	}
	return "", fmt.Errorf("you belong to %d organizations and none is selected: pass org; goship_list_orgs shows them", len(orgs))
}

func (t tokenClients) Project(ctx context.Context, c *portal.Client, org, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	projects, err := c.Projects(ctx, org)
	if err != nil {
		return "", err
	}
	switch len(projects) {
	case 1:
		return projects[0].Slug, nil
	case 0:
		return "", errors.New("you cannot reach any project in " + org + ": ask an admin of the organization to add you to one")
	}
	return "", fmt.Errorf("%s has %d projects and none is selected: pass project; goship_list_projects shows them", org, len(projects))
}
