package cli

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// deployFlagHelp is shared by the three commands that take --deploy.
const deployFlagHelp = "act on the organization's deploy tokens instead of your personal ones"

// newTokensCmd is the plural lister, matching `orgs`: personal tokens belong
// to you, not to an organization. --deploy lists the organization's instead.
func newTokensCmd(app *App) *cobra.Command {
	var deploy bool
	cmd := &cobra.Command{
		Use:   "tokens",
		Short: "List your active API tokens, or the organization's deploy tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if deploy {
				org, err := app.Org(cmd.Context())
				if err != nil {
					return err
				}
				tokens, err := app.Portal().DeployTokens(cmd.Context(), org, app.Global.Project)
				if err != nil {
					return err
				}
				if app.Global.Output == render.Table {
					for i := range tokens {
						tokens[i].Project = cmp.Or(tokens[i].Project, "(all)")
						tokens[i].Environment = cmp.Or(tokens[i].Environment, "(all)")
					}
				}
				return app.Renderer().Render(tokens)
			}
			tokens, err := app.Portal().APITokens(cmd.Context())
			if err != nil {
				return err
			}
			return app.Renderer().Render(tokens)
		},
	}
	cmd.Flags().BoolVar(&deploy, "deploy", false, deployFlagHelp+"; --project narrows the list to one project's")
	return cmd
}

// pickToken resolves a name or id among tokens to one id, and says which
// names exist when it cannot.
func pickToken(arg string, tokens []portal.APIToken, lister string) (string, error) {
	var ids, names []string
	for _, t := range tokens {
		if t.ID == arg || t.Name == arg {
			ids = append(ids, t.ID)
		}
		names = append(names, t.Name)
	}
	switch len(ids) {
	case 0:
		if len(names) == 0 {
			return "", fmt.Errorf("no token %q: you have no active tokens", arg)
		}
		return "", fmt.Errorf("no token %q; you have: %s", arg, strings.Join(names, ", "))
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("%d tokens are named %q — revoke by id instead (see `%s`)", len(ids), arg, lister)
}

func newTokenCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API tokens for CI and scripts",
		Long: "API tokens are long-lived credentials. Set one as GOSHIP_TOKEN wherever\n" +
			"there is no browser to log in with.\n\n" +
			"A personal token acts as you. A deploy token (--deploy) belongs to the\n" +
			"organization and can only deploy, within the project and environment it\n" +
			"was created for: it is the one to put in a CI secret.",
	}

	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a token and print it once",
		Long: "Without --deploy the token is personal: it acts as you, everywhere you can.\n\n" +
			"With --deploy it belongs to the organization (--org, or the current one)\n" +
			"and is for a pipeline: it can read apps, deploy, roll back, restart, manage\n" +
			"env vars and run commands, and nothing else. --project and --environment\n" +
			"confine it further; leave one out and the token reaches every project, or\n" +
			"every environment. It keeps working when its creator leaves, and the\n" +
			"organization's admins can see and revoke it.",
		Example: "  goship token create github-actions --deploy --project web --environment production",
		Args:    cobra.ExactArgs(1),
	}
	var days int
	var deploy bool
	create.Flags().IntVar(&days, "expires-in-days", 90, "days until the token expires; 0 never expires")
	create.Flags().BoolVar(&deploy, "deploy", false, "create a deploy token owned by the organization, confined to --project and --environment when given")
	create.RunE = func(cmd *cobra.Command, args []string) error {
		var created any
		var token string
		if deploy {
			org, err := app.Org(cmd.Context())
			if err != nil {
				return err
			}
			// Only what was said on this command line confines the token: a
			// saved project or a goship.yaml must not silently narrow, or
			// fail to narrow, a credential.
			t, err := app.Portal().CreateDeployToken(cmd.Context(), org, args[0], app.Global.Project, app.Global.Environment, days)
			if err != nil {
				return err
			}
			created, token = t, t.Token
			fmt.Fprintf(cmd.ErrOrStderr(), "Deploy token for %s: %s, %s.\n", org,
				cmp.Or(t.Project, "every project"), cmp.Or(t.Environment, "every environment"))
		} else {
			if app.Global.Project != "" || app.Global.Environment != "" {
				return errors.New("--project and --environment confine a deploy token: add --deploy, or drop them for a personal token")
			}
			t, err := app.Portal().CreateAPIToken(cmd.Context(), args[0], days)
			if err != nil {
				return err
			}
			created, token = t, t.Token
		}
		if app.Global.Output != render.Table {
			return app.Renderer().Render(created)
		}
		// The token alone on stdout, so `export GOSHIP_TOKEN=$(goship token
		// create ci)` works; the reminder goes to stderr.
		fmt.Fprintln(cmd.OutOrStdout(), token)
		fmt.Fprintln(cmd.ErrOrStderr(), "This is the only time the token is shown. Store it now.")
		return nil
	}

	list := deprecatedList(newTokensCmd(app))
	list.Aliases = []string{"ls"}

	var revokeDeploy bool
	revoke := &cobra.Command{
		Use:     "rm <name-or-id>",
		Aliases: []string{"remove", "revoke"},
		Short:   "Revoke a token",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if revokeDeploy {
				org, err := app.Org(cmd.Context())
				if err != nil {
					return err
				}
				tokens, err := app.Portal().DeployTokens(cmd.Context(), org, app.Global.Project)
				if err != nil {
					return err
				}
				named := make([]portal.APIToken, len(tokens))
				for i, t := range tokens {
					named[i] = portal.APIToken{ID: t.ID, Name: t.Name}
				}
				id, err := pickToken(args[0], named, "goship tokens --deploy")
				if err != nil {
					return err
				}
				if err := app.Portal().RevokeDeployToken(cmd.Context(), org, id); err != nil {
					return err
				}
				return app.Renderer().Message("Deploy token %s revoked.", args[0])
			}
			tokens, err := app.Portal().APITokens(cmd.Context())
			if err != nil {
				return err
			}
			id, err := pickToken(args[0], tokens, "goship tokens")
			if err != nil {
				return err
			}
			if err := app.Portal().RevokeAPIToken(cmd.Context(), id); err != nil {
				return err
			}
			return app.Renderer().Message("Token %s revoked.", args[0])
		},
	}
	revoke.Flags().BoolVar(&revokeDeploy, "deploy", false, deployFlagHelp)

	cmd.AddCommand(create, list, revoke)
	return cmd
}
