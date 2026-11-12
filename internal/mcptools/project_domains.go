package mcptools

import (
	"context"
	"strconv"
	"strings"

	"github.com/coffeece/goship/internal/deploy"
	"github.com/coffeece/goship/internal/portal"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type projectCarrier interface {
	orgCarrier
	project() string
}

// ProjectOnlyArg is the organization and project overrides of a tool that
// acts on the project as a whole, with no environment.
type ProjectOnlyArg struct {
	OrgArg
	Project string `json:"project,omitempty" jsonschema:"project slug; defaults to the selected project (goship_list_projects)"`
}

func (p ProjectOnlyArg) project() string { return p.Project }

type ProjectDomainList struct {
	Domains []portal.ProjectDomain `json:"domains"`
}

type ProjectDomainArg struct {
	ProjectOnlyArg
	Domain string `json:"domain" jsonschema:"domain name, e.g. example.com (as goship_list_project_domains shows it)"`
}

type ProjectDomainRuleArg struct {
	ProjectDomainArg
	Environment string `json:"environment" jsonschema:"environment slug the rule applies to, e.g. staging; never defaulted"`
}

type SetProjectDomainArgs struct {
	ProjectDomainRuleArg
	Subdomain string `json:"subdomain,omitempty" jsonschema:"label between the app name and the domain (<app>.<subdomain>.<domain>); omitted, apps get <app>.<domain>"`
}

type ProjectDomainResult struct {
	Message string                `json:"message"`
	Domain  *portal.ProjectDomain `json:"domain,omitempty"`
}

type ProjectDomainApplyResult struct {
	Message  string   `json:"message"`
	Attached []string `json:"attached"`
	Failed   []struct {
		App    string `json:"app"`
		Reason string `json:"reason"`
	} `json:"failed"`
}

// projectTool registers a typed tool that acts on a project but not on one
// of its environments; In must carry the organization and project overrides.
func projectTool[In projectCarrier, Out any](r *registry, name, desc string, k kind, fn func(ctx context.Context, c *portal.Client, t deploy.Target, in In) (Out, error)) {
	if k == destructive {
		desc += confirm
	}
	mcp.AddTool(r.s, &mcp.Tool{Name: name, Description: desc, Annotations: k.annotations(), InputSchema: inputSchema[In](r)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			var zero Out
			c, err := r.clients.Client(ctx)
			if err != nil {
				return nil, zero, withCode(err)
			}
			org, err := r.clients.Org(ctx, in.org())
			if err != nil {
				return nil, zero, withCode(err)
			}
			project, err := r.clients.Project(ctx, c, org, in.project())
			if err != nil {
				return nil, zero, withCode(err)
			}
			out, err := fn(ctx, c, deploy.Target{Org: org, Project: project}, in)
			if err != nil {
				return nil, zero, withCode(err)
			}
			return nil, out, nil
		})
}

func (r *registry) projectDomains() {
	projectTool(r, "goship_list_project_domains", "List the domains attached to a project, the rule of each environment, and the wildcard DNS record each rule needs.", read,
		func(ctx context.Context, c *portal.Client, t deploy.Target, _ ProjectOnlyArg) (ProjectDomainList, error) {
			d, err := c.ProjectDomains(ctx, t.Org, t.Project)
			return ProjectDomainList{Domains: d}, err
		})
	projectTool(r, "goship_add_project_domain", "Attach a verified domain to a project; choose what new apps get with goship_set_project_domain_environment.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in ProjectDomainArg) (ProjectDomainResult, error) {
			d, err := c.AttachProjectDomain(ctx, t.Org, t.Project, in.Domain)
			if err != nil {
				return ProjectDomainResult{}, err
			}
			return ProjectDomainResult{Message: "Attached " + d.Domain + " to project " + t.Project + ". Choose what new apps get with goship_set_project_domain_environment.", Domain: d}, nil
		})
	projectTool(r, "goship_remove_project_domain", "Detach a domain from a project and drop its rules; hosts already given to apps stay.", destructive,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in ProjectDomainArg) (Message, error) {
			d, err := deploy.ResolveProjectDomain(ctx, c, t, in.Domain)
			if err != nil {
				return Message{}, err
			}
			if err := c.DetachProjectDomain(ctx, t.Org, t.Project, d.ID); err != nil {
				return Message{}, err
			}
			return msgf("Detached %s from project %s.", d.Domain, t.Project)
		})
	projectTool(r, "goship_set_project_domain_environment", "Give the new apps of an environment a host under a project domain, <app>[.<subdomain>].<domain>.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in SetProjectDomainArgs) (ProjectDomainResult, error) {
			d, err := deploy.ResolveProjectDomain(ctx, c, t, in.Domain)
			if err != nil {
				return ProjectDomainResult{}, err
			}
			updated, err := c.SetProjectDomainRule(ctx, t.Org, t.Project, d.ID, in.Environment, in.Subdomain)
			if err != nil {
				return ProjectDomainResult{}, err
			}
			var lines []string
			for _, rule := range updated.Environments {
				if rule.Environment != in.Environment {
					continue
				}
				lines = append(lines, "New apps in "+in.Environment+" get "+rule.ExampleHost+".")
				if rule.RecordValue != "" {
					lines = append(lines, "The customer must create this DNS record: "+rule.RecordName+" CNAME "+rule.RecordValue)
				}
				if rule.MissingApps > 0 {
					verb := "do"
					if rule.MissingApps == 1 {
						verb = "does"
					}
					lines = append(lines, plural(rule.MissingApps, "existing app")+" "+verb+" not have it yet: goship_apply_project_domain attaches it.")
				}
			}
			return ProjectDomainResult{Message: strings.Join(lines, "\n"), Domain: updated}, nil
		})
	projectTool(r, "goship_unset_project_domain_environment", "Stop giving the new apps of an environment a host under a project domain.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in ProjectDomainRuleArg) (Message, error) {
			d, err := deploy.ResolveProjectDomain(ctx, c, t, in.Domain)
			if err != nil {
				return Message{}, err
			}
			if err := c.RemoveProjectDomainRule(ctx, t.Org, t.Project, d.ID, in.Environment); err != nil {
				return Message{}, err
			}
			return msgf("New apps in %s no longer get a host under %s.", in.Environment, d.Domain)
		})
	projectTool(r, "goship_apply_project_domain", "Attach the rule's host to every app already in the environment that does not have one.", write,
		func(ctx context.Context, c *portal.Client, t deploy.Target, in ProjectDomainRuleArg) (ProjectDomainApplyResult, error) {
			d, err := deploy.ResolveProjectDomain(ctx, c, t, in.Domain)
			if err != nil {
				return ProjectDomainApplyResult{}, err
			}
			res, err := c.ApplyProjectDomainRule(ctx, t.Org, t.Project, d.ID, in.Environment)
			if err != nil {
				return ProjectDomainApplyResult{}, err
			}
			out := ProjectDomainApplyResult{Attached: res.Attached, Failed: res.Failed}
			if out.Attached == nil {
				out.Attached = []string{}
			}
			lines := []string{"Attached to " + plural(len(res.Attached), "app") + "."}
			for _, f := range res.Failed {
				lines = append(lines, "could not attach "+f.App+": "+f.Reason)
			}
			out.Message = strings.Join(lines, "\n")
			return out, nil
		})
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
