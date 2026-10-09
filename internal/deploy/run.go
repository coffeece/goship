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
	"sync/atomic"
	"time"

	"github.com/coffeece/goship/internal/archive"
	"github.com/coffeece/goship/internal/portal"
)

// ErrNoOrg is returned when nothing selects an organization and the app
// exists in none of the caller's. The caller words the remedy, since it
// differs between a terminal and an agent.
var ErrNoOrg = errors.New("no organization selected")

// Options is what a deploy is told. Everything but Dir is optional: the rest
// comes from the project file in Dir, then from the files present.
type Options struct {
	Dir string
	// App is the app name; the directory's name by default.
	App string
	// Org is the organization already chosen by the caller, or empty to let
	// the app's existing home decide.
	Org        string
	Platform   string
	Plan       string
	Node       string
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
	App      string        `json:"app"`
	Org      string        `json:"org"`
	Platform string        `json:"platform,omitempty"`
	Plan     string        `json:"plan,omitempty"`
	Created  bool          `json:"created"`
	Version  string        `json:"version,omitempty"`
	URL      string        `json:"url,omitempty"`
	Duration time.Duration `json:"-"`
}

// Run deploys o.Dir: creates the app when it does not exist, applies the
// environment, uploads the source and follows the release. Errors from the
// release itself are *portal.OperationError, so a caller can tell a build
// that failed from a request that was refused.
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
	node := firstNonEmpty(o.Node, proj.Node)
	platform := o.Platform
	dockerfile := firstNonEmpty(o.Dockerfile, proj.Dockerfile)

	// The app name is known before the organization has to be, so an
	// unselected org is answerable: if this app exists in exactly one of the
	// caller's organizations, that is the one they meant.
	org := firstNonEmpty(o.Org, proj.Org)
	if org == "" {
		owners := AppInOtherOrgs(ctx, client, "", name)
		switch len(owners) {
		case 1:
			org = owners[0]
			r.Note("Using org %s, where %q already exists. Run `goship org use %s` to keep it.", org, name, org)
		case 0:
			return nil, ErrNoOrg
		default:
			return nil, ElsewhereError(name, "", owners, false)
		}
	}

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
	existing, lookupErr := client.App(ctx, org, name)
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
	res := &Result{App: name, Org: org, Platform: platform, Plan: plan}
	if lookupErr != nil {
		if others := AppInOtherOrgs(ctx, client, org, name); len(others) > 0 {
			return nil, ElsewhereError(name, org, others, portal.IsForbidden(lookupErr))
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
		nodeID, where, err := PlaceNewApp(ctx, client, org, node)
		if err != nil {
			return nil, err
		}
		// On your own hardware there is nothing to bill, so the API applies
		// the free plan itself when none is named. Asking the user to pick
		// from the paid catalogue would be wrong.
		switch {
		case OnNode(nodeID):
			origin += ", " + where
		case plan == "":
			chosen, err := ChoosePlan(ctx, client, org)
			if err != nil {
				return nil, err
			}
			plan = chosen.Slug
			origin += ", plan " + chosen.DisplayName
		}
		r.Begin("create")
		req := portal.CreateAppRequest{Name: name, Platform: platform, Plan: plan}
		if nodeID != "" {
			req.NodeID = &nodeID
		}
		if _, err := client.CreateApp(ctx, org, req); err != nil {
			return nil, err
		}
		r.Done("create", origin)
		res.Created, res.Plan = true, plan
	} else {
		res.Platform, res.Plan = existing.Platform, existing.PlanSlug
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
		r.Begin("environment")
		if err := client.SetEnv(ctx, org, name, vars, true); err != nil {
			return nil, err
		}
		r.Done("environment", plural(len(vars), "variable"))
	}

	versions := &versionWatch{Reporter: r}
	if err := Build(ctx, client, BuildArgs{Org: org, Name: name, Dir: o.Dir, Dockerfile: dockerfile, Message: o.Message}, versions); err != nil {
		return res, err
	}
	res.Version = versions.version
	if deployed, err := client.App(ctx, org, name); err == nil {
		res.URL = PublicURL(deployed)
	}
	res.Duration = time.Since(began)
	return res, nil
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
	Org, Name, Dir, Dockerfile, Message string
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
	err := client.Deploy(ctx, a.Org, a.Name, in, func(ev portal.ReleaseEvent) error {
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
