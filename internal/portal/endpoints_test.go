package portal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every call is a method, a path (query included) and maybe a body; a typo in
// any of them is a 404 in production. Names with a space prove each segment is escaped.
func TestEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		call                     func(context.Context, *Client) error
	}{
		{"apps", "GET", "/api/v1/orgs/my%20org/projects/atlas/environments/staging/apps", "", func(ctx context.Context, c *Client) error {
			_, err := c.Apps(ctx, "my org", "atlas", "staging")
			return err
		}},
		{"app", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/my%20app", "", func(ctx context.Context, c *Client) error {
			_, err := c.App(ctx, "acme", "atlas", "staging", "my app")
			return err
		}},
		{"create app", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps", `{"name":"api","platform":"go"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateApp(ctx, "acme", "atlas", "staging", CreateAppRequest{Name: "api", Platform: "go"})
			return err
		}},
		{"create private app", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps", `{"name":"api","platform":"go","private":true}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateApp(ctx, "acme", "atlas", "staging", CreateAppRequest{Name: "api", Platform: "go", Private: true})
			return err
		}},
		{"visibility", "PUT", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/visibility", `{"visibility":"private"}`, func(ctx context.Context, c *Client) error {
			return c.SetAppVisibility(ctx, "acme", "atlas", "staging", "api", "private")
		}},
		{"access users", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/access/users", "", func(ctx context.Context, c *Client) error {
			_, err := c.ListAppAccess(ctx, "acme", "atlas", "staging", "api")
			return err
		}},
		{"add access user", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/access/users", `{"username":"ana"}`, func(ctx context.Context, c *Client) error {
			_, err := c.AddAppAccess(ctx, "acme", "atlas", "staging", "api", "ana", "")
			return err
		}},
		{"reset access user", "PUT", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/access/users/a%20na", `{"password":"pw"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ResetAppAccess(ctx, "acme", "atlas", "staging", "api", "a na", "pw")
			return err
		}},
		{"remove access user", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/access/users/ana", "", func(ctx context.Context, c *Client) error {
			return c.RemoveAppAccess(ctx, "acme", "atlas", "staging", "api", "ana")
		}},
		{"access token", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/access/token", "", func(ctx context.Context, c *Client) error {
			_, err := c.AppAccessToken(ctx, "acme", "atlas", "staging", "api", false)
			return err
		}},
		{"rotate access token", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/access/token/rotate", "", func(ctx context.Context, c *Client) error {
			_, err := c.RotateAppAccessToken(ctx, "acme", "atlas", "staging", "api")
			return err
		}},
		{"delete app", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api", "", func(ctx context.Context, c *Client) error { return c.DeleteApp(ctx, "acme", "atlas", "staging", "api") }},
		{"lifecycle", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/restart", "", func(ctx context.Context, c *Client) error {
			return c.Lifecycle(ctx, "acme", "atlas", "staging", "api", "restart")
		}},
		{"scale", "PUT", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/units", `{"units":3}`, func(ctx context.Context, c *Client) error {
			return c.ScaleApp(ctx, "acme", "atlas", "staging", "api", 3)
		}},
		{"plan", "PUT", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/plan", `{"plan":"app-small"}`, func(ctx context.Context, c *Client) error {
			_, err := c.SetAppPlan(ctx, "acme", "atlas", "staging", "api", "app-small")
			return err
		}},
		{"env", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/env", "", func(ctx context.Context, c *Client) error {
			_, err := c.Env(ctx, "acme", "atlas", "staging", "api")
			return err
		}},
		{"set env", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/env", `{"envs":[{"name":"A","value":"1","public":false}],"no_restart":true}`, func(ctx context.Context, c *Client) error {
			return c.SetEnv(ctx, "acme", "atlas", "staging", "api", []EnvVar{{Name: "A", Value: "1"}}, true)
		}},
		{"unset env", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/env", `{"envs":["A"],"no_restart":false}`, func(ctx context.Context, c *Client) error {
			return c.UnsetEnv(ctx, "acme", "atlas", "staging", "api", []string{"A"}, false)
		}},

		{"databases", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases", "", func(ctx context.Context, c *Client) error {
			_, err := c.Databases(ctx, "acme", "atlas", "staging")
			return err
		}},
		{"database", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop", "", func(ctx context.Context, c *Client) error {
			_, err := c.Database(ctx, "acme", "atlas", "staging", "shop")
			return err
		}},
		{"database stats", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/stats", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseStats(ctx, "acme", "atlas", "staging", "shop")
			return err
		}},
		{"database users", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/users", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseUsers(ctx, "acme", "atlas", "staging", "shop")
			return err
		}},
		{"create database user", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/users", `{"access_mode":"ro","username":"reader"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateDatabaseUser(ctx, "acme", "atlas", "staging", "shop", "reader", "ro")
			return err
		}},
		{"database extensions", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/extensions", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseExtensions(ctx, "acme", "atlas", "staging", "shop")
			return err
		}},
		{"enable database extension", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/extensions", `{"name":"postgis"}`, func(ctx context.Context, c *Client) error {
			_, err := c.EnableDatabaseExtension(ctx, "acme", "atlas", "staging", "shop", "postgis")
			return err
		}},
		{"disable database extension", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/extensions/uuid-ossp", "", func(ctx context.Context, c *Client) error {
			return c.DisableDatabaseExtension(ctx, "acme", "atlas", "staging", "shop", "uuid-ossp")
		}},
		{"create database", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/services/postgresql/instances", `{"description":"","name":"shop","plan":"starter"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateDatabase(ctx, "acme", "atlas", "staging", "shop", "starter", "", "", nil)
			return err
		}},
		{"database servers", "GET", "/api/v1/orgs/acme/database-servers", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseServers(ctx, "acme")
			return err
		}},
		{"create database server", "POST", "/api/v1/orgs/acme/database-servers", `{"cloud_account_id":"acc-1","name":"pg","region":"southamerica-east1","tier":"db-f1-micro"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateDatabaseServer(ctx, "acme", CreateDatabaseServerRequest{CloudAccountID: "acc-1", Name: "pg", Region: "southamerica-east1", Tier: "db-f1-micro"})
			return err
		}},
		{"retry database server", "POST", "/api/v1/orgs/acme/database-servers/s%201/retry", "", func(ctx context.Context, c *Client) error { return c.RetryDatabaseServer(ctx, "acme", "s 1") }},
		{"delete database server", "DELETE", "/api/v1/orgs/acme/database-servers/s%201", "", func(ctx context.Context, c *Client) error { return c.DeleteDatabaseServer(ctx, "acme", "s 1") }},
		{"forget database server", "DELETE", "/api/v1/orgs/acme/database-servers/s%201?force=true", "", func(ctx context.Context, c *Client) error { return c.ForgetDatabaseServer(ctx, "acme", "s 1") }},
		{"database server tiers", "GET", "/api/v1/orgs/acme/database-server-tiers", "", func(ctx context.Context, c *Client) error {
			_, err := c.DatabaseServerTiers(ctx, "acme")
			return err
		}},
		{"create database on server", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/services/postgresql/instances", `{"description":"","name":"shop","plan":"db-small","server_id":"s1"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateDatabase(ctx, "acme", "atlas", "staging", "shop", "db-small", "", "s1", nil)
			return err
		}},
		{"create database on server without plan", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/services/postgresql/instances", `{"description":"","name":"shop","server_id":"s1","size_limit_mb":2048}`, func(ctx context.Context, c *Client) error {
			mb := 2048
			_, err := c.CreateDatabase(ctx, "acme", "atlas", "staging", "shop", "", "", "s1", &mb)
			return err
		}},
		{"set database size limit", "PUT", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/size-limit", `{"size_limit_mb":500}`, func(ctx context.Context, c *Client) error {
			mb := 500
			_, err := c.SetDatabaseSizeLimit(ctx, "acme", "atlas", "staging", "shop", &mb)
			return err
		}},
		{"clear database size limit", "PUT", "/api/v1/orgs/acme/projects/atlas/environments/staging/databases/shop/size-limit", `{"size_limit_mb":null}`, func(ctx context.Context, c *Client) error {
			_, err := c.SetDatabaseSizeLimit(ctx, "acme", "atlas", "staging", "shop", nil)
			return err
		}},
		{"rename database server", "PATCH", "/api/v1/orgs/acme/database-servers/s1", `{"name":"pg-new"}`, func(ctx context.Context, c *Client) error {
			_, err := c.RenameDatabaseServer(ctx, "acme", "s1", "pg-new")
			return err
		}},
		{"set database server disk limit", "PUT", "/api/v1/orgs/acme/database-servers/s1/disk-limit", `{"disk_limit_gb":200}`, func(ctx context.Context, c *Client) error {
			return c.SetDatabaseServerDiskLimit(ctx, "acme", "s1", 200)
		}},
		{"delete database", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/services/postgresql/instances/shop", "", func(ctx context.Context, c *Client) error {
			return c.DeleteDatabase(ctx, "acme", "atlas", "staging", "shop")
		}},
		{"bind database", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/services/postgresql/instances/shop/bind", `{"app_name":"api"}`, func(ctx context.Context, c *Client) error {
			return c.BindDatabase(ctx, "acme", "atlas", "staging", "shop", "api")
		}},
		{"unbind database", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/services/postgresql/instances/shop/bind/api", "", func(ctx context.Context, c *Client) error {
			return c.UnbindDatabase(ctx, "acme", "atlas", "staging", "shop", "api")
		}},

		{"org domains", "GET", "/api/v1/orgs/acme/domains", "", func(ctx context.Context, c *Client) error { _, err := c.OrgDomains(ctx, "acme"); return err }},
		{"register domain", "POST", "/api/v1/orgs/acme/domains", `{"domain":"acme.dev"}`, func(ctx context.Context, c *Client) error {
			_, err := c.RegisterOrgDomain(ctx, "acme", "acme.dev")
			return err
		}},
		{"verify domain", "POST", "/api/v1/orgs/acme/domains/d1/verify", "", func(ctx context.Context, c *Client) error { return c.VerifyOrgDomain(ctx, "acme", "d1") }},
		{"delete domain", "DELETE", "/api/v1/orgs/acme/domains/d1", "", func(ctx context.Context, c *Client) error { return c.DeleteOrgDomain(ctx, "acme", "d1") }},
		{"add app domain", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/domains", `{"domain":"api.acme.dev"}`, func(ctx context.Context, c *Client) error {
			return c.AddAppDomain(ctx, "acme", "atlas", "staging", "api", "api.acme.dev")
		}},
		{"remove app domain", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/domains/api.acme.dev", "", func(ctx context.Context, c *Client) error {
			return c.RemoveAppDomain(ctx, "acme", "atlas", "staging", "api", "api.acme.dev")
		}},

		{"environments", "GET", "/api/v1/orgs/acme/environments", "", func(ctx context.Context, c *Client) error { _, err := c.Environments(ctx, "acme"); return err }},
		{"create environment", "POST", "/api/v1/orgs/acme/environments", `{"name":"Staging","slug":"staging","cluster_id":"c1","db_server_id":"s1"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateEnvironment(ctx, "acme", CreateEnvironmentRequest{Name: "Staging", Slug: "staging", ClusterID: "c1", DBServerID: "s1"})
			return err
		}},
		{"create environment on the shared cluster", "POST", "/api/v1/orgs/acme/environments", `{"name":"Staging","slug":"staging"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateEnvironment(ctx, "acme", CreateEnvironmentRequest{Name: "Staging", Slug: "staging"})
			return err
		}},
		{"delete environment", "DELETE", "/api/v1/orgs/acme/environments/staging", "", func(ctx context.Context, c *Client) error { return c.DeleteEnvironment(ctx, "acme", "staging") }},
		{"project environments", "GET", "/api/v1/orgs/acme/projects/atlas/environments", "", func(ctx context.Context, c *Client) error {
			_, err := c.ProjectEnvironments(ctx, "acme", "atlas")
			return err
		}},

		{"clusters", "GET", "/api/v1/orgs/acme/clusters", "", func(ctx context.Context, c *Client) error { _, err := c.Clusters(ctx, "acme"); return err }},
		{"cluster", "GET", "/api/v1/orgs/acme/clusters/c%201", "", func(ctx context.Context, c *Client) error { _, err := c.Cluster(ctx, "acme", "c 1"); return err }},
		{"create cluster", "POST", "/api/v1/orgs/acme/clusters", `{"name":"box","type":"vps","host":"10.0.0.1","port":22}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateCluster(ctx, "acme", CreateClusterRequest{Name: "box", Type: "vps", Host: "10.0.0.1", Port: 22})
			return err
		}},
		{"create cluster from cloud account", "POST", "/api/v1/orgs/acme/clusters", `{"name":"box","cloud_account_id":"acc1","region":"nyc1","size":"s-1vcpu-1gb"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateCluster(ctx, "acme", CreateClusterRequest{Name: "box", CloudAccountID: "acc1", Region: "nyc1", Size: "s-1vcpu-1gb"})
			return err
		}},
		{"cluster sizes", "GET", "/api/v1/orgs/acme/clusters/c1/sizes", "", func(ctx context.Context, c *Client) error {
			_, err := c.ClusterSizes(ctx, "acme", "c1")
			return err
		}},
		{"resize cluster", "POST", "/api/v1/orgs/acme/clusters/c1/resize", `{"size":"s-2"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ResizeCluster(ctx, "acme", "c1", "s-2")
			return err
		}},
		{"delete cluster", "DELETE", "/api/v1/orgs/acme/clusters/c1", `{"destroy_server":true}`, func(ctx context.Context, c *Client) error { return c.DeleteCluster(ctx, "acme", "c1", true) }},

		{"cloud providers", "GET", "/api/v1/cloud/providers", "", func(ctx context.Context, c *Client) error { _, err := c.CloudProviders(ctx); return err }},
		{"cloud accounts", "GET", "/api/v1/orgs/acme/cloud-accounts", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudAccounts(ctx, "acme")
			return err
		}},
		{"connect cloud api key", "POST", "/api/v1/orgs/acme/cloud-accounts", `{"provider":"digitalocean","label":"prod","api_key":"secret"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAPIKey(ctx, "acme", "digitalocean", "prod", "secret", "")
			return err
		}},
		{"reconnect cloud api key in place", "POST", "/api/v1/orgs/acme/cloud-accounts", `{"provider":"hetzner","label":"hz","api_key":"new","account_id":"acc-1"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAPIKey(ctx, "acme", "hetzner", "hz", "new", "acc-1")
			return err
		}},
		{"begin cloud oauth", "POST", "/api/v1/orgs/acme/cloud-accounts/digitalocean/connect", "", func(ctx context.Context, c *Client) error {
			_, _, err := c.BeginCloudOAuth(ctx, "acme", "digitalocean")
			return err
		}},
		{"begin aws connect", "POST", "/api/v1/orgs/acme/cloud-accounts/aws/connect", "", func(ctx context.Context, c *Client) error {
			_, _, err := c.BeginCloudAWSConnect(ctx, "acme")
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
		{"cloud sizes", "GET", "/api/v1/orgs/acme/cloud-accounts/acc1/sizes?region=nyc1", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudSizes(ctx, "acme", "acc1", "nyc1")
			return err
		}},
		{"aws setup", "GET", "/api/v1/orgs/acme/cloud-accounts/aws/setup", "", func(ctx context.Context, c *Client) error {
			_, err := c.CloudAWSSetup(ctx, "acme")
			return err
		}},
		{"connect aws role", "POST", "/api/v1/orgs/acme/cloud-accounts", `{"label":"prod","provider":"aws","role_arn":"arn:aws:iam::123456789012:role/GoShipNodeRole"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAWSRole(ctx, "acme", "prod", "arn:aws:iam::123456789012:role/GoShipNodeRole")
			return err
		}},
		{"connect aws keys", "POST", "/api/v1/orgs/acme/cloud-accounts", `{"access_key_id":"AKIA1","label":"","provider":"aws","secret_access_key":"s"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudAWSKeys(ctx, "acme", "", "AKIA1", "s")
			return err
		}},
		{"begin gcp connect", "POST", "/api/v1/orgs/acme/cloud-accounts/gcp/connect", "", func(ctx context.Context, c *Client) error {
			_, err := c.BeginCloudGCPConnect(ctx, "acme")
			return err
		}},
		{"connect gcp", "POST", "/api/v1/orgs/acme/cloud-accounts", `{"label":"prod","project_number":"123456789","provider":"gcp"}`, func(ctx context.Context, c *Client) error {
			_, err := c.ConnectCloudGCP(ctx, "acme", "prod", "123456789")
			return err
		}},

		{"volumes", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/volumes", "", func(ctx context.Context, c *Client) error {
			_, err := c.Volumes(ctx, "acme", "atlas", "staging")
			return err
		}},
		{"volume", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/volumes/data", "", func(ctx context.Context, c *Client) error {
			_, err := c.Volume(ctx, "acme", "atlas", "staging", "data")
			return err
		}},
		{"create volume", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/volumes", `{"capacity_gib":5,"name":"data","plan":"byon-local"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateVolume(ctx, "acme", "atlas", "staging", "data", "byon-local", 5)
			return err
		}},
		{"bind volume", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/volumes/data/bind", `{"app":"api","mount_point":"/data","read_only":true}`, func(ctx context.Context, c *Client) error {
			return c.BindVolume(ctx, "acme", "atlas", "staging", "data", "api", "/data", true)
		}},
		{"unbind volume", "POST", "/api/v1/orgs/acme/projects/atlas/environments/staging/volumes/data/unbind", `{"app":"api","mount_point":"/data"}`, func(ctx context.Context, c *Client) error {
			return c.UnbindVolume(ctx, "acme", "atlas", "staging", "data", "api", "/data")
		}},
		{"delete volume", "DELETE", "/api/v1/orgs/acme/projects/atlas/environments/staging/volumes/data", "", func(ctx context.Context, c *Client) error {
			return c.DeleteVolume(ctx, "acme", "atlas", "staging", "data")
		}},

		{"projects", "GET", "/api/v1/orgs/acme/projects", "", func(ctx context.Context, c *Client) error { _, err := c.Projects(ctx, "acme"); return err }},
		{"create project", "POST", "/api/v1/orgs/acme/projects", `{"name":"Atlas","slug":"atlas"}`, func(ctx context.Context, c *Client) error {
			_, err := c.CreateProject(ctx, "acme", "Atlas", "atlas")
			return err
		}},
		{"available plans", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/available-plans?kind=app", "", func(ctx context.Context, c *Client) error {
			_, err := c.AvailablePlans(ctx, "acme", "atlas", "staging", "app")
			return err
		}},

		{"api tokens", "GET", "/api/v1/auth/tokens", "", func(ctx context.Context, c *Client) error { _, err := c.APITokens(ctx); return err }},
		{"create api token", "POST", "/api/v1/auth/tokens", `{"expires_in_days":90,"name":"ci"}`, func(ctx context.Context, c *Client) error { _, err := c.CreateAPIToken(ctx, "ci", 90); return err }},
		{"revoke api token", "DELETE", "/api/v1/auth/tokens/t1", "", func(ctx context.Context, c *Client) error { return c.RevokeAPIToken(ctx, "t1") }},
		{"me", "GET", "/api/v1/auth/me", "", func(ctx context.Context, c *Client) error { _, err := c.Me(ctx); return err }},
		{"deploys", "GET", "/api/v1/orgs/acme/projects/atlas/environments/staging/apps/api/deploys", "", func(ctx context.Context, c *Client) error {
			_, err := c.Deploys(ctx, "acme", "atlas", "staging", "api", 0)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Errorf("got %s %s, want %s %s", r.Method, r.URL.RequestURI(), tc.method, tc.path)
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
		tok, err := c.AppAccessToken(context.Background(), "acme", "atlas", "staging", "api", reveal)
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

	if _, err := c.CloudSizes(context.Background(), "acme", "acc1", "nyc 1"); err != nil {
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
	s, err := c.CloudAWSSetup(context.Background(), "acme")
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
