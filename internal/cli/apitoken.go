package cli

import (
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// newTokensCmd is the plural lister, matching `orgs`: tokens belong to you,
// not to an organization.
func newTokensCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "tokens",
		Short: "List your active API tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tokens, err := app.Portal().APITokens(cmd.Context())
			if err != nil {
				return err
			}
			return app.Renderer().Render(tokens)
		},
	}
}

func newTokenCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API tokens for CI and scripts",
		Long: "API tokens are long-lived credentials that act as you. Set one as\n" +
			"GOSHIP_TOKEN wherever there is no browser to log in with.",
	}

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a token and print it once",
		Args:  cobra.ExactArgs(1),
	}
	var days int
	create.Flags().IntVar(&days, "expires-in-days", 90, "days until the token expires; 0 never expires")
	create.RunE = func(cmd *cobra.Command, args []string) error {
		created, err := app.Portal().CreateAPIToken(cmd.Context(), args[0], days)
		if err != nil {
			return err
		}
		if app.Global.Output != render.Table {
			return app.Renderer().Render(created)
		}
		// The token alone on stdout, so `export GOSHIP_TOKEN=$(goship token
		// create ci)` works; the reminder goes to stderr.
		fmt.Fprintln(cmd.OutOrStdout(), created.Token)
		fmt.Fprintln(cmd.ErrOrStderr(), "This is the only time the token is shown. Store it now.")
		return nil
	}

	list := deprecatedList(newTokensCmd(app))
	list.Aliases = []string{"ls"}

	revoke := &cobra.Command{
		Use:     "rm <name-or-id>",
		Aliases: []string{"remove", "revoke"},
		Short:   "Revoke a token",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokens, err := app.Portal().APITokens(cmd.Context())
			if err != nil {
				return err
			}
			var ids, names []string
			for _, t := range tokens {
				if t.ID == args[0] || t.Name == args[0] {
					ids = append(ids, t.ID)
				}
				names = append(names, t.Name)
			}
			switch len(ids) {
			case 0:
				if len(names) == 0 {
					return fmt.Errorf("no token %q: you have no active tokens", args[0])
				}
				return fmt.Errorf("no token %q; you have: %s", args[0], strings.Join(names, ", "))
			case 1:
			default:
				return fmt.Errorf("%d tokens are named %q — revoke by id instead (see `goship tokens`)", len(ids), args[0])
			}
			if err := app.Portal().RevokeAPIToken(cmd.Context(), ids[0]); err != nil {
				return err
			}
			return app.Renderer().Message("Token %s revoked.", args[0])
		},
	}

	cmd.AddCommand(create, list, revoke)
	return cmd
}
