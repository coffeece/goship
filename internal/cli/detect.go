package cli

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

// detectPlatform guesses the platform from the files in dir, returning the
// platform and the file that gave it away. Empty when nothing matches.
func detectPlatform(dir string) (platform, evidence string) {
	for _, s := range platformSignals {
		if info, err := os.Stat(filepath.Join(dir, s.file)); err == nil && !info.IsDir() {
			return s.platform, s.file
		}
	}
	return "", ""
}

func knownPlatforms() string {
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

func signalFiles() string {
	files := make([]string, 0, len(platformSignals))
	for _, s := range platformSignals {
		files = append(files, s.file)
	}
	return strings.Join(files, ", ")
}
