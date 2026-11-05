// Package mcpserver is GoShip's MCP tool server for a host that serves many
// people at once, such as the hosted server behind https://mcp.goship.sh/mcp.
// Nothing is read from a machine or selected beforehand: each tool call
// reaches the GoShip API with the token of the person who made it, which the
// host puts in the request's context with WithToken after checking it.
//
// The tools are the ones `goship mcp` serves locally, without goship_deploy's
// directory upload (a host has no directory of the caller's to read).
package mcpserver

import (
	"context"

	"github.com/coffeece/goship/internal/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// New builds the server. Building one costs ~30ms, so a host builds it once
// and shares it across requests and people: the credential travels in each
// request's context, never in the server.
func New(apiURL, version string) *mcp.Server {
	return mcptools.New(tokenClients{api: apiURL, version: version}, version, mcptools.Options{})
}

// WithToken returns ctx carrying the GoShip token of the person a request is
// from. A tool call whose context has none fails, asking to reconnect.
func WithToken(ctx context.Context, token string) context.Context {
	return withToken(ctx, token)
}
