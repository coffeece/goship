package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coffeece/goship/internal/archive"
	"github.com/coffeece/goship/internal/portal"
)

// ErrNoOrg is returned when nothing selects an organization and the app
// exists in none of the caller's. The caller words the remedy, since it
// differs between a terminal and an agent.
var ErrNoOrg = errors.New("no organization selected")

// ErrNoProject is ErrNoOrg for the project: the organization is known, the
// app is in none of its projects and it has more than one to choose from.
var ErrNoProject = errors.New("no project selected")

// Options is what a deploy is told. Everything but Dir is optional: the rest
// comes from the project file in Dir, then from the files present.
type Options struct {
	Dir string
	// App is the app name; the directory's name by default.
	App string
	// Org is the organization already chosen by the caller, or empty to let
	// the app's existing home decide.
	Org string
	// Project is the project already chosen by the caller, or empty to let the
	// app's existing home decide.
	Project string
	// Environment is the environment already chosen by the caller, or empty
	// to let the project file, then SelectedEnvironment, then the
	// organization's default decide.
	Environment string
	// SelectedEnvironment is the caller's standing choice for a target
	// (GOSHIP_ENVIRONMENT, or the environment saved for the project), asked
	// after the project file. Optional.
	SelectedEnvironment func(Target) string
	// Orgs is the caller's org list when it already holds one; client.Orgs
	// otherwise. Either way a deploy asks for it at most once.
	Orgs OrgLister

	Platform   string
	Plan       string
	Dockerfile string
	Message    string
	// Env is applied as private variables before the build, on top of the
	// project file's.
	Env map[string]string
}

// Reporter hears a deploy as it goes. The CLI draws it; an agent buffers it.
type Reporter interface {
	Header(format string, args ...any)
	Note(format string, args ...any)
	Begin(key string)
	Done(key, detail string)
	Event(ev portal.ReleaseEvent) error
}

// Result is what a finished deploy reports.
type Result struct {
	App         string        `json:"app"`
	Org         string        `json:"org"`
	Project     string        `json:"project"`
	Environment string        `json:"environment"`
	Platform    string        `json:"platform,omitempty"`
	Plan        string        `json:"plan,omitempty"`
	Created     bool          `json:"created"`
	Version     string        `json:"version,omitempty"`
	URL         string        `json:"url,omitempty"`
	Duration    time.Duration `json:"-"`
	// BillingStartsAt is when billing starts for the app, which an invited
	// trial holds unbilled until then; absent otherwise.
	BillingStartsAt *time.Time `json:"billing_starts_at,omitempty"`
}

// Run deploys o.Dir: says where it deploys to, creates the app when it does
// not exist, applies the variables, uploads the source and follows the
// release. Errors from the release itself are *portal.OperationError, so a
// caller can tell a build that failed from a request that was refused.
func Run(ctx context.Context, client *portal.Client, o Options, r Reporter) (*Result, error) {
	began := time.Now()
	if info, err := os.Stat(o.Dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory to deploy from", o.Dir)
	}
	proj, projFile, err := LoadProject(o.Dir)
	if err != nil {
		return nil, err
	}

	name := firstNonEmpty(o.App, proj.App, filepath.Base(mustAbs(o.Dir)))
	plan := firstNonEmpty(o.Plan, proj.Plan)
	platform := o.Platform
	dockerfile := firstNonEmpty(o.Dockerfile, proj.Dockerfile)

	// The file's project belongs to the org the file names, and its
	// environment to that project.
	want := Target{Org: firstNonEmpty(o.Org, proj.Org), Project: o.Project}
	if want.Project == "" && (proj.Org == "" || proj.Org == want.Org) {
		want.Project = proj.ProjectSlug
	}
	listOrgs := o.Orgs
	if listOrgs == nil {
		listOrgs = client.Orgs
	}
	orgList := sync.OnceValues(func() ([]portal.Org, error) { return listOrgs(ctx) })
	orgs := func(context.Context) ([]portal.Org, error) { return orgList() }

	choice := EnvironmentChoice{Explicit: o.Environment, File: proj, Selected: o.SelectedEnvironment}
	want.Environment = choice.Chosen(want)
	target, err := chooseTarget(ctx, client, orgs, want, name, r)
	if err != nil {
		return nil, err
	}
	if target.Environment == "" {
		// A deploy creates and uploads, so a guessed environment is refused.
		env, err := ResolveEnvironment(ctx, target, choice, orgs)
		if err != nil {
			return nil, UnknownDefaultError(target.Org, err)
		}
		target.Environment = env
	}
	r.Note("Deploying to %s", target)

	// Where the build instructions came from, so the creation line can say
	// so: a wrong guess should be visible in the output, not discovered later.
	var platformSource string
	switch {
	case platform != "":
	case proj.Platform != "":
		platform, platformSource = proj.Platform, "from "+projFile
	default:
		var file string
		if platform, file = DetectPlatform(o.Dir); file != "" {
			platformSource = "detected from " + file
		}
	}
	// A container file is the fallback, not a competitor: it only decides
	// the build when no platform does.
	if platform == "" && dockerfile == "" {
		if found := DetectDockerfile(o.Dir); found != "" {
			dockerfile, platformSource = found, "detected from "+found
		}
	}

	// Not found and forbidden both mean "not usable from here", and both are
	// worth searching the user's other organizations for: a pinned org left
	// over from another account produces the second one.
	existing, lookupErr := client.App(ctx, target.Org, target.Project, target.Environment, name)
	if lookupErr != nil && !portal.IsNotFound(lookupErr) && !portal.IsForbidden(lookupErr) {
		return nil, lookupErr
	}

	buildWith := firstNonEmpty(dockerfile, platform)
	if lookupErr == nil {
		buildWith = firstNonEmpty(dockerfile, existing.Platform, platform)
	}
	if buildWith != "" {
		r.Header("Deploying %s · %s", name, buildWith)
	} else {
		r.Header("Deploying %s", name)
	}
	res := &Result{App: name, Org: target.Org, Project: target.Project, Environment: target.Environment, Platform: platform, Plan: plan}
	if lookupErr != nil {
		if others := AppElsewhere(ctx, client, orgs, target, name); len(others) > 0 {
			return nil, ElsewhereError(name, target, others, portal.IsForbidden(lookupErr))
		}
		if portal.IsForbidden(lookupErr) {
			return nil, lookupErr
		}
		if platform == "" && dockerfile == "" {
			return nil, fmt.Errorf(
				"cannot tell what %q is built with: none of %s or a Dockerfile found in %s.\nPass --platform (%s)",
				name, SignalFiles(), o.Dir, KnownPlatforms())
		}
		// An app with no platform is built by its container file.
		origin := platform
		if origin == "" {
			origin = "built from " + dockerfile
		}
		if platformSource != "" {
			origin += ", " + platformSource
		}
		if plan == "" {
			chosen, err := ChoosePlan(ctx, client, target)
			if err != nil {
				return nil, err
			}
			plan = chosen.Slug
			origin += ", plan " + chosen.DisplayName
		}
		r.Begin("create")
		req := portal.CreateAppRequest{Name: name, Platform: platform, Plan: plan}
		created, err := client.CreateApp(ctx, target.Org, target.Project, target.Environment, req)
		if err != nil {
			return nil, err
		}
		r.Done("create", origin)
		res.Created, res.Plan, res.BillingStartsAt = true, plan, created.BillingStartsAt
	} else {
		res.Platform, res.Plan, res.BillingStartsAt = existing.Platform, existing.PlanSlug, existing.BillingStartsAt
	}

	env := map[string]string{}
	maps.Copy(env, proj.Env)
	maps.Copy(env, o.Env)
	if len(env) > 0 {
		vars := make([]portal.EnvVar, 0, len(env))
		for _, k := range slices.Sorted(maps.Keys(env)) {
			// Secrets arrive this way, so nothing set here is published in
			// the app's public environment.
			vars = append(vars, portal.EnvVar{Name: k, Value: env[k], Public: false})
		}
		r.Begin("variables")
		if err := client.SetEnv(ctx, target.Org, target.Project, target.Environment, name, vars, true); err != nil {
			return nil, err
		}
		r.Done("variables", plural(len(vars), "variable"))
	}

	versions := &versionWatch{Reporter: r}
	if err := Build(ctx, client, BuildArgs{Target: target, Name: name, Dir: o.Dir, Dockerfile: dockerfile, Message: o.Message}, versions); err != nil {
		return res, err
	}
	res.Version = versions.version
	if deployed, err := client.App(ctx, target.Org, target.Project, target.Environment, name); err == nil {
		res.URL = PublicURL(deployed)
		res.BillingStartsAt = deployed.BillingStartsAt
	}
	res.Duration = time.Since(began)
	return res, nil
}

// chooseTarget completes the organization and project the caller left open.
// The app name is known before either has to be, so an unselected one is
// answerable: if this app exists in exactly one of the caller's projects,
// that is the one they meant, in the environment that holds it. A new app in
// an organization with a single project has nowhere else to go. An environment
// in t narrows the search; with none, every environment is searched and an
// empty one is left for the caller when no app decides it.
func chooseTarget(ctx context.Context, client *portal.Client, orgs OrgLister, t Target, name string, r Reporter) (Target, error) {
	if t.Org != "" && t.Project != "" {
		return t, nil
	}
	owners, err := FindApp(ctx, client, orgs, t.Org, t.Project, t.Environment, name)
	if err != nil {
		return Target{}, err
	}
	switch len(owners) {
	case 1:
		// The environment is part of where the app is: kept without it, the
		// next deploy would go to the organization's default instead.
		if t.Org == "" {
			r.Note("Using %s, where %q already exists. Run `goship org use %s`, `goship project use %s` and `goship environment use %s` to keep it.", owners[0], name, owners[0].Org, owners[0].Project, owners[0].Environment)
		} else {
			r.Note("Using project %s in %s, where %q already exists. Run `goship project use %s` and `goship environment use %s` to keep it.", owners[0].Project, owners[0].Environment, name, owners[0].Project, owners[0].Environment)
		}
		return owners[0], nil
	case 0:
		if t.Org == "" {
			return Target{}, ErrNoOrg
		}
		projects, err := client.Projects(ctx, t.Org)
		if err != nil {
			return Target{}, err
		}
		if len(projects) == 1 {
			return Target{Org: t.Org, Project: projects[0].Slug, Environment: t.Environment}, nil
		}
		return Target{}, ErrNoProject
	default:
		return Target{}, ElsewhereError(name, t, owners, false)
	}
}

// versionWatch forwards events and keeps the image version the build
// reported, so the result can name it.
type versionWatch struct {
	Reporter
	version string
}

func (v *versionWatch) Event(ev portal.ReleaseEvent) error {
	if ev.Type == "step" && ev.Step == "build" && ev.State == "done" {
		if ver, ok := strings.CutPrefix(ev.Detail, "image "); ok {
			v.version = ver
		}
	}
	return v.Reporter.Event(ev)
}

// BuildArgs is what the build step needs.
type BuildArgs struct {
	Target                         Target
	Name, Dir, Dockerfile, Message string
}

// Build packs the project and uploads it, reporting the build to r as it
// runs. It is a package var so a test can assert what a deploy asks to be
// built.
var Build = func(ctx context.Context, client *portal.Client, a BuildArgs, r Reporter) error {
	in := portal.DeployRequest{Message: a.Message}
	if a.Dockerfile != "" {
		content, err := os.ReadFile(filepath.Join(a.Dir, a.Dockerfile))
		if err != nil {
			return fmt.Errorf("reading %s: %w", a.Dockerfile, err)
		}
		in.Dockerfile = string(content)
	}

	// The archive is written into the request as it is produced: a project
	// is never held in memory or spooled to disk first.
	pr, pw := io.Pipe()
	sent := &countingWriter{w: pw}
	go func() {
		_, _, err := archive.Write(a.Dir, sent)
		pw.CloseWithError(err) //nolint:errcheck
	}()
	in.Archive = pr

	r.Begin("upload")
	uploaded := false
	err := client.Deploy(ctx, a.Target.Org, a.Target.Project, a.Target.Environment, a.Name, in, func(ev portal.ReleaseEvent) error {
		// The build starts once the upload is in, so the first event ends it.
		if !uploaded {
			uploaded = true
			r.Done("upload", FormatBytes(sent.n.Load()))
		}
		return r.Event(ev)
	})
	pr.CloseWithError(err) //nolint:errcheck
	return err
}

// countingWriter counts what passes through it; the upload's size is what
// the archive compressed to.
type countingWriter struct {
	w io.Writer
	n atomic.Int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n.Add(int64(n))
	return n, err
}

// FormatBytes renders a size the way the upload step shows it.
func FormatBytes(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func mustAbs(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}
