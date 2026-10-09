package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

			// The project file may pin the org; it beats the persisted
			// selection, and an unselected org is left for Run to answer
			// from where the app already lives.
			proj, _, err := deploy.LoadProject(dir)
			if err != nil {
				return err
			}
			if proj.Org != "" {
				app.Global.Org = firstNonEmpty(app.Global.Org, proj.Org)
			}
			org, orgErr := app.Org(cmd.Context())

			var env map[string]string
			if envFile != "" {
				if env, err = deploy.ParseEnvFile(envFile); err != nil {
					return err
				}
			}

			name := appName
			res, err := deploy.Run(cmd.Context(), app.Portal(), deploy.Options{
				Dir:        dir,
				App:        name,
				Org:        org,
				Platform:   platform,
				Plan:       plan,
				Node:       node,
				Dockerfile: dockerfile,
				Message:    message,
				Env:        env,
			}, p)
			switch {
			case errors.Is(err, deploy.ErrNoOrg):
				return orgErr
			case err != nil:
				p.Finish(err)
				// Only a release that ran has a log to point at.
				ran := isOperationError(err)
				shown := name
				if res != nil {
					shown = res.App
				}
				err = explainStreamError(err, "deploy of "+shown, true)
				if ran && !app.Global.Verbose {
					err = fmt.Errorf("%w\nFull log: goship deploy --verbose  \u00b7  history: goship releases -a %s", err, shown)
				}
				return err
			}
			p.Finish(nil)
			if res.URL != "" {
				p.Summary(res.URL)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (default: the directory name)")
	f.StringVar(&platform, "platform", "", "platform for a new app (default: inferred from the files present)")
	f.StringVar(&dockerfile, "dockerfile", "", "build from this container file instead of a platform")
	f.StringVar(&node, "node", "", "place a new app on one of your own machines by name (never billed), or goship for GoShip's servers (default: the org's default placement)")
	f.StringVar(&plan, "plan", "", "plan, used when creating the app")
	f.StringVar(&envFile, "env-file", "", "dotenv file applied as private variables")
	f.StringVarP(&message, "message", "m", "", "deploy message")

	return cmd
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
