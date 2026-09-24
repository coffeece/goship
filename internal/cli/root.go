package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/coffeece/goship/internal/config"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// Global carries the flags every command may read.
type Global struct {
	Org     string
	Output  string
	Yes     bool
	Verbose bool
}

func (g *Global) validate() error {
	switch g.Output {
	case render.Table, render.JSON:
		return nil
	default:
		return fmt.Errorf("unknown --output %q: use %q or %q", g.Output, render.Table, render.JSON)
	}
}

// App is the state shared by every command in the tree.
type App struct {
	Version string
	Global  Global
	Config  *config.Config
	Out     io.Writer
	// ProjectOrg is the org named by a config file in the working directory.
	ProjectOrg string
}

func (a *App) Renderer() *render.Renderer {
	return render.New(a.Out, a.Global.Output)
}

// Org resolves the organization a command runs against: --org, then whatever
// `org use` persisted. With neither, an account that belongs to exactly one
// organization has nothing to choose, so pick it rather than demand a step.
func (a *App) Org(ctx context.Context) (string, error) {
	if a.Global.Org != "" {
		return a.Global.Org, nil
	}
	// A goship.yml in the working directory says which organization this
	// project belongs to, which is more specific than whatever `org use` was
	// last pointed at.
	if a.ProjectOrg != "" {
		return a.ProjectOrg, nil
	}
	if a.Config.Org != "" {
		return a.Config.Org, nil
	}
	if orgs, err := a.Portal().Orgs(ctx); err == nil && len(orgs) == 1 {
		return orgs[0].Slug, nil
	}
	return a.Config.OrgOrError("")
}

// OrgForApp resolves the org to act on for a named app. It prefers the
// normal Org() resolution; only when nothing selects an org does it search
// every org the user belongs to for the app. A name present in more than one
// org is ambiguous and returns an error asking for --org, so a bare command
// never guesses between two apps that share a name.
func (a *App) OrgForApp(ctx context.Context, name string) (string, error) {
	if org, err := a.Org(ctx); err == nil {
		return org, nil
	}
	orgs, err := a.Portal().Orgs(ctx)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, o := range orgs {
		apps, err := a.Portal().Apps(ctx, o.Slug)
		if err != nil {
			return "", err
		}
		for _, app := range apps {
			if app.Name == name {
				matches = append(matches, o.Slug)
				break
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("app %q not found in any of your organizations", name)
	default:
		return "", fmt.Errorf("app %q exists in more than one organization (%s): pass --org to choose", name, strings.Join(matches, ", "))
	}
}

func (a *App) Portal() *portal.Client {
	return a.portalWithToken(resolveToken(a.Config.Token))
}

// anonymousPortal is for the calls that happen before there is a credential.
func (a *App) anonymousPortal() *portal.Client { return a.portalWithToken("") }

func (a *App) portalWithToken(token string) *portal.Client {
	opts := []portal.Option{portal.WithVersion(a.Version)}
	if a.Global.Verbose {
		opts = append(opts, portal.WithTrace(os.Stderr))
	}
	return portal.New(a.Config.API, token, opts...)
}

func NewRoot(version string) *cobra.Command {
	app := &App{Version: version}

	root := &cobra.Command{
		Use:           "goship",
		Short:         "Deploy and operate apps on GoShip",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := app.Global.validate(); err != nil {
				return err
			}
			app.Out = cmd.OutOrStdout()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			app.Config = cfg
			if proj, _, err := loadProject("."); err == nil {
				app.ProjectOrg = proj.Org
			}
			return nil
		},
	}

	f := root.PersistentFlags()
	f.StringVar(&app.Global.Org, "org", "", "organization slug (overrides the current org)")
	f.StringVarP(&app.Global.Output, "output", "o", render.Table, "output format: table or json")
	f.BoolVarP(&app.Global.Yes, "yes", "y", false, "answer yes to confirmations")
	f.BoolVarP(&app.Global.Verbose, "verbose", "v", false, "show everything: the full deploy and rollback log, and HTTP requests on stderr")

	root.AddCommand(
		newVersionCmd(app),
		newDeployCmd(app),
		newInitCmd(app),
		newLoginCmd(app),
		newLogoutCmd(app),
		newWhoamiCmd(app),
		newTokensCmd(app),
		newTokenCmd(app),
		newOrgCmd(app),
		newOrgsCmd(app),
		newAppsCmd(app),
		newAppCmd(app),
		newEnvCmd(app),
		newDBsCmd(app),
		newDBCmd(app),
		newDomainsCmd(app),
		newDomainCmd(app),
		newVolumesCmd(app),
		newVolumeCmd(app),
		newNodesCmd(app),
		newNodeCmd(app),
		newPlansCmd(app),
	)
	root.AddCommand(newStreamCmds(app)...)

	return root
}
