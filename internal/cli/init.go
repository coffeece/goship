package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
			platform, evidence := detectPlatform(dir)
			var dockerfile string
			if platform == "" {
				dockerfile = detectDockerfile(dir)
			}

			path := filepath.Join(dir, initFile)
			if err := os.WriteFile(path, []byte(renderProject(name, platform, dockerfile)), 0o644); err != nil {
				return err
			}

			r := app.Renderer()
			if err := r.Message("Wrote %s", path); err != nil {
				return err
			}
			if err := r.Message("  app:      %s (from the directory name)", name); err != nil {
				return err
			}
			switch {
			case platform != "":
				return r.Message("  platform: %s (detected from %s)", platform, evidence)
			case dockerfile != "":
				return r.Message("  build:    %s (no platform needed — your container file builds the image)", dockerfile)
			default:
				return r.Message("  platform: left empty — no %s or Dockerfile here, so set it yourself (%s)",
					signalFiles(), knownPlatforms())
			}
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing config file")

	return cmd
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
	if name == "" || !(name[0] >= 'a' && name[0] <= 'z') {
		name = "app-" + name
	}
	return strings.Trim(name, "-")
}

func renderProject(name, platform, dockerfile string) string {
	build := "platform: " + platform
	if platform == "" && dockerfile != "" {
		build = "# No platform: the container file below builds the image.\nplatform:\ndockerfile: " + dockerfile
	}
	return fmt.Sprintf(`# goship.yml — optional. Every value here can also be passed as a flag.
# Flags beat this file; this file beats whatever goship works out on its own.

app: %s
%s

# Plan for the app the first time it is created. Run "goship plans" to see what
# your organization can choose. Left empty, goship picks a free plan if you have
# one and asks otherwise.
plan:

# Environment variables applied on every deploy. Secrets do not belong in a file
# you commit — pass those with --env-file instead.
env:
`, name, build)
}
