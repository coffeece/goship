package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
)

// initFile is what `goship init` writes. loadProject also accepts goship.yaml
// and the dotted spellings; this is the one we generate.
const initFile = "goship.yml"

func newInitCmd(app *App) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Write a " + initFile + " with what can be worked out from the directory",
		Long: "Deploy needs no configuration, so this file is a convenience: it pins the\n" +
			"values goship would otherwise infer, and gives you somewhere to keep the\n" +
			"ones it cannot guess.\n\n" +
			"Anything discoverable from the directory is filled in. Anything that is a\n" +
			"choice — the plan, the environment — is left empty with a note.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}

			if existing, err := existingConfig(dir); err != nil {
				return err
			} else if existing != "" && !force {
				return fmt.Errorf("%s already exists; pass --force to replace it", filepath.Join(dir, existing))
			}

			name := appNameFor(dir)
			platform, evidence := deploy.DetectPlatform(dir)
			var dockerfile string
			if platform == "" {
				dockerfile = deploy.DetectDockerfile(dir)
			}

			// The selected org is local state, so init needs no network for it.
			org := app.Config.SelectedOrg()
			project, projectNote := initProject(cmd.Context(), app, org)

			path := filepath.Join(dir, initFile)
			if err := os.WriteFile(path, []byte(renderProject(name, org, project, platform, dockerfile)), 0o644); err != nil { //nolint:gosec // G306: a project file, meant to be committed
				return err
			}

			r := app.Renderer()
			if err := r.Message("Wrote %s", path); err != nil {
				return err
			}
			if err := r.Message("  app:      %s (from the directory name)", name); err != nil {
				return err
			}
			if org != "" {
				if err := r.Message("  org:      %s (the one you have selected)", org); err != nil {
					return err
				}
			} else if err := r.Message("  org:      left empty — set it, or pick one with `goship org use`"); err != nil {
				return err
			}
			if project != "" {
				if err := r.Message("  project:  %s (the one you have selected)", project); err != nil {
					return err
				}
			} else if projectNote != "" {
				if err := r.Message("  project:  %s", projectNote); err != nil {
					return err
				}
			}
			switch {
			case platform != "":
				return r.Message("  platform: %s (detected from %s)", platform, evidence)
			case dockerfile != "":
				return r.Message("  build:    %s (no platform needed — your container file builds the image)", dockerfile)
			default:
				return r.Message("  platform: left empty — no %s or Dockerfile here, so set it yourself (%s)",
					deploy.SignalFiles(), deploy.KnownPlatforms())
			}
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing config file")

	return cmd
}

// initProject is the project to pin: the one selected for org, unless org has
// only one, in which case there is nothing to choose. The count needs the
// network, so a machine that is signed out pins what it has selected and asks
// nothing. Once the list is known, a selection that is not in it (a stale
// config, a typo in GOSHIP_PROJECT) is not pinned. note says why the project
// was left empty.
func initProject(ctx context.Context, app *App, org string) (project, note string) {
	if org == "" {
		return "", ""
	}
	project = app.selectedProject(org)
	if app.Config.Credential() == "" {
		return project, ""
	}
	projects, err := app.Portal().Projects(ctx, org)
	switch {
	case err != nil:
		return project, ""
	case len(projects) == 0:
		return "", "left empty — you cannot reach any project in " + org
	case len(projects) == 1:
		return "", ""
	case slices.ContainsFunc(projects, func(p portal.Project) bool { return p.Slug == project }):
		return project, ""
	}
	return "", "left empty — " + org + " has several projects, pick one with `goship project use`"
}

func existingConfig(dir string) (string, error) {
	for _, name := range projectFiles {
		_, err := os.Stat(filepath.Join(dir, name))
		switch {
		case err == nil:
			return name, nil
		case errors.Is(err, fs.ErrNotExist):
		default:
			return "", err
		}
	}
	return "", nil
}

var notNameChar = regexp.MustCompile(`[^a-z0-9-]+`)

// appNameFor turns a directory name into something the platform accepts:
// lowercase, alphanumeric and dashes, starting with a letter.
func appNameFor(dir string) string {
	base := strings.ToLower(filepath.Base(mustAbs(dir)))
	name := strings.Trim(notNameChar.ReplaceAllString(base, "-"), "-")
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "app-" + name
	}
	return strings.Trim(name, "-")
}

func renderProject(name, org, project, platform, dockerfile string) string {
	orgLine := strings.TrimRight("org: "+org, " ")
	projectLine := strings.TrimRight("project: "+project, " ")
	build := strings.TrimRight("platform: "+platform, " ")
	if platform == "" && dockerfile != "" {
		build = "# No platform: the container file below builds the image.\nplatform:\ndockerfile: " + dockerfile
	}
	return fmt.Sprintf(`# goship.yml — optional. Every value here can also be passed as a flag.
# Flags beat this file; this file beats whatever goship works out on its own.

app: %s

# Organization this project belongs to. Set here, it beats whatever
# "goship org use" last selected, so a repo always deploys to the same place.
%s

# Project inside the organization, when it has more than one. Set here, it beats
# whatever "goship project use" last selected.
%s

%s

# Plan for the app the first time it is created. Run "goship plans" to see what
# your organization can choose. Left empty, goship picks a free plan if you have
# one and asks otherwise.
plan:

# Place the app on one of your own machines, by name ("goship nodes").
# A node-placed app is never billed and needs no plan.
node:

# Environment variables applied on every deploy. Secrets do not belong in a file
# you commit — pass those with --env-file instead.
env:

# Everything below is read by the platform, not by goship: how to check the app
# is healthy, what to run around a build or a restart, and which processes it
# has. Uncomment what you need. https://docs.goship.sh/apps/goship-yaml/
#
# healthcheck:
#   path: /healthz
#
# hooks:
#   build:
#     - make assets
#   restart:
#     after:
#       - ./migrate.sh
#
# processes:
#   - name: web
#     command: ./server
`, name, orgLine, projectLine, build)
}
