package portal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		{"apps", "GET", "/api/v1/orgs/my%20org/projects/clave/apps", "", func(ctx context.Context, c *Client) error { _, err := c.Apps(ctx, "my org", "clave"); return err }},
		{"app", "GET", "/api/v1/orgs/acme/projects/clave/apps/my%20app", "", func(ctx context.Context, c *Client) error {
			_, err := c.App(ctx, "acme", "clave", "my app")
			return err
		}},
		{"create app", "POST", "/api/v1/orgs/acme/projects/clave/apps", `{"name":"api","platform":"go","node_id":"n 1"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateApp(ctx, "acme", "clave", CreateAppRequest{Name: "api", Platform: "go", NodeID: &node})
			return err
		}},
		{"create private app", "POST", "/api/v1/orgs/acme/projects/clave/apps", `{"name":"api","platform":"go","private":true}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateApp(ctx, "acme", "clave", CreateAppRequest{Name: "api", Platform: "go", Private: true})
			return err
		}},
		{"visibility", "PUT", "/api/v1/orgs/acme/projects/clave/apps/api/visibility", `{"visibility":"private"}`, func(ctx context.Context, c *Client) error {
			return c.SetAppVisibility(ctx, "acme", "clave", "api", "private")
		}},
		{"access users", "GET", "/api/v1/orgs/acme/projects/clave/apps/api/access/users", "", func(ctx context.Context, c *Client) error {
			_, err := c.ListAppAccess(ctx, "acme", "clave", "api")
			return err
		}},
		{"add access user", "POST", "/api/v1/orgs/acme/projects/clave/apps/api/access/users", `{"username":"ana"}`, func(ctx context.Context, c *Client) error {
			_, err := c.AddAppAccess(ctx, "acme", "clave", "api", "ana", "")
			return err
		}},
		{"reset access user", "PUT", "/api/v1/orgs/acme/projects/clave/apps/api/access/users/a%20na", `{"password":"pw"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ResetAppAccess(ctx, "acme", "clave", "api", "a na", "pw")
			return err
		}},
		{"remove access user", "DELETE", "/api/v1/orgs/acme/projects/clave/apps/api/access/users/ana", "", func(ctx context.Context, c *Client) error {
			return c.RemoveAppAccess(ctx, "acme", "clave", "api", "ana")
		}},
		{"access token", "GET", "/api/v1/orgs/acme/projects/clave/apps/api/access/token", "", func(ctx context.Context, c *Client) error {
			_, err := c.AppAccessToken(ctx, "acme", "clave", "api", false)
			return err
		}},
		{"rotate access token", "POST", "/api/v1/orgs/acme/projects/clave/apps/api/access/token/rotate", "", func(ctx context.Context, c *Client) error {
			_, err := c.RotateAppAccessToken(ctx, "acme", "clave", "api")
			return err
		}},
		{"delete app", "DELETE", "/api/v1/orgs/acme/projects/clave/apps/api", "", func(ctx context.Context, c *Client) error { return c.DeleteApp(ctx, "acme", "clave", "api") }},
		{"lifecycle", "POST", "/api/v1/orgs/acme/projects/clave/apps/api/restart", "", func(ctx context.Context, c *Client) error { return c.Lifecycle(ctx, "acme", "clave", "api", "restart") }},
		{"scale", "PUT", "/api/v1/orgs/acme/projects/clave/apps/api/units", `{"units":3}`, func(ctx context.Context, c *Client) error { return c.ScaleApp(ctx, "acme", "clave", "api", 3) }},
		{"plan", "PUT", "/api/v1/orgs/acme/projects/clave/apps/api/plan", `{"plan":"app-small"}`, func(ctx context.Context, c *Client) error {
			return c.SetAppPlan(ctx, "acme", "clave", "api", "app-small")
		}},
		{"env", "GET", "/api/v1/orgs/acme/projects/clave/apps/api/env", "", func(ctx context.Context, c *Client) error { _, err := c.Env(ctx, "acme", "clave", "api"); return err }},
		{"set env", "POST", "/api/v1/orgs/acme/projects/clave/apps/api/env", `{"envs":[{"name":"A","value":"1","public":false}],"no_restart":true}`, func(ctx context.Context, c *Client) error {
			return c.SetEnv(ctx, "acme", "clave", "api", []EnvVar{{Name: "A", Value: "1"}}, true)
		}},
		{"unset env", "DELETE", "/api/v1/orgs/acme/projects/clave/apps/api/env", `{"envs":["A"],"no_restart":false}`, func(ctx context.Context, c *Client) error {
			return c.UnsetEnv(ctx, "acme", "clave", "api", []string{"A"}, false)
		}},

		{"databases", "GET", "/api/v1/orgs/acme/projects/clave/databases", "", func(ctx context.Context, c *Client) error { _, err := c.Databases(ctx, "acme", "clave"); return err }},
		{"database", "GET", "/api/v1/orgs/acme/projects/clave/databases/shop", "", func(ctx context.Context, c *Client) error {
			_, err := c.Database(ctx, "acme", "clave", "shop")
			return err
		}},
		{"database stats", "GET", "/api/v1/orgs/acme/projects/clave/databases/shop/stats", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseStats(ctx, "acme", "clave", "shop")
			return err
		}},
		{"database users", "GET", "/api/v1/orgs/acme/projects/clave/databases/shop/users", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseUsers(ctx, "acme", "clave", "shop")
			return err
		}},
		{"create database user", "POST", "/api/v1/orgs/acme/projects/clave/databases/shop/users", `{"access_mode":"ro","username":"reader"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateDatabaseUser(ctx, "acme", "clave", "shop", "reader", "ro")
			return err
		}},
		{"database extensions", "GET", "/api/v1/orgs/acme/projects/clave/databases/shop/extensions", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseExtensions(ctx, "acme", "clave", "shop")
			return err
		}},
		{"enable database extension", "POST", "/api/v1/orgs/acme/projects/clave/databases/shop/extensions", `{"name":"postgis"}`, func(ctx context.Context, c *Client) error {
			_, err := c.EnableDatabaseExtension(ctx, "acme", "clave", "shop", "postgis")
			return err
		}},
		{"disable database extension", "DELETE", "/api/v1/orgs/acme/projects/clave/databases/shop/extensions/uuid-ossp", "", func(ctx context.Context, c *Client) error {
			return c.DisableDatabaseExtension(ctx, "acme", "clave", "shop", "uuid-ossp")
		}},
		{"create database", "POST", "/api/v1/orgs/acme/projects/clave/services/postgresql/instances", `{"description":"","name":"shop","plan":"starter"}`, func(ctx context.Context, c *Client) error {
			return c.CreateDatabase(ctx, "acme", "clave", "shop", "starter", "", "", nil)
		}},
		{"database servers", "GET", "/api/v1/orgs/acme/projects/clave/database-servers", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseServers(ctx, "acme", "clave")
			return err
		}},
		{"database servers at org", "GET", "/api/v1/orgs/acme/database-servers", "", func(ctx context.Context, c *Client) error { _, err := c.DatabaseServers(ctx, "acme", ""); return err }},
		{"create database server", "POST", "/api/v1/orgs/acme/database-servers", `{"cloud_account_id":"acc-1","name":"pg","region":"southamerica-east1","tier":"db-f1-micro"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateDatabaseServer(ctx, "acme", "", CreateDatabaseServerRequest{CloudAccountID: "acc-1", Name: "pg", Region: "southamerica-east1", Tier: "db-f1-micro"})
			return err
		}},
		{"retry database server", "POST", "/api/v1/orgs/acme/database-servers/s%201/retry", "", func(ctx context.Context, c *Client) error { return c.RetryDatabaseServer(ctx, "acme", "", "s 1") }},
		{"delete database server", "DELETE", "/api/v1/orgs/acme/database-servers/s%201", "", func(ctx context.Context, c *Client) error { return c.DeleteDatabaseServer(ctx, "acme", "", "s 1") }},
		{"database server tiers", "GET", "/api/v1/orgs/acme/database-server-tiers", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseServerTiers(ctx, "acme", "")
			return err
		}},
		{"create database on server", "POST", "/api/v1/orgs/acme/projects/clave/services/postgresql/instances", `{"description":"","name":"shop","plan":"db-small","server_id":"s1"}`, func(ctx context.Context, c *Client) error {
			return c.CreateDatabase(ctx, "acme", "clave", "shop", "db-small", "", "s1", nil)
		}},
		{"create database on server without plan", "POST", "/api/v1/orgs/acme/projects/clave/services/postgresql/instances", `{"description":"","name":"shop","server_id":"s1","size_limit_mb":2048}`, func(ctx context.Context, c *Client) error {
			mb := 2048
			return c.CreateDatabase(ctx, "acme", "clave", "shop", "", "", "s1", &mb)
		}},
		{"set database size limit", "PUT", "/api/v1/orgs/acme/projects/clave/databases/shop/size-limit", `{"size_limit_mb":500}`, func(ctx context.Context, c *Client) error {
			mb := 500
			_, err := c.SetDatabaseSizeLimit(ctx, "acme", "clave", "shop", &mb)
			return err
		}},
		{"clear database size limit", "PUT", "/api/v1/orgs/acme/projects/clave/databases/shop/size-limit", `{"size_limit_mb":null}`, func(ctx context.Context, c *Client) error {
			_, err := c.SetDatabaseSizeLimit(ctx, "acme", "clave", "shop", nil)
			return err
		}},
		{"set database server disk limit", "PUT", "/api/v1/orgs/acme/database-servers/s1/disk-limit", `{"disk_limit_gb":200}`, func(ctx context.Context, c *Client) error {
			return c.SetDatabaseServerDiskLimit(ctx, "acme", "", "s1", 200)
		}},
		{"delete database", "DELETE", "/api/v1/orgs/acme/projects/clave/services/postgresql/instances/shop", "", func(ctx context.Context, c *Client) error { return c.DeleteDatabase(ctx, "acme", "clave", "shop") }},
		{"bind database", "POST", "/api/v1/orgs/acme/projects/clave/services/postgresql/instances/shop/bind", `{"app_name":"api"}`, func(ctx context.Context, c *Client) error { return c.BindDatabase(ctx, "acme", "clave", "shop", "api") }},
		{"unbind database", "DELETE", "/api/v1/orgs/acme/projects/clave/services/postgresql/instances/shop/bind/api", "", func(ctx context.Context, c *Client) error {
			return c.UnbindDatabase(ctx, "acme", "clave", "shop", "api")
		}},

		{"org domains", "GET", "/api/v1/orgs/acme/domains", "", func(ctx context.Context, c *Client) error { _, err := c.OrgDomains(ctx, "acme"); return err }},
		{"register domain", "POST", "/api/v1/orgs/acme/domains", `{"domain":"acme.dev"}`, func(ctx context.Context, c *Client) error {
			_, err := c.RegisterOrgDomain(ctx, "acme", "acme.dev")
			return err
		}},
		{"verify domain", "POST", "/api/v1/orgs/acme/domains/d1/verify", "", func(ctx context.Context, c *Client) error { return c.VerifyOrgDomain(ctx, "acme", "d1") }},
		{"delete domain", "DELETE", "/api/v1/orgs/acme/domains/d1", "", func(ctx context.Context, c *Client) error { return c.DeleteOrgDomain(ctx, "acme", "d1") }},
		{"add app domain", "POST", "/api/v1/orgs/acme/projects/clave/apps/api/domains", `{"domain":"api.acme.dev"}`, func(ctx context.Context, c *Client) error {
			return c.AddAppDomain(ctx, "acme", "clave", "api", "api.acme.dev")
		}},
		{"remove app domain", "DELETE", "/api/v1/orgs/acme/projects/clave/apps/api/domains/api.acme.dev", "", func(ctx context.Context, c *Client) error {
			return c.RemoveAppDomain(ctx, "acme", "clave", "api", "api.acme.dev")
		}},

		{"nodes", "GET", "/api/v1/orgs/acme/projects/clave/nodes", "", func(ctx context.Context, c *Client) error { _, err := c.Nodes(ctx, "acme", "clave"); return err }},
		{"node", "GET", "/api/v1/orgs/acme/projects/clave/nodes/n1", "", func(ctx context.Context, c *Client) error { _, err := c.Node(ctx, "acme", "clave", "n1"); return err }},
		{"create node", "POST", "/api/v1/orgs/acme/projects/clave/nodes", `{"name":"box","type":"vps","host":"10.0.0.1","port":22}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateNode(ctx, "acme", "clave", CreateNodeRequest{Name: "box", Type: "vps", Host: "10.0.0.1", Port: 22})
			return err
		}},
		{"create node from cloud account", "POST", "/api/v1/orgs/acme/projects/clave/nodes", `{"name":"box","cloud_account_id":"acc1","region":"nyc1","size":"s-1vcpu-1gb"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateNode(ctx, "acme", "clave", CreateNodeRequest{Name: "box", CloudAccountID: "acc1", Region: "nyc1", Size: "s-1vcpu-1gb"})
			return err
		}},
		{"node sizes", "GET", "/api/v1/orgs/acme/projects/clave/nodes/n1/sizes", "", func(ctx context.Context, c *Client) error {
			_, err := c.NodeSizes(ctx, "acme", "clave", "n1")
			return err
		}},
		{"resize node", "POST", "/api/v1/orgs/acme/projects/clave/nodes/n1/resize", `{"size":"e2-standard-4"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ResizeNode(ctx, "acme", "clave", "n1", "e2-standard-4")
			return err
		}},
		{"delete node", "DELETE", "/api/v1/orgs/acme/projects/clave/nodes/n1", `{"destroy_server":true}`, func(ctx context.Context, c *Client) error { return c.DeleteNode(ctx, "acme", "clave", "n1", true) }},

		{"cloud providers", "GET", "/api/v1/cloud/providers", "", func(ctx context.Context, c *Client) error { _, err := c.CloudProviders(ctx); return err }},
		{"cloud accounts", "GET", "/api/v1/orgs/acme/projects/clave/cloud-accounts", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudAccounts(ctx, "acme", "clave")
			return err
		}},
		{"connect cloud api key", "POST", "/api/v1/orgs/acme/projects/clave/cloud-accounts", `{"provider":"digitalocean","label":"prod","api_key":"secret"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAPIKey(ctx, "acme", "clave", "digitalocean", "prod", "secret")
			return err
		}},
		{"begin cloud oauth", "POST", "/api/v1/orgs/acme/projects/clave/cloud-accounts/digitalocean/connect", "", func(ctx context.Context, c *Client) error {
			_, _, err := c.BeginCloudOAuth(ctx, "acme", "clave", "digitalocean")
			return err
		}},
		{"begin aws connect", "POST", "/api/v1/orgs/acme/projects/clave/cloud-accounts/aws/connect", "", func(ctx context.Context, c *Client) error {
			_, _, err := c.BeginCloudAWSConnect(ctx, "acme", "clave")
			return err
		}},
		{"cloud connect ticket", "GET", "/api/v1/orgs/acme/projects/clave/cloud-accounts/connect/t1", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudConnectTicket(ctx, "acme", "clave", "t1")
			return err
		}},
		{"delete cloud account", "DELETE", "/api/v1/orgs/acme/projects/clave/cloud-accounts/acc1", "", func(ctx context.Context, c *Client) error { return c.DeleteCloudAccount(ctx, "acme", "clave", "acc1") }},
		{"cloud regions", "GET", "/api/v1/orgs/acme/projects/clave/cloud-accounts/acc1/regions", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudRegions(ctx, "acme", "clave", "acc1")
			return err
		}},
		{"cloud sizes", "GET", "/api/v1/orgs/acme/projects/clave/cloud-accounts/acc1/sizes", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudSizes(ctx, "acme", "clave", "acc1", "nyc1")
			return err
		}},
		{"aws setup", "GET", "/api/v1/orgs/acme/projects/clave/cloud-accounts/aws/setup", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudAWSSetup(ctx, "acme", "clave")
			return err
		}},
		{"connect aws role", "POST", "/api/v1/orgs/acme/projects/clave/cloud-accounts", `{"label":"prod","provider":"aws","role_arn":"arn:aws:iam::123456789012:role/GoShipNodeRole"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAWSRole(ctx, "acme", "clave", "prod", "arn:aws:iam::123456789012:role/GoShipNodeRole")
			return err
		}},
		{"connect aws keys", "POST", "/api/v1/orgs/acme/projects/clave/cloud-accounts", `{"access_key_id":"AKIA1","label":"","provider":"aws","secret_access_key":"s"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAWSKeys(ctx, "acme", "clave", "", "AKIA1", "s")
			return err
		}},
		{"begin gcp connect", "POST", "/api/v1/orgs/acme/projects/clave/cloud-accounts/gcp/connect", "", func(ctx context.Context, c *Client) error {
			_, err := c.BeginCloudGCPConnect(ctx, "acme", "clave")
			return err
		}},
		{"connect gcp", "POST", "/api/v1/orgs/acme/projects/clave/cloud-accounts", `{"label":"prod","project_number":"123456789","provider":"gcp"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudGCP(ctx, "acme", "clave", "prod", "123456789")
			return err
		}},

		{"volumes", "GET", "/api/v1/orgs/acme/projects/clave/volumes", "", func(ctx context.Context, c *Client) error { _, err := c.Volumes(ctx, "acme", "clave"); return err }},
		{"volume", "GET", "/api/v1/orgs/acme/projects/clave/volumes/data", "", func(ctx context.Context, c *Client) error {
			_, err := c.Volume(ctx, "acme", "clave", "data")
			return err
		}},
		{"create volume", "POST", "/api/v1/orgs/acme/projects/clave/volumes", `{"capacity_gib":5,"name":"data","node_id":"n1","plan":"byon-local"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateVolume(ctx, "acme", "clave", "data", "n1", "byon-local", 5)
			return err
		}},
		{"bind volume", "POST", "/api/v1/orgs/acme/projects/clave/volumes/data/bind", `{"app":"api","mount_point":"/data","read_only":true}`, func(ctx context.Context, c *Client) error {
			return c.BindVolume(ctx, "acme", "clave", "data", "api", "/data", true)
		}},
		{"unbind volume", "POST", "/api/v1/orgs/acme/projects/clave/volumes/data/unbind", `{"app":"api","mount_point":"/data"}`, func(ctx context.Context, c *Client) error {
			return c.UnbindVolume(ctx, "acme", "clave", "data", "api", "/data")
		}},
		{"delete volume", "DELETE", "/api/v1/orgs/acme/projects/clave/volumes/data", "", func(ctx context.Context, c *Client) error { return c.DeleteVolume(ctx, "acme", "clave", "data") }},

		{"projects", "GET", "/api/v1/orgs/acme/projects", "", func(ctx context.Context, c *Client) error { _, err := c.Projects(ctx, "acme"); return err }},
		{"create project", "POST", "/api/v1/orgs/acme/projects", `{"name":"Clave","slug":"clave"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateProject(ctx, "acme", "Clave", "clave")
			return err
		}},
		{"placement", "GET", "/api/v1/orgs/acme/projects/clave/placement", "", func(ctx context.Context, c *Client) error { _, err := c.Placement(ctx, "acme", "clave"); return err }},
		{"available plans", "GET", "/api/v1/orgs/acme/projects/clave/available-plans", "", func(ctx context.Context, c *Client) error {
			_, err := c.AvailablePlans(ctx, "acme", "clave", "app", "")
			return err
		}},
		{"cloud accounts without a project", "GET", "/api/v1/orgs/acme/cloud-accounts", "", func(ctx context.Context, c *Client) error { _, err := c.CloudAccounts(ctx, "acme", ""); return err }},
		{"connect cloud api key at the org level", "POST", "/api/v1/orgs/acme/cloud-accounts", `{"provider":"digitalocean","label":"prod","api_key":"secret"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAPIKey(ctx, "acme", "", "digitalocean", "prod", "secret")
			return err
		}},

		{"api tokens", "GET", "/api/v1/auth/tokens", "", func(ctx context.Context, c *Client) error { _, err := c.APITokens(ctx); return err }},
		{"create api token", "POST", "/api/v1/auth/tokens", `{"expires_in_days":90,"name":"ci"}`, func(ctx context.Context, c *Client) error { _, err := c.CreateAPIToken(ctx, "ci", 90); return err }},
		{"revoke api token", "DELETE", "/api/v1/auth/tokens/t1", "", func(ctx context.Context, c *Client) error { return c.RevokeAPIToken(ctx, "t1") }},
		{"me", "GET", "/api/v1/auth/me", "", func(ctx context.Context, c *Client) error { _, err := c.Me(ctx); return err }},
		{"deploys", "GET", "/api/v1/orgs/acme/projects/clave/apps/api/deploys", "", func(ctx context.Context, c *Client) error {
			_, err := c.Deploys(ctx, "acme", "clave", "api", 0)
			return err
		}},
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

func TestAppAccessTokenRevealQuery(t *testing.T) {
	for _, reveal := range []bool{false, true} {
		var gotQuery string
		c := serve(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Write([]byte(`{"token":"tok","masked":false}`)) //nolint:errcheck
		})
		tok, err := c.AppAccessToken(context.Background(), "acme", "clave", "api", reveal)
		if err != nil || tok != "tok" {
			t.Fatalf("token = %q, err = %v", tok, err)
		}
		if want := map[bool]string{false: "", true: "reveal=1"}[reveal]; gotQuery != want {
			t.Errorf("reveal=%v: query = %q, want %q", reveal, gotQuery, want)
		}
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

	if _, err := c.CloudSizes(context.Background(), "acme", "clave", "acc1", "nyc 1"); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "region=nyc+1" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestCloudAWSSetupTreats404AsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "tok")
	s, err := c.CloudAWSSetup(context.Background(), "acme", "clave")
	if err != nil || s != nil {
		t.Fatalf("CloudAWSSetup() = %+v, %v; want nil, nil", s, err)
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
