// Package mcphost serves the MCP tools over Streamable HTTP for clients that
// cannot run `goship mcp` on the user's machine, such as a claude.ai
// connector. It holds no credential of its own: each request carries the
// user's GoShip access token, which is checked against the portal's signing
// keys and then passed through to the API.
package mcphost

import (
	"cmp"
	"fmt"
	"net/url"
	"strings"
)

// Config is where the server listens and what it trusts.
type Config struct {
	// Addr is the listen address, ":" + PORT.
	Addr string
	// APIURL is the GoShip API the tools call with the caller's token.
	APIURL string
	// ResourceURL is this server's MCP endpoint as clients reach it. It is the
	// OAuth resource identifier, so it must match the URL the user adds.
	ResourceURL string
	// Issuer is the authorization server whose tokens are accepted.
	Issuer string
}

// ConfigFromEnv reads the configuration, defaulting to production.
func ConfigFromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		Addr:        ":" + cmp.Or(getenv("PORT"), "8888"),
		APIURL:      strings.TrimRight(cmp.Or(getenv("GOSHIP_API_URL"), "https://api.goship.sh"), "/"),
		ResourceURL: cmp.Or(getenv("GOSHIP_MCP_URL"), "https://mcp.goship.sh/mcp"),
		Issuer:      strings.TrimRight(cmp.Or(getenv("GOSHIP_ISSUER"), "https://goship.sh"), "/"),
	}
	for name, v := range map[string]string{"GOSHIP_API_URL": c.APIURL, "GOSHIP_MCP_URL": c.ResourceURL, "GOSHIP_ISSUER": c.Issuer} {
		if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return Config{}, fmt.Errorf("%s must be an absolute http(s) URL, got %q", name, v)
		}
	}
	return c, nil
}

// JWKSURL is where the issuer publishes its signing keys.
func (c Config) JWKSURL() string { return c.Issuer + "/.well-known/jwks.json" }

// mcpPath is the path part of ResourceURL, where the MCP handler is mounted.
func (c Config) mcpPath() string {
	u, _ := url.Parse(c.ResourceURL)
	return cmp.Or(u.Path, "/")
}

// ResourceMetadataURL is the RFC 9728 document for ResourceURL, in the
// path-suffixed form clients try first.
func (c Config) ResourceMetadataURL() string {
	u, _ := url.Parse(c.ResourceURL)
	return u.Scheme + "://" + u.Host + "/.well-known/oauth-protected-resource" + strings.TrimRight(u.Path, "/")
}
