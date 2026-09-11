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

const projectFile = "goship.yaml"

// project is the optional goship.yaml in a project root. Flags win over it.
type project struct {
	App      string            `yaml:"app"`
	Platform string            `yaml:"platform"`
	Plan     string            `yaml:"plan"`
	Env      map[string]string `yaml:"env"`
}

func loadProject(dir string) (*project, error) {
	path := filepath.Join(dir, projectFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &project{}, nil
	}
	if err != nil {
		return nil, err
	}
	p := &project{}
	if err := yaml.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return p, nil
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
		Long: "Reads " + projectFile + " from the directory being deployed. The app is\n" +
			"created on first deploy; environment variables come from the file's env:\n" +
			"block and from --env-file, which is applied as private.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Global.Output == render.JSON {
				return fmt.Errorf("deploy streams its output; --output json is not supported")
			}
			org, err := app.Org()
			if err != nil {
				return err
			}

			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			proj, err := loadProject(dir)
			if err != nil {
				return err
			}

			name := firstNonEmpty(appName, proj.App, filepath.Base(mustAbs(dir)))
			platform = firstNonEmpty(platform, proj.Platform)
			plan = firstNonEmpty(plan, proj.Plan)

			r := app.Renderer()
			client := app.Portal()

			if _, err := client.App(cmd.Context(), org, name); err != nil {
				if !portal.IsNotFound(err) {
					return err
				}
				if platform == "" {
					return fmt.Errorf("app %q does not exist yet: pass --platform or set it in %s", name, projectFile)
				}
				if err := r.Message("Creating app %s (%s)...", name, platform); err != nil {
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
	f.StringVarP(&appName, "app", "a", "", "app name (overrides "+projectFile+")")
	f.StringVar(&platform, "platform", "", "platform, used when creating the app")
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
