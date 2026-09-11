package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectIsOptional(t *testing.T) {
	p, err := loadProject(t.TempDir())
	if err != nil || p.App != "" {
		t.Fatalf("a missing %s must not be an error: %v", projectFile, err)
	}
}

func TestLoadProjectReadsAppAndEnv(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, projectFile), "app: api\nplatform: go\nenv:\n  LOG_LEVEL: debug\n")

	p, err := loadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.App != "api" || p.Platform != "go" || p.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("got %+v", p)
	}
}

func TestParseEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	write(t, path, "# comment\n\nexport TOKEN=abc123\nQUOTED=\"a b\"\nURL=https://a?b=c\n")

	env, err := parseEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"TOKEN":  "abc123",
		"QUOTED": "a b",
		"URL":    "https://a?b=c",
	} {
		if env[key] != want {
			t.Errorf("%s = %q, want %q", key, env[key], want)
		}
	}
	if len(env) != 3 {
		t.Errorf("unexpected keys: %v", env)
	}
}

func TestParseEnvFileRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	write(t, path, "NOT_A_PAIR\n")

	if _, err := parseEnvFile(path); err == nil {
		t.Fatal("expected an error")
	}
}

// Streaming commands have nothing for the renderer to format, so asking for
// JSON has to fail rather than silently produce a stream.
func TestStreamingCommandsRejectJSONOutput(t *testing.T) {
	for _, name := range []string{"deploy", "logs", "shell", "run", "releases", "rollback"} {
		t.Run(name, func(t *testing.T) {
			isolateConfig(t)
			t.Setenv("GOSHIP_ORG", "acme")

			_, err := run(t, "", name, "--output", "json")
			if err == nil {
				t.Fatal("expected --output json to be refused")
			}
		})
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
