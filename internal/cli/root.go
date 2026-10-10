package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/coffeece/goship/internal/config"
	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
)

// Global carries the flags every command may read.
type Global struct {
	Org     string
	Project string
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
	// ProjectProject is the project named by that same file.
	ProjectProject string
}

// Target is the organization and project a project-scoped command runs
// against.
type Target = deploy.Target

func (a *App) Renderer() *render.Renderer {
	return render.New(a.Out, a.Global.Output)
}

// errNoOrg is what a command that acts on one organization says when nothing
// chooses it.
var errNoOrg = errors.New("no organization selected: run `goship org use <slug>` or pass --org")

// selectedOrg is the organization something chose: --org, then a goship.yml
// in the working directory — more specific than whatever `org use` was last
// pointed at — then GOSHIP_ORG or the persisted selection.
func (a *App) selectedOrg() string {
	return cmp.Or(a.Global.Org, a.ProjectOrg, a.Config.SelectedOrg())
}

// Org resolves the organization a command runs against. With nothing
// selected, an account that belongs to exactly one organization has nothing
// to choose, so pick it rather than demand a step.
func (a *App) Org(ctx context.Context) (string, error) {
	if org := a.selectedOrg(); org != "" {
		return org, nil
	}
	if orgs, err := a.Portal().Orgs(ctx); err == nil && len(orgs) == 1 {
		return orgs[0].Slug, nil
	}
	return "", errNoOrg
}

// errNoProject is what a project-scoped command says when its organization
// has several projects and nothing chooses one.
var errNoProject = errors.New("no project selected: run `goship project use <slug>`, pass --project, or set project: in goship.yaml")

// selectedProject is the project something chose: --project, then a
// goship.yml in the working directory, then GOSHIP_PROJECT or the persisted
// selection for org. A file that pins a different org says nothing about this
// one.
func (a *App) selectedProject(org string) string {
	return cmp.Or(a.explicitProject(org), a.Config.SelectedProject(org))
}

// explicitProject is the part of selectedProject that was said for this
// command: --project or the working directory's goship.yml.
func (a *App) explicitProject(org string) string {
	fromFile := a.ProjectProject
	if a.ProjectOrg != "" && a.ProjectOrg != org {
		fromFile = ""
	}
	return cmp.Or(a.Global.Project, fromFile)
}

// Target resolves the organization and project a command runs against. An
// org with one reachable project has nothing to choose. When only the project
// is missing, the returned Target still names the org.
func (a *App) Target(ctx context.Context) (Target, error) {
	org, err := a.Org(ctx)
	if err != nil {
		return Target{}, err
	}
	project, err := a.projectIn(ctx, org)
	return Target{Org: org, Project: project}, err
}

// projectIn is the project to act on in org: the selected one, else the only
// one the user can reach.
func (a *App) projectIn(ctx context.Context, org string) (string, error) {
	if p := a.selectedProject(org); p != "" {
		return p, nil
	}
	projects, err := a.Portal().Projects(ctx, org)
	if err != nil {
		return "", err
	}
	if len(projects) == 1 {
		return projects[0].Slug, nil
	}
	return "", errNoProject
}

// TargetForApp is Target for a named app: with nothing selected it looks for
// the app in every project the user can reach. A name present in more than
// one is ambiguous and returns an error asking for --org and --project, so a
// bare command never guesses between two apps that share a name.
func (a *App) TargetForApp(ctx context.Context, name string) (Target, error) {
	tg, err := a.Target(ctx)
	if err == nil {
		return tg, nil
	}
	if !errors.Is(err, errNoOrg) && !errors.Is(err, errNoProject) {
		return Target{}, err
	}
	matches, err := deploy.FindApp(ctx, a.Portal(), tg.Org, name)
	if err != nil {
		return Target{}, err
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		if tg.Org != "" {
			return Target{}, fmt.Errorf("app %q not found in any project of %s that you can reach", name, tg.Org)
		}
		return Target{}, fmt.Errorf("app %q not found in any of your projects", name)
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.String()
		}
		return Target{}, fmt.Errorf("app %q exists in more than one project (%s): pass --org and --project to choose", name, strings.Join(names, ", "))
	}
}

// orgRunE adapts a command body that needs the resolved organization.
func orgRunE(app *App, fn func(cmd *cobra.Command, org string, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		org, err := app.Org(cmd.Context())
		if err != nil {
			return err
		}
		return fn(cmd, org, args)
	}
}

// targetRunE adapts a command body that acts on one project.
func targetRunE(app *App, fn func(cmd *cobra.Command, t Target, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		t, err := app.Target(cmd.Context())
		if err != nil {
			return err
		}
		return fn(cmd, t, args)
	}
}

func (a *App) Portal() *portal.Client {
	return a.portalWithToken(a.Config.Credential())
}

// anonymousPortal is for the calls that happen before there is a credential.
func (a *App) anonymousPortal() *portal.Client { return a.portalWithToken("") }

func (a *App) portalWithToken(token string) *portal.Client {
	opts := []portal.Option{portal.WithVersion(a.Version)}
	if a.Global.Verbose {
		opts = append(opts, portal.WithTrace(os.Stderr))
	}
	return portal.New(a.Config.Endpoint(), token, opts...)
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
			if proj, _, err := deploy.LoadProject("."); err == nil {
				app.ProjectOrg, app.ProjectProject = proj.Org, proj.ProjectSlug
			}
			return nil
		},
	}

	f := root.PersistentFlags()
	f.StringVar(&app.Global.Org, "org", "", "organization slug (overrides the current org)")
	f.StringVar(&app.Global.Project, "project", "", "project slug (overrides the current project)")
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
		newProjectCmd(app),
		newProjectsCmd(app),
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
		newCloudCmd(app),
		newMCPCmd(app),
	)
	root.AddCommand(newStreamCmds(app)...)

	return root
}
