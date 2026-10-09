// Package deploy holds the decisions a deploy makes — which app, which
// platform, which plan, which org — so the CLI and the MCP server make them
// the same way. Rendering stays with the caller.
package deploy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v3"
)

// ProjectFiles are the optional config names, in the order they are tried.
// Nothing requires one: every value in it can be passed as a flag or inferred.
var ProjectFiles = []string{"goship.yaml", "goship.yml", ".goship.yaml", ".goship.yml"}

// Project is the optional config in a project root. Flags win over it.
type Project struct {
	App string `yaml:"app"`
	// Org pins the organization for the project, so a repo that belongs to one
	// org does not depend on whatever `goship org use` was last pointed at.
	Org      string `yaml:"org"`
	Platform string `yaml:"platform"`
	Plan     string `yaml:"plan"`
	// Node places the app on one of the organization's own machines. A
	// node-placed app is never billed, so it needs no plan.
	Node string            `yaml:"node"`
	Env  map[string]string `yaml:"env"`
	// Dockerfile names a container file to build from instead of letting a
	// platform build the source. Mutually exclusive with platform.
	Dockerfile string `yaml:"dockerfile"`
}

// LoadProject reads the first config file that exists, returning its name so
// messages can say which one was used. A directory with none is not an error.
func LoadProject(dir string) (*Project, string, error) {
	for _, name := range ProjectFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		p := &Project{}
		if err := yaml.Unmarshal(data, p); err != nil {
			return nil, "", fmt.Errorf("parsing %s: %w", path, err)
		}
		return p, name, nil
	}
	return &Project{}, "", nil
}
