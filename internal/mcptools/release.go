package mcptools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
)

type DeployArgs struct {
	OrgArg
	Path       string            `json:"path" jsonschema:"directory to deploy, absolute or relative to the server's working directory"`
	App        string            `json:"app,omitempty" jsonschema:"app name; defaults to goship.yml's app, then the directory name"`
	Platform   string            `json:"platform,omitempty" jsonschema:"go, python, nodejs or static; inferred from the files present when omitted"`
	Plan       string            `json:"plan,omitempty" jsonschema:"plan slug for a new app; a free plan is picked only when the org has one"`
	Node       string            `json:"node,omitempty" jsonschema:"run a new app on this node of the organization's own (never billed), or goship for GoShip's servers; omitted, the organization's default placement decides"`
	Dockerfile string            `json:"dockerfile,omitempty" jsonschema:"build from this container file instead of a platform"`
	Message    string            `json:"message,omitempty" jsonschema:"release message"`
	Env        map[string]string `json:"env,omitempty" jsonschema:"private variables applied before the build"`
}

// DeployResult is a finished deploy, with the steps it went through.
type DeployResult struct {
	deploy.Result
	DurationSeconds float64 `json:"duration_seconds"`
	Steps           []Step  `json:"steps"`
}

type Step struct {
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

type ReleaseList struct {
	Releases []portal.Deploy `json:"releases"`
}

type ListReleasesArgs struct {
	AppArg
	Limit int `json:"limit,omitempty" jsonschema:"how many, newest first; default 20"`
}

type RollbackArgs struct {
	AppArg
	Version string `json:"version" jsonschema:"release to run again, e.g. v12 (goship_list_releases shows them)"`
}

type RollbackResult struct {
	App             string  `json:"app"`
	Version         string  `json:"version"`
	DurationSeconds float64 `json:"duration_seconds"`
	Steps           []Step  `json:"steps"`
}

func (r *registry) releases(withDeploy bool) {
	if withDeploy {
		tool(r, "goship_deploy", "Deploy a directory: creates the app on first deploy (platform inferred from go.mod, pyproject.toml, requirements.txt, package.json or index.html; a Dockerfile is the fallback), applies env, uploads the source and follows the build and release. Returns the version and public URL. Can take minutes.", write,
			func(ctx context.Context, c *portal.Client, org string, in DeployArgs) (*DeployResult, error) {
				rec := &recorder{}
				res, err := deploy.Run(ctx, c, deploy.Options{
					Dir: in.Path, App: in.App, Org: org, Platform: in.Platform, Plan: in.Plan,
					Node: in.Node, Dockerfile: in.Dockerfile, Message: in.Message, Env: in.Env,
				}, rec)
				if err != nil {
					return nil, rec.explain(err)
				}
				return &DeployResult{Result: *res, DurationSeconds: res.Duration.Seconds(), Steps: rec.steps()}, nil
			})
	} else {
		tool(r, "goship_deploy", "Not available on the hosted server: a deploy reads a directory on your machine. Run `goship deploy` or the local `goship mcp` server instead.", read,
			func(context.Context, *portal.Client, string, DeployArgs) (Message, error) {
				return Message{}, errors.New("deploys need the project directory, which this hosted server cannot read: run `goship deploy <dir>` in a terminal, or add the local server with `goship mcp`")
			})
	}
	tool(r, "goship_list_releases", "List an app's releases, newest first, with who deployed and whether it can be rolled back to.", read,
		func(ctx context.Context, c *portal.Client, org string, in ListReleasesArgs) (ReleaseList, error) {
			limit := in.Limit
			if limit == 0 {
				limit = 20
			}
			rel, err := c.Deploys(ctx, org, in.App, limit)
			return ReleaseList{Releases: rel}, err
		})
	tool(r, "goship_rollback", "Release a previous version of an app again.", write,
		func(ctx context.Context, c *portal.Client, org string, in RollbackArgs) (*RollbackResult, error) {
			rec := &recorder{}
			began := time.Now()
			if err := c.Rollback(ctx, org, in.App, in.Version, rec.Event); err != nil {
				return nil, rec.explain(err)
			}
			return &RollbackResult{App: in.App, Version: in.Version, DurationSeconds: time.Since(began).Seconds(), Steps: rec.steps()}, nil
		})
}

// recorder is a deploy.Reporter that keeps the steps and the output, so a
// failure can carry the last lines of the log.
type recorder struct {
	mu    sync.Mutex
	order []string
	done  map[string]string
	lines []string
}

const logTail = 50

func (r *recorder) Header(string, ...any) {}
func (r *recorder) Note(string, ...any)   {}

func (r *recorder) Begin(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, key)
}

func (r *recorder) Done(key, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done == nil {
		r.done = map[string]string{}
	}
	r.done[key] = detail
}

func (r *recorder) Event(ev portal.ReleaseEvent) error {
	switch ev.Type {
	case "output":
		r.mu.Lock()
		for l := range strings.SplitSeq(strings.TrimRight(ev.Data, "\n"), "\n") {
			if strings.TrimSpace(l) != "" {
				r.lines = append(r.lines, l)
			}
		}
		if len(r.lines) > 2000 {
			r.lines = r.lines[len(r.lines)-2000:]
		}
		r.mu.Unlock()
	case "step":
		switch ev.State {
		case "start":
			r.Begin(ev.Step)
		case "done":
			r.Done(ev.Step, ev.Detail)
		}
	}
	return nil
}

func (r *recorder) steps() []Step {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Step, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, Step{Name: k, Detail: r.done[k]})
	}
	return out
}

// explain turns a failed release into an error that carries the reason and
// the end of its log, which is where the reason usually is.
func (r *recorder) explain(err error) error {
	var opErr *portal.OperationError
	switch {
	case errors.As(err, &opErr):
		err = fmt.Errorf("release failed: %s", strings.TrimPrefix(opErr.Message, "deploy failed: "))
	case errors.Is(err, portal.ErrStreamCut):
		return fmt.Errorf("lost the connection during the release; it keeps running on GoShip — goship_list_releases shows how it ended")
	default:
		return err
	}
	r.mu.Lock()
	lines := r.lines
	if len(lines) > logTail {
		lines = lines[len(lines)-logTail:]
	}
	r.mu.Unlock()
	if len(lines) == 0 {
		return err
	}
	return fmt.Errorf("%w\n\nlast %d log lines:\n%s", err, len(lines), strings.Join(lines, "\n"))
}
