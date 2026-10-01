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
	Kind         string     `json:"kind"`
	Status       string     `json:"status" table:"STATUS"`
	NodeCount    int        `json:"node_count" table:"NODES"`
	ErrorMessage string     `json:"error_message,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
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
}

func (c *Client) CloudProviders(ctx context.Context) ([]CloudProvider, error) {
	var providers []CloudProvider
	return providers, c.get(ctx, "/cloud/providers", &providers)
}

func (c *Client) CloudAccounts(ctx context.Context, org string) ([]CloudAccount, error) {
	var accounts []CloudAccount
	return accounts, c.get(ctx, cloudAccountsPath(org), &accounts)
}

// CloudAWSSetup returns the org's AWS setup, or nil when this GoShip cannot
// assume roles (the API answers 404) and only access keys can connect.
func (c *Client) CloudAWSSetup(ctx context.Context, org string) (*AWSSetup, error) {
	var setup AWSSetup
	err := c.get(ctx, orgPath(org, "/cloud-accounts/aws/setup"), &setup)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &setup, nil
}

func (c *Client) ConnectCloudAWSRole(ctx context.Context, org, label, roleARN string) (*CloudAccount, error) {
	var account CloudAccount
	body := map[string]any{"provider": "aws", "label": label, "role_arn": roleARN}
	return &account, c.post(ctx, cloudAccountsPath(org), body, &account)
}

func (c *Client) ConnectCloudAWSKeys(ctx context.Context, org, label, accessKeyID, secretAccessKey string) (*CloudAccount, error) {
	var account CloudAccount
	body := map[string]any{"provider": "aws", "label": label, "access_key_id": accessKeyID, "secret_access_key": secretAccessKey}
	return &account, c.post(ctx, cloudAccountsPath(org), body, &account)
}

func (c *Client) ConnectCloudAPIKey(ctx context.Context, org, provider, label, apiKey string) (*CloudAccount, error) {
	var account CloudAccount
	body := map[string]any{"provider": provider, "label": label, "api_key": apiKey}
	return &account, c.post(ctx, cloudAccountsPath(org), body, &account)
}

func (c *Client) BeginCloudOAuth(ctx context.Context, org, provider string) (authorizeURL, ticket string, err error) {
	var result struct {
		AuthorizeURL string `json:"authorize_url"`
		Ticket       string `json:"ticket"`
	}
	if err := c.post(ctx, cloudAccountsPath(org)+"/"+esc(provider)+"/connect", nil, &result); err != nil {
		return "", "", err
	}
	return result.AuthorizeURL, result.Ticket, nil
}

func (c *Client) CloudConnectTicket(ctx context.Context, org, ticket string) (*ConnectTicket, error) {
	var t ConnectTicket
	return &t, c.get(ctx, cloudAccountsPath(org)+"/connect/"+esc(ticket), &t)
}

func (c *Client) DeleteCloudAccount(ctx context.Context, org, id string) error {
	return c.delete(ctx, cloudAccountsPath(org)+"/"+esc(id), nil)
}

func (c *Client) CloudRegions(ctx context.Context, org, accountID string) ([]CloudRegion, error) {
	var regions []CloudRegion
	return regions, c.get(ctx, cloudAccountsPath(org)+"/"+esc(accountID)+"/regions", &regions)
}

func (c *Client) CloudSizes(ctx context.Context, org, accountID, region string) ([]CloudSize, error) {
	var sizes []CloudSize
	query := url.Values{"region": {region}}.Encode()
	path := cloudAccountsPath(org) + "/" + esc(accountID) + "/sizes?" + query
	return sizes, c.get(ctx, path, &sizes)
}

func cloudAccountsPath(org string) string { return orgPath(org, "/cloud-accounts") }
