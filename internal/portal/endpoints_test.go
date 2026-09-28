package portal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// Every call is a method, a path and maybe a body; a typo in any of them is a
// 404 in production. Names with a space prove each segment is escaped.
func TestEndpoints(t *testing.T) {
	node := "n 1"
	for _, tc := range []struct {
		name, method, path, body string
		call                     func(context.Context, *Client) error
	}{
		{"apps", "GET", "/api/v1/orgs/my%20org/apps", "", func(ctx context.Context, c *Client) error { _, err := c.Apps(ctx, "my org"); return err }},
		{"app", "GET", "/api/v1/orgs/acme/apps/my%20app", "", func(ctx context.Context, c *Client) error { _, err := c.App(ctx, "acme", "my app"); return err }},
		{"create app", "POST", "/api/v1/orgs/acme/apps", `{"name":"api","platform":"go","node_id":"n 1"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateApp(ctx, "acme", CreateAppRequest{Name: "api", Platform: "go", NodeID: &node})
			return err
		}},
		{"delete app", "DELETE", "/api/v1/orgs/acme/apps/api", "", func(ctx context.Context, c *Client) error { return c.DeleteApp(ctx, "acme", "api") }},
		{"lifecycle", "POST", "/api/v1/orgs/acme/apps/api/restart", "", func(ctx context.Context, c *Client) error { return c.Lifecycle(ctx, "acme", "api", "restart") }},
		{"scale", "PUT", "/api/v1/orgs/acme/apps/api/units", `{"units":3}`, func(ctx context.Context, c *Client) error { return c.ScaleApp(ctx, "acme", "api", 3) }},
		{"plan", "PUT", "/api/v1/orgs/acme/apps/api/plan", `{"plan":"app-small"}`, func(ctx context.Context, c *Client) error { return c.SetAppPlan(ctx, "acme", "api", "app-small") }},
		{"env", "GET", "/api/v1/orgs/acme/apps/api/env", "", func(ctx context.Context, c *Client) error { _, err := c.Env(ctx, "acme", "api"); return err }},
		{"set env", "POST", "/api/v1/orgs/acme/apps/api/env", `{"envs":[{"name":"A","value":"1","public":false}],"no_restart":true}`, func(ctx context.Context, c *Client) error {
			return c.SetEnv(ctx, "acme", "api", []EnvVar{{Name: "A", Value: "1"}}, true)
		}},
		{"unset env", "DELETE", "/api/v1/orgs/acme/apps/api/env", `{"envs":["A"],"no_restart":false}`, func(ctx context.Context, c *Client) error {
			return c.UnsetEnv(ctx, "acme", "api", []string{"A"}, false)
		}},

		{"databases", "GET", "/api/v1/orgs/acme/databases", "", func(ctx context.Context, c *Client) error { _, err := c.Databases(ctx, "acme"); return err }},
		{"database", "GET", "/api/v1/orgs/acme/databases/shop", "", func(ctx context.Context, c *Client) error { _, err := c.Database(ctx, "acme", "shop"); return err }},
		{"database stats", "GET", "/api/v1/orgs/acme/databases/shop/stats", "", func(ctx context.Context, c *Client) error { _, err := c.DatabaseStats(ctx, "acme", "shop"); return err }},
		{"database users", "GET", "/api/v1/orgs/acme/databases/shop/users", "", func(ctx context.Context, c *Client) error { _, err := c.DatabaseUsers(ctx, "acme", "shop"); return err }},
		{"create database user", "POST", "/api/v1/orgs/acme/databases/shop/users", `{"access_mode":"ro","username":"reader"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateDatabaseUser(ctx, "acme", "shop", "reader", "ro")
			return err
		}},
		{"create database", "POST", "/api/v1/orgs/acme/services/postgresql/instances", `{"description":"","name":"shop","plan":"starter"}`, func(ctx context.Context, c *Client) error {
			return c.CreateDatabase(ctx, "acme", "shop", "starter", "")
		}},
		{"delete database", "DELETE", "/api/v1/orgs/acme/services/postgresql/instances/shop", "", func(ctx context.Context, c *Client) error { return c.DeleteDatabase(ctx, "acme", "shop") }},
		{"bind database", "POST", "/api/v1/orgs/acme/services/postgresql/instances/shop/bind", `{"app_name":"api"}`, func(ctx context.Context, c *Client) error { return c.BindDatabase(ctx, "acme", "shop", "api") }},
		{"unbind database", "DELETE", "/api/v1/orgs/acme/services/postgresql/instances/shop/bind/api", "", func(ctx context.Context, c *Client) error { return c.UnbindDatabase(ctx, "acme", "shop", "api") }},

		{"org domains", "GET", "/api/v1/orgs/acme/domains", "", func(ctx context.Context, c *Client) error { _, err := c.OrgDomains(ctx, "acme"); return err }},
		{"register domain", "POST", "/api/v1/orgs/acme/domains", `{"domain":"acme.dev"}`, func(ctx context.Context, c *Client) error {
			_, err := c.RegisterOrgDomain(ctx, "acme", "acme.dev")
			return err
		}},
		{"verify domain", "POST", "/api/v1/orgs/acme/domains/d1/verify", "", func(ctx context.Context, c *Client) error { return c.VerifyOrgDomain(ctx, "acme", "d1") }},
		{"delete domain", "DELETE", "/api/v1/orgs/acme/domains/d1", "", func(ctx context.Context, c *Client) error { return c.DeleteOrgDomain(ctx, "acme", "d1") }},
		{"add app domain", "POST", "/api/v1/orgs/acme/apps/api/domains", `{"domain":"api.acme.dev"}`, func(ctx context.Context, c *Client) error { return c.AddAppDomain(ctx, "acme", "api", "api.acme.dev") }},
		{"remove app domain", "DELETE", "/api/v1/orgs/acme/apps/api/domains/api.acme.dev", "", func(ctx context.Context, c *Client) error {
			return c.RemoveAppDomain(ctx, "acme", "api", "api.acme.dev")
		}},

		{"nodes", "GET", "/api/v1/orgs/acme/nodes", "", func(ctx context.Context, c *Client) error { _, err := c.Nodes(ctx, "acme"); return err }},
		{"node", "GET", "/api/v1/orgs/acme/nodes/n1", "", func(ctx context.Context, c *Client) error { _, err := c.Node(ctx, "acme", "n1"); return err }},
		{"create node", "POST", "/api/v1/orgs/acme/nodes", `{"name":"box","type":"vps","host":"10.0.0.1","port":22}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateNode(ctx, "acme", CreateNodeRequest{Name: "box", Type: "vps", Host: "10.0.0.1", Port: 22})
			return err
		}},
		{"create node from cloud account", "POST", "/api/v1/orgs/acme/nodes", `{"name":"box","cloud_account_id":"acc1","region":"nyc1","size":"s-1vcpu-1gb"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateNode(ctx, "acme", CreateNodeRequest{Name: "box", CloudAccountID: "acc1", Region: "nyc1", Size: "s-1vcpu-1gb"})
			return err
		}},
		{"delete node", "DELETE", "/api/v1/orgs/acme/nodes/n1", `{"destroy_server":true}`, func(ctx context.Context, c *Client) error { return c.DeleteNode(ctx, "acme", "n1", true) }},

		{"cloud providers", "GET", "/api/v1/cloud/providers", "", func(ctx context.Context, c *Client) error { _, err := c.CloudProviders(ctx); return err }},
		{"cloud accounts", "GET", "/api/v1/orgs/acme/cloud-accounts", "", func(ctx context.Context, c *Client) error { _, err := c.CloudAccounts(ctx, "acme"); return err }},
		{"connect cloud api key", "POST", "/api/v1/orgs/acme/cloud-accounts", `{"provider":"digitalocean","label":"prod","api_key":"secret"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAPIKey(ctx, "acme", "digitalocean", "prod", "secret")
			return err
		}},
		{"begin cloud oauth", "POST", "/api/v1/orgs/acme/cloud-accounts/digitalocean/connect", "", func(ctx context.Context, c *Client) error {
			_, _, err := c.BeginCloudOAuth(ctx, "acme", "digitalocean")
			return err
		}},
		{"cloud connect ticket", "GET", "/api/v1/orgs/acme/cloud-accounts/connect/t1", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudConnectTicket(ctx, "acme", "t1")
			return err
		}},
		{"delete cloud account", "DELETE", "/api/v1/orgs/acme/cloud-accounts/acc1", "", func(ctx context.Context, c *Client) error { return c.DeleteCloudAccount(ctx, "acme", "acc1") }},
		{"cloud regions", "GET", "/api/v1/orgs/acme/cloud-accounts/acc1/regions", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudRegions(ctx, "acme", "acc1")
			return err
		}},
		{"cloud sizes", "GET", "/api/v1/orgs/acme/cloud-accounts/acc1/sizes", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudSizes(ctx, "acme", "acc1", "nyc1")
			return err
		}},

		{"volumes", "GET", "/api/v1/orgs/acme/volumes", "", func(ctx context.Context, c *Client) error { _, err := c.Volumes(ctx, "acme"); return err }},
		{"volume", "GET", "/api/v1/orgs/acme/volumes/data", "", func(ctx context.Context, c *Client) error { _, err := c.Volume(ctx, "acme", "data"); return err }},
		{"create volume", "POST", "/api/v1/orgs/acme/volumes", `{"capacity_gib":5,"name":"data","node_id":"n1","plan":"byon-local"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateVolume(ctx, "acme", "data", "n1", "byon-local", 5)
			return err
		}},
		{"bind volume", "POST", "/api/v1/orgs/acme/volumes/data/bind", `{"app":"api","mount_point":"/data","read_only":true}`, func(ctx context.Context, c *Client) error {
			return c.BindVolume(ctx, "acme", "data", "api", "/data", true)
		}},
		{"unbind volume", "POST", "/api/v1/orgs/acme/volumes/data/unbind", `{"app":"api","mount_point":"/data"}`, func(ctx context.Context, c *Client) error {
			return c.UnbindVolume(ctx, "acme", "data", "api", "/data")
		}},
		{"delete volume", "DELETE", "/api/v1/orgs/acme/volumes/data", "", func(ctx context.Context, c *Client) error { return c.DeleteVolume(ctx, "acme", "data") }},

		{"api tokens", "GET", "/api/v1/auth/tokens", "", func(ctx context.Context, c *Client) error { _, err := c.APITokens(ctx); return err }},
		{"create api token", "POST", "/api/v1/auth/tokens", `{"expires_in_days":90,"name":"ci"}`, func(ctx context.Context, c *Client) error { _, err := c.CreateAPIToken(ctx, "ci", 90); return err }},
		{"revoke api token", "DELETE", "/api/v1/auth/tokens/t1", "", func(ctx context.Context, c *Client) error { return c.RevokeAPIToken(ctx, "t1") }},
		{"me", "GET", "/api/v1/auth/me", "", func(ctx context.Context, c *Client) error { _, err := c.Me(ctx); return err }},
		{"deploys", "GET", "/api/v1/orgs/acme/apps/api/deploys", "", func(ctx context.Context, c *Client) error { _, err := c.Deploys(ctx, "acme", "api", 0); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.EscapedPath() != tc.path {
					t.Errorf("got %s %s, want %s %s", r.Method, r.URL.EscapedPath(), tc.method, tc.path)
				}
				body, _ := io.ReadAll(r.Body)
				if !sameJSON(t, string(body), tc.body) {
					t.Errorf("body = %s, want %s", body, tc.body)
				}
				// null decodes into a slice and a struct alike.
				w.Write([]byte(`null`)) //nolint:errcheck
			})
			if err := tc.call(context.Background(), c); err != nil {
				t.Error(err)
			}
		})
	}
}

// CloudSizes builds its query with url.Values, not string concatenation;
// this pins the resulting encoding the way TestEndpoints can't (it only
// checks the path).
func TestCloudSizesEncodesTheRegionQuery(t *testing.T) {
	var gotQuery string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`null`)) //nolint:errcheck
	})

	if _, err := c.CloudSizes(context.Background(), "acme", "acc1", "nyc 1"); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "region=nyc+1" {
		t.Errorf("query = %q", gotQuery)
	}
}

func sameJSON(t *testing.T, got, want string) bool {
	t.Helper()
	if got == "" || want == "" {
		return got == want
	}
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad fixture %s: %v", want, err)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	return string(gb) == string(wb)
}
