package portal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type Me struct {
	ID            string   `json:"id"`
	Email         string   `json:"email" table:"EMAIL"`
	Name          string   `json:"name" table:"NAME"`
	Groups        []string `json:"groups" table:"ORGS"`
	Provider      string   `json:"provider"`
	EmailVerified bool     `json:"email_verified"`
	IsAdmin       bool     `json:"is_admin"`
}

// Login exchanges credentials for a bearer token the API accepts.
func (c *Client) Login(ctx context.Context, email, password string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	err := c.post(ctx, "/login", map[string]string{"email": email, "password": password}, &out)
	return out.Token, err
}

func (c *Client) Me(ctx context.Context) (*Me, error) {
	var me Me
	return &me, c.get(ctx, "/auth/me", &me)
}

// ExchangeCode trades an authorization code from the browser login for an
// access token. The verifier proves this is the process that started the flow.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI, verifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.send(c.http, req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("the login answered without a token")
	}
	return out.AccessToken, nil
}
