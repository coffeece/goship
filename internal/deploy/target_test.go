package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/coffeece/goship/internal/config"
	"github.com/coffeece/goship/internal/portal"
)

// orgList stands in for GET /orgs and counts how often it is asked.
type orgList struct {
	orgs  []portal.Org
	err   error
	calls int
}

func (l *orgList) list(context.Context) ([]portal.Org, error) {
	l.calls++
	return l.orgs, l.err
}

func TestTargetEnvironmentResolution(t *testing.T) {
	saved := &config.Config{Environments: map[string]string{"acme/atlas": "qa"}}
	none := &config.Config{}
	acme := Target{Org: "acme", Project: "atlas"}
	cases := []struct {
		name   string
		target Target
		flag   string
		file   *Project
		envVar string
		config *config.Config
		want   string
	}{
		{"--environment beats everything", acme, "preview", &Project{Environment: "staging"}, "dev", saved, "preview"},
		{"goship.yaml beats GOSHIP_ENVIRONMENT", acme, "", &Project{Environment: "staging"}, "dev", saved, "staging"},
		{"GOSHIP_ENVIRONMENT beats the saved choice", acme, "", &Project{App: "api"}, "dev", saved, "dev"},
		{"the saved choice beats the default", acme, "", nil, "", saved, "qa"},
		{"the saved choice is the project's own", Target{Org: "acme", Project: "comet"}, "", nil, "", saved, "production"},
		{"goship.yaml of the same org counts", acme, "", &Project{Org: "acme", ProjectSlug: "atlas", Environment: "staging"}, "", none, "staging"},
		{"other org's goship.yaml is ignored", acme, "", &Project{Org: "other", Environment: "staging"}, "", none, "production"},
		{"another project's goship.yaml is ignored", acme, "", &Project{ProjectSlug: "comet", Environment: "staging"}, "", none, "production"},
		{"no selection → default from /orgs", Target{Org: "globex", Project: "web"}, "", nil, "", none, "live"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOSHIP_ENVIRONMENT", tc.envVar)
			orgs := &orgList{orgs: []portal.Org{{Slug: "acme", DefaultEnvironment: "production"}, {Slug: "globex", DefaultEnvironment: "live"}}}
			choice := EnvironmentChoice{
				Explicit: tc.flag,
				File:     tc.file,
				Selected: func(t Target) string { return tc.config.SelectedEnvironment(t.Org, t.Project) },
			}
			if got := ResolveEnvironment(context.Background(), tc.target, choice, orgs.list); got != tc.want {
				t.Errorf("ResolveEnvironment = %q, want %q", got, tc.want)
			}
			// The org list is for the default only: a chosen environment
			// costs no request.
			if asked := orgs.calls > 0; asked != (tc.want == "production" || tc.want == "live") {
				t.Errorf("GET /orgs asked %d times", orgs.calls)
			}
		})
	}
}

// Nothing chosen is never an error: an org list that cannot answer, or that
// names no default, leaves the environment every organization starts with.
func TestResolveEnvironmentFallsBackToProduction(t *testing.T) {
	acme := Target{Org: "acme", Project: "atlas"}
	for name, orgs := range map[string]*orgList{
		"/orgs fails":        {err: errors.New("boom")},
		"org not listed":     {orgs: []portal.Org{{Slug: "other", DefaultEnvironment: "live"}}},
		"org has no default": {orgs: []portal.Org{{Slug: "acme"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ResolveEnvironment(context.Background(), acme, EnvironmentChoice{}, orgs.list); got != "production" {
				t.Errorf("got %q", got)
			}
		})
	}
}

func TestTargetStringNamesTheEnvironment(t *testing.T) {
	if got := (Target{Org: "acme", Project: "atlas", Environment: "staging"}).String(); got != "acme/atlas/staging" {
		t.Errorf("got %q", got)
	}
	if got := (Target{Org: "acme", Project: "atlas"}).String(); got != "acme/atlas" {
		t.Errorf("without an environment: %q", got)
	}
}
