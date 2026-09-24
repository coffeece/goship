// Package config holds the CLI's per-machine state: the file `goship login`
// and `goship org use` write, and the GOSHIP_* environment variables that
// override it.
package config

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

const DefaultAPI = "https://api.goship.sh"

// legacyAPI is what DefaultAPI used to be. The dashboard still answers there,
// but the API moved to a host of its own; a config file that pinned the old
// value is moved along rather than left on a host that may stop serving it.
const legacyAPI = "https://goship.sh"

// Config is what the config file holds, and nothing else: the environment is
// read by Endpoint, SelectedOrg and Credential, never stored here, so Save
// cannot write a GOSHIP_TOKEN meant for one CI job to disk.
type Config struct {
	// API is the GoShip API, the only thing the CLI talks to.
	API string `json:"api,omitempty"`
	Org string `json:"org,omitempty"`
	// Token is the credential; TokenID identifies it to the API when it is an
	// API token minted by `goship login`, so `goship logout` can revoke it.
	Token   string `json:"token,omitempty"`
	TokenID string `json:"token_id,omitempty"`
}

// Path is the config file location: $GOSHIP_CONFIG, else under
// $XDG_CONFIG_HOME, else ~/.config.
func Path() (string, error) {
	if p := os.Getenv("GOSHIP_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "goship", "config.json"), nil
}

// Load reads the config file. A missing file is not an error.
func Load() (*Config, error) {
	c := &Config{}

	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own config location
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	default:
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	}

	if c.API == legacyAPI {
		c.API = ""
	}
	return c, nil
}

// Endpoint is the API to talk to: GOSHIP_API, else the file, else DefaultAPI.
func (c *Config) Endpoint() string {
	return cmp.Or(os.Getenv("GOSHIP_API"), c.API, DefaultAPI)
}

// SelectedOrg is GOSHIP_ORG, else whatever `goship org use` persisted.
func (c *Config) SelectedOrg() string {
	return cmp.Or(os.Getenv("GOSHIP_ORG"), c.Org)
}

// Credential is the token to send: GOSHIP_TOKEN, for CI and anywhere without
// a browser, wins over whatever `goship login` stored.
func (c *Config) Credential() string {
	return cmp.Or(os.Getenv("GOSHIP_TOKEN"), c.Token)
}

// Save writes the config file, creating its directory. It holds a bearer
// token, so the file is owner-only.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// ForgetOrgUnlessMember clears the selected organization when the signed-in
// identity does not belong to it, returning what was dropped. A pin set under
// one account is meaningless under another.
func (c *Config) ForgetOrgUnlessMember(orgs []string) string {
	if c.Org == "" || slices.Contains(orgs, c.Org) {
		return ""
	}
	dropped := c.Org
	c.Org = ""
	return dropped
}
