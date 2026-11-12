package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
)

// domainRow is one line of `project domain list`: a rule of a domain, or the
// bare domain when it has none.
type domainRow struct {
	Domain      string `table:"DOMAIN"`
	Environment string `table:"ENVIRONMENT"`
	AppsGet     string `table:"APPS GET"`
	Record      string `table:"DNS RECORD"`
	DNS         string `table:"DNS"`
}

func domainRows(domains []portal.ProjectDomain) []domainRow {
	var rows []domainRow
	for _, d := range domains {
		if len(d.Environments) == 0 {
			rows = append(rows, domainRow{Domain: d.Domain})
		}
		for _, r := range d.Environments {
			row := domainRow{Domain: d.Domain, Environment: r.Environment, AppsGet: r.ExampleHost, DNS: r.DNS}
			if r.RecordName != "" && r.RecordValue != "" {
				row.Record = r.RecordName + " CNAME " + r.RecordValue
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// projectRunE adapts a command body that acts on the project as a whole, with
// no environment involved.
func projectRunE(app *App, fn func(cmd *cobra.Command, t Target, args []string) error) func(*cobra.Command, []string) error {
	return orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		project, err := app.projectIn(cmd.Context(), org)
		if err != nil {
			return err
		}
		t := Target{Org: org, Project: project}
		return app.explainProject(cmd.Context(), t, fn(cmd, t, args))
	})
}

// requireEnvironment is the --environment the rule commands insist on: the
// environment a rule applies to is never guessed.
func requireEnvironment(app *App) (string, error) {
	if app.Global.Environment == "" {
		return "", errors.New(`required flag "--environment" not set`)
	}
	return app.Global.Environment, nil
}

// appCount is "1 app" or "3 apps"; adjective goes before the noun.
func appCount(n int, adjective string) string {
	if n == 1 {
		return "1 " + adjective + "app"
	}
	return fmt.Sprintf("%d %sapps", n, adjective)
}

func newProjectDomainCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Attach domains to the project and choose which hosts new apps get",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List the project's domains, the rule of each environment and its DNS record",
		Args:  cobra.NoArgs,
		RunE: projectRunE(app, func(cmd *cobra.Command, t Target, _ []string) error {
			domains, err := app.Portal().ProjectDomains(cmd.Context(), t.Org, t.Project)
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(domains)
			}
			return app.Renderer().Render(domainRows(domains))
		}),
	}

	add := &cobra.Command{
		Use:   "add <domain>",
		Short: "Attach a domain to the project",
		Args:  cobra.ExactArgs(1),
		RunE: projectRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			d, err := app.Portal().AttachProjectDomain(cmd.Context(), t.Org, t.Project, args[0])
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(d)
			}
			return app.Renderer().Message("Attached %s to project %s. Choose what new apps get: goship project domain set %s --environment <slug>", d.Domain, t.Project, d.Domain)
		}),
	}

	rm := &cobra.Command{
		Use:   "rm <domain>",
		Short: "Detach a domain from the project",
		Args:  cobra.ExactArgs(1),
		RunE: projectRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			d, err := deploy.ResolveProjectDomain(cmd.Context(), app.Portal(), t, args[0])
			if err != nil {
				return err
			}
			if err := app.Portal().DetachProjectDomain(cmd.Context(), t.Org, t.Project, d.ID); err != nil {
				return err
			}
			return app.Renderer().Message("Detached %s from project %s.", d.Domain, t.Project)
		}),
	}

	var subdomain string
	set := &cobra.Command{
		Use:   "set <domain> --environment <slug> [--subdomain <label>]",
		Short: "Give new apps of an environment a host under the domain",
		Args:  cobra.ExactArgs(1),
		RunE: projectRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			env, err := requireEnvironment(app)
			if err != nil {
				return err
			}
			d, err := deploy.ResolveProjectDomain(cmd.Context(), app.Portal(), t, args[0])
			if err != nil {
				return err
			}
			updated, err := app.Portal().SetProjectDomainRule(cmd.Context(), t.Org, t.Project, d.ID, env, subdomain)
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(updated)
			}
			r := app.Renderer()
			for _, rule := range updated.Environments {
				if rule.Environment != env {
					continue
				}
				if err := r.Message("New apps in %s get %s.", env, rule.ExampleHost); err != nil {
					return err
				}
				if rule.RecordValue != "" {
					if err := r.Message("Create this DNS record: %s CNAME %s", rule.RecordName, rule.RecordValue); err != nil {
						return err
					}
				}
				if rule.MissingApps > 0 {
					verb := "do"
					if rule.MissingApps == 1 {
						verb = "does"
					}
					return r.Message("%s %s not have it yet: goship project domain apply %s --environment %s", appCount(rule.MissingApps, "existing "), verb, updated.Domain, env)
				}
			}
			return nil
		}),
	}
	set.Flags().StringVar(&subdomain, "subdomain", "", "label between the app name and the domain (<app>.<label>.<domain>); none when omitted")

	unset := &cobra.Command{
		Use:   "unset <domain> --environment <slug>",
		Short: "Stop giving new apps of an environment a host under the domain",
		Args:  cobra.ExactArgs(1),
		RunE: projectRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			env, err := requireEnvironment(app)
			if err != nil {
				return err
			}
			d, err := deploy.ResolveProjectDomain(cmd.Context(), app.Portal(), t, args[0])
			if err != nil {
				return err
			}
			if err := app.Portal().RemoveProjectDomainRule(cmd.Context(), t.Org, t.Project, d.ID, env); err != nil {
				return err
			}
			return app.Renderer().Message("New apps in %s no longer get a host under %s.", env, d.Domain)
		}),
	}

	apply := &cobra.Command{
		Use:   "apply <domain> --environment <slug>",
		Short: "Attach the rule's host to the environment's existing apps",
		Args:  cobra.ExactArgs(1),
		RunE: projectRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
			env, err := requireEnvironment(app)
			if err != nil {
				return err
			}
			d, err := deploy.ResolveProjectDomain(cmd.Context(), app.Portal(), t, args[0])
			if err != nil {
				return err
			}
			res, err := app.Portal().ApplyProjectDomainRule(cmd.Context(), t.Org, t.Project, d.ID, env)
			if err != nil {
				return err
			}
			if app.Global.Output == render.JSON {
				if err := app.Renderer().Render(res); err != nil {
					return err
				}
			} else {
				r := app.Renderer()
				if err := r.Message("Attached to %s.", appCount(len(res.Attached), "")); err != nil {
					return err
				}
				for _, f := range res.Failed {
					if err := r.Message("could not attach %s: %s", f.App, f.Reason); err != nil {
						return err
					}
				}
			}
			if len(res.Failed) > 0 {
				return fmt.Errorf("%s could not be attached", appCount(len(res.Failed), ""))
			}
			return nil
		}),
	}

	cmd.AddCommand(list, add, rm, set, unset, apply)
	return cmd
}
