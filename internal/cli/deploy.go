package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/coffeece/goship/internal/tsuru"
	"github.com/spf13/cobra"
	tsuruclient "github.com/tsuru/tsuru-client/tsuru/client"
	tsurucmd "github.com/tsuru/tsuru-client/tsuru/cmd"
	yaml "gopkg.in/yaml.v3"
)

// projectFiles are the optional config names, in the order they are tried.
// Nothing requires one: every value in it can be passed as a flag or inferred.
var projectFiles = []string{"goship.yaml", "goship.yml", ".goship.yaml", ".goship.yml"}

// project is the optional config in a project root. Flags win over it.
type project struct {
	App      string            `yaml:"app"`
	Platform string            `yaml:"platform"`
	Plan     string            `yaml:"plan"`
	Env      map[string]string `yaml:"env"`
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
	var appName, platform, plan, envFile, message string

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
			proj, projFile, err := loadProject(dir)
			if err != nil {
				return err
			}

			name := firstNonEmpty(appName, proj.App, filepath.Base(mustAbs(dir)))
			plan = firstNonEmpty(plan, proj.Plan)

			r := app.Renderer()
			client := app.Portal()

			// The app name is known before the organization has to be, so an
			// unselected org is answerable: if this app exists in exactly one
			// of the caller's organizations, that is the one they meant.
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

			// Where the platform came from, so the creation line can say so: a
			// wrong guess should be visible in the output, not discovered later.
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
				if platform == "" {
					return fmt.Errorf(
						"cannot tell what %q is built with: none of %s found in %s.\nPass --platform (%s)",
						name, signalFiles(), dir, knownPlatforms())
				}
				origin := platform
				if platformSource != "" {
					origin = platform + ", " + platformSource
				}
				if plan == "" {
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
				if _, err := client.CreateApp(cmd.Context(), org, portal.CreateAppRequest{
					Name: name, Platform: platform, Plan: plan,
				}); err != nil {
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

			if err := tsuru.Setup(app.Config.Tsuru, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			defer tsuru.Flush()

			deploy := &tsuruclient.AppDeploy{}
			flags := []string{"--app", name}
			if message != "" {
				flags = append(flags, "--message", message)
			}
			if err := deploy.Flags().Parse(flags); err != nil {
				return err
			}
			err = tsuru.Run(deploy, []string{dir}, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
			if errors.Is(err, tsurucmd.ErrAbortCommand) {
				return fmt.Errorf("deploy of %q failed", name)
			}
			return err
		},
	}

	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (default: the directory name)")
	f.StringVar(&platform, "platform", "", "platform for a new app (default: inferred from the files present)")
	f.StringVar(&plan, "plan", "", "plan, used when creating the app")
	f.StringVar(&envFile, "env-file", "", "dotenv file applied as private variables")
	f.StringVarP(&message, "message", "m", "", "deploy message")

	return cmd
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
