package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

// projectConfig is the optional `coffeece.yaml` in a project root. Every field
// is optional; flags passed to `coffeece deploy` override it.
type projectConfig struct {
	App      string            `yaml:"app"`
	Platform string            `yaml:"platform"`
	Plan     string            `yaml:"plan"`
	Pool     string            `yaml:"pool"`
	Team     string            `yaml:"team"`
	Env      map[string]string `yaml:"env"`
}

// loadProjectConfig reads coffeece.yaml from path. A missing file is not an
// error — it returns an empty config so the command can run on flags alone.
func loadProjectConfig(path string) (*projectConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &projectConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lendo %s: %w", path, err)
	}
	cfg := &projectConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("interpretando %s: %w", path, err)
	}
	return cfg, nil
}

// parseEnvFile reads a dotenv-style file: one KEY=VALUE per line, blank lines
// and `#` comments ignored, an optional leading `export `, and surrounding
// single/double quotes stripped from the value.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("lendo env-file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	env := map[string]string{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		s = strings.TrimPrefix(s, "export ")
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: esperado KEY=VALUE", path, line)
		}
		k = strings.TrimSpace(k)
		if k == "" {
			return nil, fmt.Errorf("%s:%d: chave vazia", path, line)
		}
		env[k] = unquote(strings.TrimSpace(v))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("lendo env-file %s: %w", path, err)
	}
	return env, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
