package portal

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

type CloudProvider struct {
	Name          string `json:"name" table:"NAME"`
	Label         string `json:"label" table:"LABEL"`
	Kind          string `json:"kind" table:"KIND"`
	Status        string `json:"status" table:"STATUS"`
	KeyDocsURL    string `json:"key_docs_url"`
	AcceptsAPIKey bool   `json:"accepts_api_key"`
}

type CloudAccount struct {
	ID           string     `json:"id" table:"ID"`
	Provider     string     `json:"provider" table:"PROVIDER"`
	Label        string     `json:"label" table:"LABEL"`
	Kind         string     `json:"kind" table:"KIND"`
	RoleARN      string     `json:"role_arn,omitempty"`
	ProjectID    string     `json:"project_id,omitempty"`
	Status       string     `json:"status" table:"STATUS"`
	NodeCount    int        `json:"node_count" table:"NODES"`
	ErrorMessage string     `json:"error_message,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	// Scope is "org" or "project": where the account was connected. Only the
	// project-level listing says.
	Scope string `json:"scope,omitempty" table:"SCOPE"`
}

type CloudRegion struct {
	Slug    string `json:"slug" table:"SLUG"`
	Label   string `json:"label" table:"LABEL"`
	Country string `json:"country" table:"COUNTRY"`
}

type CloudSize struct {
	Slug         string   `json:"slug" table:"SLUG"`
	VCPU         int      `json:"vcpu" table:"VCPU"`
	MemoryMB     int      `json:"memory_mb"`
	DiskGB       int      `json:"disk_gb"`
	PriceMonthly *float64 `json:"price_monthly"`
	Currency     string   `json:"currency"`
	Band         *struct {
		Slug       string `json:"slug"`
		PriceCents int    `json:"price_cents"`
		Currency   string `json:"currency"`
	} `json:"band,omitempty"`
}

type ConnectTicket struct {
	Status  string        `json:"status"`
	Error   string        `json:"error,omitempty"`
	Account *CloudAccount `json:"account,omitempty"`
}

// AWSSetup is what the role path needs: the CloudFormation quick-create
// link, the external id the stack must carry and GoShip's account id.
type AWSSetup struct {
	LaunchURL       string `json:"launch_url"`
	ExternalID      string `json:"external_id"`
	GoShipAccountID string `json:"goship_account_id"`
	OneClick        bool   `json:"one_click"`
}

func (c *Client) CloudProviders(ctx context.Context) ([]CloudProvider, error) {
	var providers []CloudProvider
	return providers, c.get(ctx, "/cloud/providers", &providers)
}

func (c *Client) CloudAccounts(ctx context.Context, org, project string) ([]CloudAccount, error) {
	var accounts []CloudAccount
	return accounts, c.get(ctx, cloudAccountsPath(org, project), &accounts)
}

// CloudAWSSetup returns the org's AWS setup, or nil when this GoShip cannot
// assume roles (the API answers 404) and only access keys can connect.
func (c *Client) CloudAWSSetup(ctx context.Context, org, project string) (*AWSSetup, error) {
	var setup AWSSetup
	err := c.get(ctx, cloudAccountsPath(org, project)+"/aws/setup", &setup)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &setup, nil
}

func (c *Client) ConnectCloudAWSRole(ctx context.Context, org, project, label, roleARN string) (*CloudAccount, error) {
	var account CloudAccount
	body := map[string]any{"provider": "aws", "label": label, "role_arn": roleARN}
	return &account, c.post(ctx, cloudAccountsPath(org, project), body, &account)
}

func (c *Client) ConnectCloudAWSKeys(ctx context.Context, org, project, label, accessKeyID, secretAccessKey string) (*CloudAccount, error) {
	var account CloudAccount
	body := map[string]any{"provider": "aws", "label": label, "access_key_id": accessKeyID, "secret_access_key": secretAccessKey}
	return &account, c.post(ctx, cloudAccountsPath(org, project), body, &account)
}

func (c *Client) ConnectCloudAPIKey(ctx context.Context, org, project, provider, label, apiKey string) (*CloudAccount, error) {
	var account CloudAccount
	body := map[string]any{"provider": provider, "label": label, "api_key": apiKey}
	return &account, c.post(ctx, cloudAccountsPath(org, project), body, &account)
}

func (c *Client) BeginCloudOAuth(ctx context.Context, org, project, provider string) (authorizeURL, ticket string, err error) {
	var result struct {
		AuthorizeURL string `json:"authorize_url"`
		Ticket       string `json:"ticket"`
	}
	if err := c.post(ctx, cloudAccountsPath(org, project)+"/"+esc(provider)+"/connect", nil, &result); err != nil {
		return "", "", err
	}
	return result.AuthorizeURL, result.Ticket, nil
}

// BeginCloudAWSConnect starts a one-click AWS connect: the console link the
// customer opens, and the ticket that completes when the stack reports back.
func (c *Client) BeginCloudAWSConnect(ctx context.Context, org, project string) (launchURL, ticket string, err error) {
	var out struct {
		LaunchURL string `json:"launch_url"`
		Ticket    string `json:"ticket"`
	}
	if err := c.post(ctx, cloudAccountsPath(org, project)+"/aws/connect", nil, &out); err != nil {
		return "", "", err
	}
	return out.LaunchURL, out.Ticket, nil
}

// GCPConnectStart is a guided Google Cloud connect: the setup command, the
// Cloud Shell to run it in, and the ticket that completes when the script
// reports back.
type GCPConnectStart struct {
	Ticket   string `json:"ticket"`
	Command  string `json:"command"`
	ShellURL string `json:"shell_url"`
}

func (c *Client) BeginCloudGCPConnect(ctx context.Context, org, project string) (*GCPConnectStart, error) {
	var start GCPConnectStart
	return &start, c.post(ctx, cloudAccountsPath(org, project)+"/gcp/connect", nil, &start)
}

// ConnectCloudGCP connects a Google Cloud project whose setup already ran.
func (c *Client) ConnectCloudGCP(ctx context.Context, org, project, label, projectNumber string) (*CloudAccount, error) {
	var account CloudAccount
	body := map[string]any{"provider": "gcp", "label": label, "project_number": projectNumber}
	return &account, c.post(ctx, cloudAccountsPath(org, project), body, &account)
}

func (c *Client) CloudConnectTicket(ctx context.Context, org, project, ticket string) (*ConnectTicket, error) {
	var t ConnectTicket
	return &t, c.get(ctx, cloudAccountsPath(org, project)+"/connect/"+esc(ticket), &t)
}

// MoveCloudAccount puts an account at a project, or back at the
// organization when project is empty. The route is the organization's:
// the scope decides who may use the account, so the organization decides.
func (c *Client) MoveCloudAccount(ctx context.Context, org, id, project string) error {
	return c.post(ctx, cloudAccountsPath(org, "")+"/"+esc(id)+"/move", map[string]string{"project": project}, nil)
}

func (c *Client) DeleteCloudAccount(ctx context.Context, org, project, id string) error {
	return c.delete(ctx, cloudAccountsPath(org, project)+"/"+esc(id), nil)
}

func (c *Client) CloudRegions(ctx context.Context, org, project, accountID string) ([]CloudRegion, error) {
	var regions []CloudRegion
	return regions, c.get(ctx, cloudAccountsPath(org, project)+"/"+esc(accountID)+"/regions", &regions)
}

func (c *Client) CloudSizes(ctx context.Context, org, project, accountID, region string) ([]CloudSize, error) {
	var sizes []CloudSize
	query := url.Values{"region": {region}}.Encode()
	path := cloudAccountsPath(org, project) + "/" + esc(accountID) + "/sizes?" + query
	return sizes, c.get(ctx, path, &sizes)
}

// cloudAccountsPath is the project's view of the cloud accounts (its own and
// the organization's) when a project is given, else the organization's.
func cloudAccountsPath(org, project string) string {
	if project == "" {
		return orgPath(org, "/cloud-accounts")
	}
	return projectPath(org, project, "/cloud-accounts")
}
