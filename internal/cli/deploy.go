package cli

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/coffeece/goship/internal/archive"
	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
)

// projectFiles are the optional config names, in the order they are tried.
// Nothing requires one: every value in it can be passed as a flag or inferred.
var projectFiles = []string{"goship.yaml", "goship.yml", ".goship.yaml", ".goship.yml"}

func newDeployCmd(app *App) *cobra.Command {
	var appName, platform, plan, envFile, message, dockerfile, node string

	cmd := &cobra.Command{
		Use:   "deploy [dir]",
		Short: "Create the app if needed, apply env and deploy",
		Long: "Deploys the current directory. No configuration is required: the app is\n" +
			"named after the directory, its platform is inferred from the files present,\n" +
			"and it is created on first deploy.\n\n" +
			"A goship.yml (or goship.yaml) is an optional shortcut for the same values.\n" +
			"Flags beat the file, the file beats what is inferred.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			if err := noJSON(app, cmd); err != nil {
				return err
			}
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			// Before anything is created on the other side.
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return fmt.Errorf("%s is not a directory to deploy from", dir)
			}

			p := newProgress(cmd.OutOrStdout(), app.Global.Verbose)
			defer func() { p.Finish(err) }()

			proj, projFile, err := deploy.LoadProject(dir)
			if err != nil {
				return err
			}

			name := firstNonEmpty(appName, proj.App, filepath.Base(mustAbs(dir)))
			plan = firstNonEmpty(plan, proj.Plan)
			node = firstNonEmpty(node, proj.Node)

			client := app.Portal()

			// The app name is known before the organization has to be, so an
			// unselected org is answerable: if this app exists in exactly one
			// of the caller's organizations, that is the one they meant.
			if proj.Org != "" {
				app.Global.Org = firstNonEmpty(app.Global.Org, proj.Org)
			}
			org, err := app.Org(cmd.Context())
			if err != nil {
				owners := deploy.AppInOtherOrgs(cmd.Context(), client, "", name)
				if len(owners) != 1 {
					if len(owners) > 1 {
						return deploy.ElsewhereError(name, "", owners, false)
					}
					return err
				}
				org = owners[0]
				p.Note("Using org %s, where %q already exists. Run `goship org use %s` to keep it.", org, name, org)
			}

			// Where the build instructions came from, so the creation line can
			// say so: a wrong guess should be visible in the output, not
			// discovered later.
			var platformSource string
			switch {
			case platform != "":
			case proj.Platform != "":
				platform, platformSource = proj.Platform, "from "+projFile
			default:
				var file string
				if platform, file = deploy.DetectPlatform(dir); file != "" {
					platformSource = "detected from " + file
				}
			}

			// A container file is the fallback, not a competitor: it only
			// decides the build when no platform does.
			dockerfile = firstNonEmpty(dockerfile, proj.Dockerfile)
			if platform == "" && dockerfile == "" {
				if found := deploy.DetectDockerfile(dir); found != "" {
					dockerfile, platformSource = found, "detected from "+found
				}
			}

			// Not found and forbidden both mean "not usable from here", and both
			// are worth searching the user's other organizations for: a pinned
			// org left over from another account produces the second one.
			existing, lookupErr := client.App(cmd.Context(), org, name)
			if lookupErr != nil && !portal.IsNotFound(lookupErr) && !portal.IsForbidden(lookupErr) {
				return lookupErr
			}

			buildWith := firstNonEmpty(dockerfile, platform)
			if lookupErr == nil {
				buildWith = firstNonEmpty(dockerfile, existing.Platform, platform)
			}
			if buildWith != "" {
				p.Header("Deploying %s · %s", name, buildWith)
			} else {
				p.Header("Deploying %s", name)
			}
			if lookupErr != nil {
				if others := deploy.AppInOtherOrgs(cmd.Context(), client, org, name); len(others) > 0 {
					return deploy.ElsewhereError(name, org, others, portal.IsForbidden(lookupErr))
				}
				if portal.IsForbidden(lookupErr) {
					return lookupErr
				}
				if platform == "" && dockerfile == "" {
					return fmt.Errorf(
						"cannot tell what %q is built with: none of %s or a Dockerfile found in %s.\nPass --platform (%s)",
						name, deploy.SignalFiles(), dir, deploy.KnownPlatforms())
				}
				// An app with no platform is built by its container file.
				origin := platform
				if origin == "" {
					origin = "built from " + dockerfile
				}
				if platformSource != "" {
					origin += ", " + platformSource
				}
				// On your own hardware there is nothing to bill, so the API
				// applies the free plan itself when none is named. Asking the
				// user to pick from the paid catalogue would be wrong.
				switch {
				case node != "":
					origin += ", on your node"
				case plan == "":
					chosen, err := deploy.ChoosePlan(cmd.Context(), client, org)
					if err != nil {
						return err
					}
					plan = chosen.Slug
					origin += ", plan " + chosen.DisplayName
				}
				p.Begin("create")
				req := portal.CreateAppRequest{Name: name, Platform: platform, Plan: plan}
				if node != "" {
					id, err := deploy.ResolveNode(cmd.Context(), client, org, node)
					if err != nil {
						return err
					}
					req.NodeID = &id
				}
				if _, err := client.CreateApp(cmd.Context(), org, req); err != nil {
					return err
				}
				p.Done("create", origin)
			}

			env := map[string]string{}
			maps.Copy(env, proj.Env)
			if envFile != "" {
				fromFile, err := deploy.ParseEnvFile(envFile)
				if err != nil {
					return err
				}
				maps.Copy(env, fromFile)
			}
			if len(env) > 0 {
				vars := make([]portal.EnvVar, 0, len(env))
				for _, k := range slices.Sorted(maps.Keys(env)) {
					// Secrets arrive through --env-file, so nothing set here is
					// published in the app's public environment.
					vars = append(vars, portal.EnvVar{Name: k, Value: env[k], Public: false})
				}
				p.Begin("environment")
				if err := client.SetEnv(cmd.Context(), org, name, vars, true); err != nil {
					return err
				}
				p.Done("environment", plural(len(vars), "variable"))
			}

			buildErr := runBuild(cmd, app, buildArgs{
				org:        org,
				name:       name,
				dir:        dir,
				dockerfile: dockerfile,
				message:    message,
			}, p)
			p.Finish(buildErr)
			if buildErr != nil {
				// Only a release that ran has a log to point at.
				ran := isOperationError(buildErr)
				buildErr = explainStreamError(buildErr, "deploy of "+name, true)
				if ran && !app.Global.Verbose {
					buildErr = fmt.Errorf("%w\nFull log: goship deploy --verbose  \u00b7  history: goship releases -a %s", buildErr, name)
				}
				return buildErr
			}
			if deployed, appErr := client.App(cmd.Context(), org, name); appErr == nil {
				if url := deploy.PublicURL(deployed); url != "" {
					p.Summary(url)
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (default: the directory name)")
	f.StringVar(&platform, "platform", "", "platform for a new app (default: inferred from the files present)")
	f.StringVar(&dockerfile, "dockerfile", "", "build from this container file instead of a platform")
	f.StringVar(&node, "node", "", "place a new app on one of your own machines, by name (never billed)")
	f.StringVar(&plan, "plan", "", "plan, used when creating the app")
	f.StringVar(&envFile, "env-file", "", "dotenv file applied as private variables")
	f.StringVarP(&message, "message", "m", "", "deploy message")

	return cmd
}

// buildArgs is what the build step needs.
type buildArgs struct {
	org, name, dir, dockerfile, message string
}

// runBuild packs the project and uploads it, reporting the build to p as it
// runs. It is a package var so a test can assert what a deploy asks to be
// built.
var runBuild = func(cmd *cobra.Command, app *App, a buildArgs, p *progress) error {
	in := portal.DeployRequest{Message: a.message}
	if a.dockerfile != "" {
		content, err := os.ReadFile(filepath.Join(a.dir, a.dockerfile))
		if err != nil {
			return fmt.Errorf("reading %s: %w", a.dockerfile, err)
		}
		in.Dockerfile = string(content)
	}

	// The archive is written into the request as it is produced: a project is
	// never held in memory or spooled to disk first.
	pr, pw := io.Pipe()
	sent := &countingWriter{w: pw}
	go func() {
		_, _, err := archive.Write(a.dir, sent)
		pw.CloseWithError(err) //nolint:errcheck
	}()
	in.Archive = pr

	p.Begin("upload")
	uploaded := false
	err := app.Portal().Deploy(cmd.Context(), a.org, a.name, in, func(ev portal.ReleaseEvent) error {
		// The build starts once the upload is in, so the first event ends it.
		if !uploaded {
			uploaded = true
			p.Done("upload", formatBytes(sent.n.Load()))
		}
		return p.Event(ev)
	})
	pr.CloseWithError(err) //nolint:errcheck
	return err
}

// countingWriter counts what passes through it; the upload's size is what the
// archive compressed to.
type countingWriter struct {
	w io.Writer
	n atomic.Int64
}

func (c *countingWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n.Add(int64(n))
	return n, err
}

func formatBytes(n int64) string {
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

func isOperationError(err error) bool {
	var opErr *portal.OperationError
	return errors.As(err, &opErr)
}

// explainStreamError turns the ways a streamed operation ends badly into what
// the person should do about it. detached says the operation carries on
// server-side after a dropped connection, which is true of releases only.
func explainStreamError(err error, what string, detached bool) error {
	var opErr *portal.OperationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &opErr):
		msg := strings.TrimPrefix(strings.TrimPrefix(opErr.Message, "deploy failed: "), "command failed: ")
		return fmt.Errorf("%s failed: %s", what, msg)
	case errors.Is(err, portal.ErrStreamCut) && detached:
		return fmt.Errorf("lost the connection during the %s. It keeps running on GoShip — `goship releases` shows how it ended", what)
	case errors.Is(err, portal.ErrStreamCut):
		return fmt.Errorf("lost the connection during the %s", what)
	}
	return err
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
