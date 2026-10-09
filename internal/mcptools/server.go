// Package mcptools exposes everything the CLI can do as MCP tools, for the
// local `goship mcp` server and the hosted one alike. Only the way a client
// and an organization are found differs between the two, and Clients hides
// that.
package mcptools

import (
	"context"
	"fmt"

	"github.com/coffeece/goship/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Clients gives each tool call its API client and organization. The local
// server reads them from the config file; the hosted one from the request.
type Clients interface {
	Client(ctx context.Context) (*portal.Client, error)
	// Org resolves the organization for a call: override when the tool was
	// given one, else whatever the caller's context selects. The error says
	// how to select one.
	Org(ctx context.Context, override string) (string, error)
}

// Options selects what the server offers.
type Options struct {
	// Deploy adds goship_deploy, which reads a directory on the machine the
	// server runs on. Only the local server can honour it.
	Deploy bool
}

// New builds a server with every tool registered.
func New(c Clients, version string, o Options) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "goship", Title: "GoShip", Version: version}, nil)
	Register(s, c, o)
	return s
}

// Register adds the tools to s.
func Register(s *mcp.Server, c Clients, o Options) {
	r := &registry{s: s, clients: c}
	r.identity()
	r.apps()
	r.env()
	r.releases(o.Deploy)
	r.runtime()
	r.databases()
	r.domains()
	r.nodes()
	r.volumes()
	r.plans()
	r.cloud()
	r.tokens()
}

type registry struct {
	s       *mcp.Server
	clients Clients
}

// kind is what a tool does to the world, for its annotations.
type kind int

const (
	read kind = iota
	write
	destructive
)

func (k kind) annotations() *mcp.ToolAnnotations {
	f := false
	switch k {
	case read:
		return &mcp.ToolAnnotations{ReadOnlyHint: true}
	case destructive:
		return &mcp.ToolAnnotations{}
	default:
		return &mcp.ToolAnnotations{DestructiveHint: &f}
	}
}

// confirm is appended to every destructive tool's description.
const confirm = " Destructive and not undoable: confirm with the user before calling it."

// tool registers one typed tool. The handler gets the client and the
// resolved org; In must carry an Org field read through orgOf.
func tool[In orgCarrier, Out any](r *registry, name, desc string, k kind, fn func(ctx context.Context, c *portal.Client, org string, in In) (Out, error)) {
	if k == destructive {
		desc += confirm
	}
	mcp.AddTool(r.s, &mcp.Tool{Name: name, Description: desc, Annotations: k.annotations()},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			var zero Out
			c, err := r.clients.Client(ctx)
			if err != nil {
				return nil, zero, err
			}
			org, err := r.clients.Org(ctx, in.org())
			if err != nil {
				return nil, zero, err
			}
			out, err := fn(ctx, c, org, in)
			if err != nil {
				return nil, zero, err
			}
			return nil, out, nil
		})
}

// orgless registers a tool that acts on the account, not an organization.
func orgless[In any, Out any](r *registry, name, desc string, k kind, fn func(ctx context.Context, c *portal.Client, in In) (Out, error)) {
	if k == destructive {
		desc += confirm
	}
	mcp.AddTool(r.s, &mcp.Tool{Name: name, Description: desc, Annotations: k.annotations()},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			var zero Out
			c, err := r.clients.Client(ctx)
			if err != nil {
				return nil, zero, err
			}
			out, err := fn(ctx, c, in)
			if err != nil {
				return nil, zero, err
			}
			return nil, out, nil
		})
}

type orgCarrier interface{ org() string }

// OrgArg is the organization override every org-scoped tool accepts.
type OrgArg struct {
	Org string `json:"org,omitempty" jsonschema:"organization slug; defaults to the selected organization"`
}

func (o OrgArg) org() string { return o.Org }

// Message is the result of a call that changes something and returns nothing.
type Message struct {
	Message string `json:"message"`
}

func msgf(format string, args ...any) (Message, error) {
	return Message{Message: fmt.Sprintf(format, args...)}, nil
}
