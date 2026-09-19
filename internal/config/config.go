package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const DefaultAPI = "https://goship.sh"

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

// Load reads the config file and overlays GOSHIP_* environment variables.
// A missing file is not an error.
func Load() (*Config, error) {
	c := &Config{API: DefaultAPI}

	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	default:
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
	}

	if c.API == "" {
		c.API = DefaultAPI
	}
	overlayEnv(c)
	return c, nil
}

func overlayEnv(c *Config) {
	for env, field := range map[string]*string{
		"GOSHIP_API":   &c.API,
		"GOSHIP_ORG":   &c.Org,
		"GOSHIP_TOKEN": &c.Token,
	} {
		if v := os.Getenv(env); v != "" {
			*field = v
		}
	}
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

// OrgOrError resolves the organization for a command: the --org flag wins,
// then whatever `goship org use` persisted.
func (c *Config) OrgOrError(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if c.Org != "" {
		return c.Org, nil
	}
	return "", errors.New("no organization selected: run `goship org use <slug>` or pass --org")
}

// ForgetOrgUnlessMember clears the selected organization when the signed-in
// identity does not belong to it, returning what was dropped. A pin set under
// one account is meaningless under another.
func (c *Config) ForgetOrgUnlessMember(orgs []string) string {
	if c.Org == "" {
		return ""
	}
	for _, o := range orgs {
		if o == c.Org {
			return ""
		}
	}
	dropped := c.Org
	c.Org = ""
	return dropped
}
