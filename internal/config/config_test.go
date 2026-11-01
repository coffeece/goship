package config

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("GOSHIP_CONFIG", path)
	return path
}

func TestLoadWithoutAFileReturnsDefaults(t *testing.T) {
	tempConfig(t)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Endpoint(); got != DefaultAPI {
		t.Errorf("got API=%q, want the default", got)
	}
}

// The API moved to a host of its own. A config written when it still lived on
// the dashboard's host follows it; anything else a person chose is left alone.
func TestLoadMovesTheOldDefaultAPIAlong(t *testing.T) {
	tempConfig(t)
	if err := (&Config{API: "https://goship.sh", Org: "acme"}).Save(); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Endpoint() != DefaultAPI || c.Org != "acme" {
		t.Errorf("API=%q Org=%q", c.Endpoint(), c.Org)
	}

	if err := (&Config{API: "https://staging.example"}).Save(); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(); c.Endpoint() != "https://staging.example" {
		t.Errorf("a chosen API was overwritten: %q", c.Endpoint())
	}

	t.Setenv("GOSHIP_API", "https://goship.sh")
	if c, _ = Load(); c.Endpoint() != "https://goship.sh" {
		t.Errorf("GOSHIP_API was overridden: %q", c.Endpoint())
	}
}

func TestEnvOverridesTheFile(t *testing.T) {
	tempConfig(t)
	if err := (&Config{API: "https://file.example", Org: "from-file", Token: "stored"}).Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSHIP_ORG", "from-env")
	t.Setenv("GOSHIP_TOKEN", "gsp_from_env")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SelectedOrg(); got != "from-env" {
		t.Errorf("SelectedOrg = %q, want the environment value", got)
	}
	if got := c.Credential(); got != "gsp_from_env" {
		t.Errorf("Credential = %q, want GOSHIP_TOKEN to win", got)
	}
	if got := c.Endpoint(); got != "https://file.example" {
		t.Errorf("Endpoint = %q, want the file value to survive", got)
	}
}

// An override is for the process that has it. Saving must write what the file
// said, or a CI job's GOSHIP_TOKEN ends up on disk for the next user.
func TestSaveNeverWritesTheEnvironment(t *testing.T) {
	path := tempConfig(t)
	if err := (&Config{Org: "from-file"}).Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSHIP_API", "https://env.example")
	t.Setenv("GOSHIP_ORG", "from-env")
	t.Setenv("GOSHIP_TOKEN", "gsp_ci_secret")
	t.Setenv("GOSHIP_ENVIRONMENT", "env-only-staging")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"gsp_ci_secret", "env.example", "from-env", "env-only-staging"} {
		if strings.Contains(string(data), leaked) {
			t.Errorf("config file holds %q from the environment:\n%s", leaked, data)
		}
	}
}

func TestSaveKeepsTheTokenOwnerOnly(t *testing.T) {
	path := tempConfig(t)
	if err := (&Config{Token: "secret"}).Save(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600 — the file holds a bearer token", perm)
	}
}

// The selected org lives in the config, not in the session, so signing in as
// someone else leaves a pin they may have no access to — and every org-scoped
// call then answers "forbidden" for no visible reason.
func TestForgetOrgUnlessMember(t *testing.T) {
	t.Run("keeps an org the identity belongs to", func(t *testing.T) {
		c := &Config{Org: "acme"}
		if dropped := c.ForgetOrgUnlessMember([]string{"acme", "other"}); dropped != "" || c.Org != "acme" {
			t.Errorf("dropped %q, org now %q", dropped, c.Org)
		}
	})

	t.Run("clears one they do not", func(t *testing.T) {
		c := &Config{Org: "acme"}
		if dropped := c.ForgetOrgUnlessMember([]string{"other"}); dropped != "acme" || c.Org != "" {
			t.Errorf("dropped %q, org now %q", dropped, c.Org)
		}
	})

	t.Run("no pin is nothing to drop", func(t *testing.T) {
		c := &Config{}
		if dropped := c.ForgetOrgUnlessMember(nil); dropped != "" {
			t.Errorf("dropped %q", dropped)
		}
	})
}

func TestSelectedProjectEnvWinsOverTheFile(t *testing.T) {
	tempConfig(t)
	c := &Config{Projects: map[string]string{"acme": "clave", "other": "mova"}}
	if got := c.SelectedProject("acme"); got != "clave" {
		t.Errorf("SelectedProject = %q, want the file value", got)
	}
	t.Setenv("GOSHIP_PROJECT", "from-env")
	if got := c.SelectedProject("acme"); got != "from-env" {
		t.Errorf("SelectedProject = %q, want the environment value", got)
	}
}

func TestProjectsSurviveASaveAndLoad(t *testing.T) {
	tempConfig(t)
	if err := (&Config{Projects: map[string]string{"acme": "clave"}}).Save(); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SelectedProject("acme"); got != "clave" || c.SelectedProject("other") != "" {
		t.Errorf("Projects = %v", c.Projects)
	}
}

func TestSelectedEnvironmentEnvWinsOverTheFile(t *testing.T) {
	tempConfig(t)
	t.Setenv("GOSHIP_ENVIRONMENT", "")
	c := &Config{Environments: map[string]string{"acme/clave": "staging"}}
	if got := c.SelectedEnvironment("acme", "clave"); got != "staging" {
		t.Errorf("SelectedEnvironment = %q, want the file value", got)
	}
	if got := c.SelectedEnvironment("acme", "mova"); got != "" {
		t.Errorf("SelectedEnvironment(acme, mova) = %q, want none: the choice is per project", got)
	}
	t.Setenv("GOSHIP_ENVIRONMENT", "from-env")
	if got := c.SelectedEnvironment("acme", "clave"); got != "from-env" {
		t.Errorf("SelectedEnvironment = %q, want the environment value", got)
	}
}

func TestEnvironmentsSurviveASaveAndLoad(t *testing.T) {
	tempConfig(t)
	t.Setenv("GOSHIP_ENVIRONMENT", "")
	c := &Config{}
	c.UseEnvironment("acme", "clave", "staging")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SelectedEnvironment("acme", "clave"); got != "staging" || c.Environments["acme/clave"] != "staging" {
		t.Errorf("Environments = %v", c.Environments)
	}
}

// A removed environment is forgotten wherever it was selected in its own
// org, and nowhere else.
func TestForgetEnvironmentDropsOnlyThatOrgsSelections(t *testing.T) {
	c := &Config{Environments: map[string]string{
		"acme/clave":    "staging",
		"acme/mova":     "staging",
		"acme/web":      "development",
		"acme-labs/web": "staging",
		"globex/clave":  "staging",
	}}
	if !c.ForgetEnvironment("acme", "staging") {
		t.Fatal("ForgetEnvironment = false, want true: two selections were dropped")
	}
	want := map[string]string{"acme/web": "development", "acme-labs/web": "staging", "globex/clave": "staging"}
	if !maps.Equal(c.Environments, want) {
		t.Errorf("Environments = %v, want %v", c.Environments, want)
	}
	if c.ForgetEnvironment("acme", "staging") {
		t.Error("ForgetEnvironment = true with nothing left to drop")
	}
}

// A dropped org takes its environment choices along, and only its own: an
// org whose slug merely starts the same keeps them.
func TestForgetOrgUnlessMemberDropsTheOrgsEnvironments(t *testing.T) {
	c := &Config{
		Org:          "acme",
		Projects:     map[string]string{"acme": "clave", "acme-labs": "web"},
		Environments: map[string]string{"acme/clave": "staging", "acme/mova": "dev", "acme-labs/web": "qa"},
	}
	if dropped := c.ForgetOrgUnlessMember([]string{"acme-labs"}); dropped != "acme" {
		t.Fatalf("dropped %q", dropped)
	}
	if len(c.Environments) != 1 || c.Environments["acme-labs/web"] != "qa" {
		t.Errorf("Environments = %v, want only acme-labs/web", c.Environments)
	}
	if len(c.Projects) != 1 || c.Projects["acme-labs"] != "web" {
		t.Errorf("Projects = %v", c.Projects)
	}
}
