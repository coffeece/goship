package portal

import "context"

// ProjectDomain is a domain attached to a project, with one rule per
// environment that gives new apps a host under it.
type ProjectDomain struct {
	ID           string              `json:"id"`
	Domain       string              `json:"domain"`
	Environments []ProjectDomainRule `json:"environments"`
}

// ProjectDomainRule says what new apps of one environment get on a domain.
// RecordName is the wildcard DNS record the domain's owner must create.
type ProjectDomainRule struct {
	Environment     string `json:"environment"`
	EnvironmentName string `json:"environment_name"`
	Subdomain       string `json:"subdomain"`
	Suffix          string `json:"suffix"`
	ExampleHost     string `json:"example_host"`
	RecordName      string `json:"record_name"`
	RecordValue     string `json:"record_value"`
	DNS             string `json:"dns"`
	DNSDetail       string `json:"dns_detail,omitempty"`
	MissingApps     int    `json:"missing_apps"`
}

// ProjectDomainApply is the outcome of attaching a rule's host to the
// environment's existing apps.
type ProjectDomainApply struct {
	Attached []string                    `json:"attached"`
	Failed   []ProjectDomainApplyFailure `json:"failed"`
}

// ProjectDomainApplyFailure is an app a rule's host could not be attached to.
type ProjectDomainApplyFailure struct {
	App    string `json:"app"`
	Reason string `json:"reason"`
}

func projectDomainPath(org, project, id, rest string) string {
	return projectPath(org, project, "/domains/"+esc(id)+rest)
}

func (c *Client) ProjectDomains(ctx context.Context, org, project string) ([]ProjectDomain, error) {
	var out []ProjectDomain
	return out, c.get(ctx, projectPath(org, project, "/domains"), &out)
}

func (c *Client) AttachProjectDomain(ctx context.Context, org, project, domain string) (*ProjectDomain, error) {
	var out ProjectDomain
	return &out, c.post(ctx, projectPath(org, project, "/domains"), map[string]string{"domain": domain}, &out)
}

func (c *Client) DetachProjectDomain(ctx context.Context, org, project, id string) error {
	return c.delete(ctx, projectDomainPath(org, project, id, ""), nil)
}

// SetProjectDomainRule makes new apps of env get <app>[.<subdomain>].<domain>;
// an empty subdomain means none.
func (c *Client) SetProjectDomainRule(ctx context.Context, org, project, id, env, subdomain string) (*ProjectDomain, error) {
	var out ProjectDomain
	return &out, c.put(ctx, projectDomainPath(org, project, id, "/environments/"+esc(env)), map[string]string{"subdomain": subdomain}, &out)
}

func (c *Client) RemoveProjectDomainRule(ctx context.Context, org, project, id, env string) error {
	return c.delete(ctx, projectDomainPath(org, project, id, "/environments/"+esc(env)), nil)
}

func (c *Client) ApplyProjectDomainRule(ctx context.Context, org, project, id, env string) (*ProjectDomainApply, error) {
	var out ProjectDomainApply
	return &out, c.post(ctx, projectDomainPath(org, project, id, "/environments/"+esc(env)+"/apply"), nil, &out)
}
