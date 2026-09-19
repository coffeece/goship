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
	if c.API != DefaultAPI {
		t.Errorf("got API=%q, want the default", c.API)
	}
}

func TestEnvOverridesTheFile(t *testing.T) {
	tempConfig(t)
	if err := (&Config{API: "https://file.example", Org: "from-file"}).Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOSHIP_ORG", "from-env")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Org != "from-env" {
		t.Errorf("Org = %q, want the environment value", c.Org)
	}
	if c.API != "https://file.example" {
		t.Errorf("API = %q, want the file value to survive", c.API)
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

func TestOrgResolution(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		got, err := (&Config{Org: "persisted"}).OrgOrError("from-flag")
		if err != nil || got != "from-flag" {
			t.Errorf("got %q, %v", got, err)
		}
	})

	t.Run("falls back to the persisted org", func(t *testing.T) {
		got, err := (&Config{Org: "persisted"}).OrgOrError("")
		if err != nil || got != "persisted" {
			t.Errorf("got %q, %v", got, err)
		}
	})

	t.Run("neither is an error naming the fix", func(t *testing.T) {
		_, err := (&Config{}).OrgOrError("")
		if err == nil {
			t.Fatal("expected an error")
		}
		if want := "goship org use"; !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should point at %q", err, want)
		}
	})
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
