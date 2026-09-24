package config

import (
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
	for _, leaked := range []string{"gsp_ci_secret", "env.example", "from-env"} {
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
