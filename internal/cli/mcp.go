package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/coffeece/goship/internal/mcptools"
	"github.com/coffeece/goship/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve GoShip to an AI agent over MCP (stdio)",
		Long: "Runs a Model Context Protocol server on stdin/stdout that exposes everything\n" +
			"this CLI can do as tools, using the login or GOSHIP_TOKEN this machine has.\n\n" +
			"Claude Code:  claude mcp add goship -- goship mcp\n" +
			"Other clients: run \"goship mcp\" as a stdio server.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The protocol owns stdout; anything else would corrupt it.
			t := &mcp.IOTransport{Reader: io.NopCloser(cmd.InOrStdin()), Writer: nopWriteCloser{cmd.OutOrStdout()}}
			if cmd.InOrStdin() == os.Stdin {
				t = &mcp.IOTransport{Reader: os.Stdin, Writer: os.Stdout}
			}
			ctx, cancel := interruptible(cmd.Context())
			defer cancel()
			return mcptools.New(localClients{app}, app.Version, mcptools.Options{Deploy: true}).Run(ctx, t)
		},
	}
}

// localClients serves the MCP tools from this machine's config, the way the
// other commands run.
type localClients struct{ app *App }

func (l localClients) Client(context.Context) (*portal.Client, error) {
	if l.app.Config.Credential() == "" {
		return nil, errors.New(`not logged in: ask the user to run "goship login" in a terminal, or set GOSHIP_TOKEN for this server`)
	}
	return l.app.Portal(), nil
}

func (l localClients) Org(ctx context.Context, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	org, err := l.app.Org(ctx)
	if errors.Is(err, errNoOrg) {
		return "", fmt.Errorf(`no organization selected: pass org, or ask the user to run "goship org use <slug>"; goship_list_orgs shows them`)
	}
	return org, err
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
