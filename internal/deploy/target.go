package deploy

import (
	"cmp"
	"context"
	"fmt"

	"github.com/coffeece/goship/internal/portal"
)

// Target is the organization, project and environment a command acts on.
type Target struct{ Org, Project, Environment string }

// String is "org/project", with "/environment" when one is set.
func (t Target) String() string {
	s := t.Org + "/" + t.Project
	if t.Environment != "" {
		s += "/" + t.Environment
	}
	return s
}

// OrgLister answers GET /orgs. A caller that already holds the list passes
// its own, so a command asks once.
type OrgLister func(context.Context) ([]portal.Org, error)

// FallbackEnvironment is the environment every organization starts with as
// its default, used when the organization's own default cannot be read.
const FallbackEnvironment = "production"

// EnvironmentChoice is what may pick a target's environment, in the order it
// is asked.
type EnvironmentChoice struct {
	// Explicit is what the command was told: --environment, or an agent's
	// environment argument.
	Explicit string
	// File is the project file in the working directory, nil when there is
	// none. Its environment: speaks only for the org and project the file
	// names, as its project: does.
	File *Project
	// Selected is the caller's standing choice for a target: GOSHIP_ENVIRONMENT,
	// else the environment saved for the project. Nil when there is none.
	Selected func(Target) string
}

// Chosen is the environment something picked for t, or "" when nothing did.
// t may be partial: a file that names an org or project speaks only for a
// target that has the same one.
func (c EnvironmentChoice) Chosen(t Target) string {
	if c.Explicit != "" {
		return c.Explicit
	}
	if f := c.File; f != nil && f.Environment != "" && (f.Org == "" || f.Org == t.Org) && (f.ProjectSlug == "" || f.ProjectSlug == t.Project) {
		return f.Environment
	}
	if c.Selected != nil {
		return c.Selected(t)
	}
	return ""
}

// ResolveEnvironment is the environment t acts on, t's org and project being
// resolved: what c picks, else the organization's default from orgs (GET
// /orgs), which is only asked when nothing picked one. No selection is not an
// error, and nothing it returns stops a command that only reads. When the
// org list cannot answer it returns FallbackEnvironment together with why: a
// guess that a command which creates or changes something must refuse with
// UnknownDefaultError. An org the list does not carry, or carries without a
// default, is FallbackEnvironment too; the call that follows answers for it.
func ResolveEnvironment(ctx context.Context, t Target, c EnvironmentChoice, orgs OrgLister) (string, error) {
	if env := c.Chosen(t); env != "" {
		return env, nil
	}
	list, err := orgs(ctx)
	if err != nil {
		return FallbackEnvironment, err
	}
	for _, o := range list {
		if o.Slug == t.Org {
			return defaultEnvironment(o), nil
		}
	}
	return FallbackEnvironment, nil
}

// UnknownDefaultError refuses to act in an environment nobody chose when
// org's default could not be read (err, from ResolveEnvironment).
func UnknownDefaultError(org string, err error) error {
	return fmt.Errorf("could not determine the default environment of %s: %w; pass --environment", org, err)
}

func defaultEnvironment(o portal.Org) string {
	return cmp.Or(o.DefaultEnvironment, FallbackEnvironment)
}
