package deploy

import (
	"os"
	"path/filepath"
	"strings"
)

// platformSignals maps a marker file to the platform it implies, in precedence
// order. Order is what makes a Go service with a package.json for its frontend
// assets read as go rather than nodejs, and static the fallback rather than a
// competitor — index.html shows up in plenty of projects that are not sites.
var platformSignals = []struct{ file, platform string }{
	{"go.mod", "go"},
	{"pyproject.toml", "python"},
	{"requirements.txt", "python"},
	{"Pipfile", "python"},
	{"setup.py", "python"},
	{"package.json", "nodejs"},
	{"index.html", "static"},
}

// DetectPlatform guesses the platform from the files in dir, returning the
// platform and the file that gave it away. Empty when nothing matches.
func DetectPlatform(dir string) (platform, evidence string) {
	for _, s := range platformSignals {
		if info, err := os.Stat(filepath.Join(dir, s.file)); err == nil && !info.IsDir() {
			return s.platform, s.file
		}
	}
	return "", ""
}

func KnownPlatforms() string {
	seen := map[string]bool{}
	var names []string
	for _, s := range platformSignals {
		if !seen[s.platform] {
			seen[s.platform] = true
			names = append(names, s.platform)
		}
	}
	return strings.Join(names, ", ")
}

func SignalFiles() string {
	files := make([]string, 0, len(platformSignals))
	for _, s := range platformSignals {
		files = append(files, s.file)
	}
	return strings.Join(files, ", ")
}

// dockerfileNames are the container build recipes we recognise, in the order
// docker itself would.
var dockerfileNames = []string{"Dockerfile", "dockerfile", "Containerfile"}

// DetectDockerfile reports the container file in dir, if any. It is the
// fallback when no platform marker matches: a project carrying both a go.mod
// and a Dockerfile is a Go app that happens to ship one, and is built by the
// platform unless --dockerfile says otherwise.
func DetectDockerfile(dir string) string {
	for _, name := range dockerfileNames {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return name
		}
	}
	return ""
}
