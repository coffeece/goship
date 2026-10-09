package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/deploy"
)

func TestDetectPlatform(t *testing.T) {
	for _, tc := range []struct {
		name          string
		files         []string
		want, wantWhy string
	}{
		{"go", []string{"go.mod", "main.go"}, "go", "go.mod"},
		{"python via pyproject", []string{"pyproject.toml"}, "python", "pyproject.toml"},
		{"python via requirements", []string{"requirements.txt"}, "python", "requirements.txt"},
		{"nodejs", []string{"package.json", "index.js"}, "nodejs", "package.json"},
		{"static", []string{"index.html", "style.css"}, "static", "index.html"},

		// A Go service that builds frontend assets is still a Go service, and a
		// Node app that serves a page is still a Node app. Precedence decides.
		{"go wins over node", []string{"go.mod", "package.json"}, "go", "go.mod"},
		{"node wins over static", []string{"package.json", "index.html"}, "nodejs", "package.json"},

		{"nothing recognisable", []string{"README.md"}, "", ""},
		{"empty directory", nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				write(t, filepath.Join(dir, f), "")
			}

			got, why := deploy.DetectPlatform(dir)
			if got != tc.want || why != tc.wantWhy {
				t.Errorf("detectPlatform = (%q, %q), want (%q, %q)", got, why, tc.want, tc.wantWhy)
			}
		})
	}
}

// A directory named go.mod would otherwise be read as a Go project.
func TestDetectPlatformIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "go.mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := deploy.DetectPlatform(dir); got != "" {
		t.Errorf("got %q, want no detection", got)
	}
}

func TestErrorTextNamesWhatToPass(t *testing.T) {
	for _, want := range []string{"go", "python", "nodejs", "static"} {
		if !strings.Contains(deploy.KnownPlatforms(), want) {
			t.Errorf("deploy.KnownPlatforms() = %q, missing %q", deploy.KnownPlatforms(), want)
		}
	}
	if !strings.Contains(deploy.SignalFiles(), "go.mod") || !strings.Contains(deploy.SignalFiles(), "package.json") {
		t.Errorf("deploy.SignalFiles() = %q", deploy.SignalFiles())
	}
}

func TestDetectDockerfile(t *testing.T) {
	for _, tc := range []struct{ name, file, want string }{
		{"Dockerfile", "Dockerfile", "Dockerfile"},
		{"lowercase", "dockerfile", "dockerfile"},
		{"podman's name", "Containerfile", "Containerfile"},
		{"nothing", "README.md", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, tc.file), "")
			if got := deploy.DetectDockerfile(dir); got != tc.want {
				t.Errorf("detectDockerfile = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectDockerfileIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Dockerfile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := deploy.DetectDockerfile(dir); got != "" {
		t.Errorf("got %q, want no detection", got)
	}
}
