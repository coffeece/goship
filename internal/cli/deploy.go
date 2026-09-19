package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/coffeece/goship/internal/archive"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

// projectFiles are the optional config names, in the order they are tried.
// Nothing requires one: every value in it can be passed as a flag or inferred.
var projectFiles = []string{"goship.yaml", "goship.yml", ".goship.yaml", ".goship.yml"}

// project is the optional config in a project root. Flags win over it.
type project struct {
	App string `yaml:"app"`
	// Org pins the organization for the project, so a repo that belongs to one
	// org does not depend on whatever `goship org use` was last pointed at.
	Org      string `yaml:"org"`
	Platform string `yaml:"platform"`
	Plan     string `yaml:"plan"`
	// Node places the app on one of the organization's own machines. A
	// node-placed app is never billed, so it needs no plan.
	Node string            `yaml:"node"`
	Env  map[string]string `yaml:"env"`
	// Dockerfile names a container file to build from instead of letting a
	// platform build the source. Mutually exclusive with platform.
	Dockerfile string `yaml:"dockerfile"`
}

// loadProject reads the first config file that exists, returning its name so
// messages can say which one was used. A directory with none is not an error.
func loadProject(dir string) (*project, string, error) {
	for _, name := range projectFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		p := &project{}
		if err := yaml.Unmarshal(data, p); err != nil {
			return nil, "", fmt.Errorf("parsing %s: %w", path, err)
		}
		return p, name, nil
	}
	return &project{}, "", nil
}

// parseEnvFile reads a dotenv-style file: KEY=VALUE per line, # comments and
// blank lines ignored, surrounding quotes stripped.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	env := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s: expected KEY=VALUE, got %q", path, line)
		}
		env[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
	}
	return env, scanner.Err()
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func newDeployCmd(app *App) *cobra.Command {
	var appName, platform, plan, envFile, message, dockerfile, node string

	cmd := &cobra.Command{
		Use:   "deploy [dir]",
		Short: "Create the app if needed, apply env and deploy",
		Long: "Deploys the current directory. No configuration is required: the app is\n" +
			"named after the directory, its platform is inferred from the files present,\n" +
			"and it is created on first deploy.\n\n" +
			"A goship.yaml (or .goship.yaml) is an optional shortcut for the same values.\n" +
			"Flags beat the file, the file beats what is inferred.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Global.Output == render.JSON {
				return fmt.Errorf("deploy streams its output; --output json is not supported")
			}
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			// Before anything is created on the other side.
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return fmt.Errorf("%s is not a directory to deploy from", dir)
			}
			proj, projFile, err := loadProject(dir)
			if err != nil {
				return err
			}

			name := firstNonEmpty(appName, proj.App, filepath.Base(mustAbs(dir)))
			plan = firstNonEmpty(plan, proj.Plan)
			node = firstNonEmpty(node, proj.Node)

			r := app.Renderer()
			client := app.Portal()

			// The app name is known before the organization has to be, so an
			// unselected org is answerable: if this app exists in exactly one
			// of the caller's organizations, that is the one they meant.
			if proj.Org != "" {
				app.Global.Org = firstNonEmpty(app.Global.Org, proj.Org)
			}
			org, err := app.Org(cmd.Context())
			if err != nil {
				owners := appInOtherOrgs(cmd.Context(), client, "", name)
				if len(owners) != 1 {
					if len(owners) > 1 {
						return elsewhereError(name, "", owners, false)
					}
					return err
				}
				org = owners[0]
				if err := r.Message("Using org %s, where %q already exists. Run `goship org use %s` to keep it.", org, name, org); err != nil {
					return err
				}
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
				if platform, file = detectPlatform(dir); file != "" {
					platformSource = "detected from " + file
				}
			}

			// A container file is the fallback, not a competitor: it only
			// decides the build when no platform does.
			dockerfile = firstNonEmpty(dockerfile, proj.Dockerfile)
			if platform == "" && dockerfile == "" {
				if found := detectDockerfile(dir); found != "" {
					dockerfile, platformSource = found, "detected from "+found
				}
			}

			// Not found and forbidden both mean "not usable from here", and both
			// are worth searching the user's other organizations for: a pinned
			// org left over from another account produces the second one.
			_, lookupErr := client.App(cmd.Context(), org, name)
			if lookupErr != nil && !portal.IsNotFound(lookupErr) && !portal.IsForbidden(lookupErr) {
				return lookupErr
			}
			if lookupErr != nil {
				if others := appInOtherOrgs(cmd.Context(), client, org, name); len(others) > 0 {
					return elsewhereError(name, org, others, portal.IsForbidden(lookupErr))
				}
				if portal.IsForbidden(lookupErr) {
					return lookupErr
				}
				if platform == "" && dockerfile == "" {
					return fmt.Errorf(
						"cannot tell what %q is built with: none of %s or a Dockerfile found in %s.\nPass --platform (%s)",
						name, signalFiles(), dir, knownPlatforms())
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
					chosen, err := choosePlan(cmd.Context(), client, org)
					if err != nil {
						return err
					}
					plan = chosen.Slug
					origin += ", plan " + chosen.DisplayName
				}
				if err := r.Message("Creating app %s (%s)...", name, origin); err != nil {
					return err
				}
				req := portal.CreateAppRequest{Name: name, Platform: platform, Plan: plan}
				if node != "" {
					id, err := resolveNode(cmd.Context(), client, org, node)
					if err != nil {
						return err
					}
					req.NodeID = &id
				}
				if _, err := client.CreateApp(cmd.Context(), org, req); err != nil {
					return err
				}
			}

			env := map[string]string{}
			for k, v := range proj.Env {
				env[k] = v
			}
			if envFile != "" {
				fromFile, err := parseEnvFile(envFile)
				if err != nil {
					return err
				}
				for k, v := range fromFile {
					env[k] = v
				}
			}
			if len(env) > 0 {
				vars := make([]portal.EnvVar, 0, len(env))
				for _, k := range sortedKeys(env) {
					// Secrets arrive through --env-file, so nothing set here is
					// published in the app's public environment.
					vars = append(vars, portal.EnvVar{Name: k, Value: env[k], Public: false})
				}
				if err := r.Message("Applying %d environment variable(s)...", len(vars)); err != nil {
					return err
				}
				if err := client.SetEnv(cmd.Context(), org, name, vars, true); err != nil {
					return err
				}
			}

			if dockerfile != "" {
				if err := r.Message("Building %s from %s.", name, dockerfile); err != nil {
					return err
				}
			}

			if err := runBuild(cmd, app, buildArgs{
				org:        org,
				name:       name,
				dir:        dir,
				dockerfile: dockerfile,
				message:    message,
			}); err != nil {
				return err
			}

			// The build output ends on "OK" with no address. Close on the URL,
			// which is the thing the user actually wanted.
			if deployed, appErr := client.App(cmd.Context(), org, name); appErr == nil {
				if url := publicURL(deployed); url != "" {
					return r.Message("\n\u2713 %s", url)
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

// runBuild packs the project and uploads it, printing the build as it runs.
// It is a package var so a test can assert what a deploy asks to be built.
var runBuild = func(cmd *cobra.Command, app *App, a buildArgs) error {
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
	go func() {
		_, _, err := archive.Write(a.dir, pw)
		pw.CloseWithError(err) //nolint:errcheck
	}()
	in.Archive = pr

	err := app.Portal().Deploy(cmd.Context(), a.org, a.name, in, cmd.OutOrStdout())
	pr.CloseWithError(err) //nolint:errcheck
	return explainStreamError(err, "deploy of "+a.name, true)
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

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mustAbs(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// publicURL is the address to show after a deploy: a custom domain when the
// app has one, otherwise the platform address it always has.
func publicURL(a *portal.App) string {
	if len(a.CNames) > 0 {
		return "https://" + strings.TrimPrefix(strings.TrimPrefix(a.CNames[0], "https://"), "http://")
	}
	if len(a.Addresses) > 0 {
		return a.Addresses[0]
	}
	return ""
}
