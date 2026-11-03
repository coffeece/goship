package mcptools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// clustersAPI is an organization with GoShip's shared cluster, one machine of
// its own and a connected DigitalOcean account.
func clustersAPI(t *testing.T, record func(r *http.Request, body string)) string {
	t.Helper()
	return api(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/clusters") && !strings.HasSuffix(r.URL.Path, "/cloud-accounts") {
			record(r, string(b))
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/clusters":
			w.Write([]byte(`[{"id":"c-goship","name":"goship","type":"kubernetes","shared":true},{"id":"c-home","name":"vulcan","type":"vps","status":"active"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-do","provider":"digitalocean","label":"prod-do"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/clusters/c-home":
			w.Write([]byte(`{"id":"c-home","name":"vulcan","stage":"ready"}`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/clusters/c-home/sizes":
			w.Write([]byte(`[{"slug":"s-1vcpu-1gb","vcpu":1,"current":true},{"slug":"s-2vcpu-4gb","vcpu":2}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/clusters", "POST /api/v1/orgs/acme/clusters/c-home/resize":
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"id":"c-new","name":"edge","status":"provisioning"}`)) //nolint:errcheck
		case "DELETE /api/v1/orgs/acme/clusters/c-home":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// Cluster tools belong to the organization: they need no project, and take a
// cluster by name or id.
func TestClusterToolsResolveTheName(t *testing.T) {
	var calls []string
	url := clustersAPI(t, func(r *http.Request, body string) {
		calls = append(calls, strings.TrimSpace(r.Method+" "+r.URL.RequestURI()+" "+body))
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})

	res := call(t, cs, "goship_list_clusters", nil)
	var list ClusterList
	raw, _ := json.Marshal(res.StructuredContent)
	if res.IsError || json.Unmarshal(raw, &list) != nil || len(list.Clusters) != 2 || !list.Clusters[0].Shared {
		t.Errorf("list: isError=%v %s", res.IsError, raw)
	}

	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"goship_get_cluster", map[string]any{"cluster": "vulcan"}, "GET /api/v1/orgs/acme/clusters/c-home"},
		{"goship_get_cluster", map[string]any{"cluster": "c-home"}, "GET /api/v1/orgs/acme/clusters/c-home"},
		{"goship_list_cluster_sizes", map[string]any{"cluster": "vulcan"}, "GET /api/v1/orgs/acme/clusters/c-home/sizes"},
		{"goship_resize_cluster", map[string]any{"cluster": "vulcan", "size": "s-2vcpu-4gb"}, `POST /api/v1/orgs/acme/clusters/c-home/resize {"size":"s-2vcpu-4gb"}`},
		{"goship_delete_cluster", map[string]any{"cluster": "vulcan"}, `DELETE /api/v1/orgs/acme/clusters/c-home {"destroy_server":false}`},
		{"goship_delete_cluster", map[string]any{"cluster": "vulcan", "destroy_server": true}, `DELETE /api/v1/orgs/acme/clusters/c-home {"destroy_server":true}`},
	} {
		calls = nil
		res := call(t, cs, c.tool, c.args)
		if res.IsError {
			t.Errorf("%s %v: %s", c.tool, c.args, text(res))
			continue
		}
		if len(calls) != 1 || calls[0] != c.want {
			t.Errorf("%s %v called %v, want %s", c.tool, c.args, calls, c.want)
		}
	}

	res = call(t, cs, "goship_get_cluster", map[string]any{"cluster": "nope"})
	if !res.IsError || !strings.Contains(text(res), "goship_list_clusters") {
		t.Errorf("unknown cluster: isError=%v %q", res.IsError, text(res))
	}
}

// goship_create_cluster has the CLI's three ways in, and takes exactly one.
func TestCreateClusterPaths(t *testing.T) {
	var posted []string
	url := clustersAPI(t, func(r *http.Request, body string) {
		posted = append(posted, body)
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})

	for _, c := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"cloud account by label", map[string]any{"name": "edge", "cloud_account": "prod-do", "region": "nyc3", "size": "s-1vcpu-1gb"},
			`{"name":"edge","cloud_account_id":"acc-do","region":"nyc3","size":"s-1vcpu-1gb"}`},
		{"cloud account by provider", map[string]any{"name": "edge", "cloud_account": "digitalocean", "region": "nyc3", "size": "s-1vcpu-1gb"},
			`{"name":"edge","cloud_account_id":"acc-do","region":"nyc3","size":"s-1vcpu-1gb"}`},
		{"ssh host", map[string]any{"name": "edge", "host": "203.0.113.7", "ssh_key": "KEY"},
			`{"name":"edge","type":"vps","host":"203.0.113.7","port":22,"ssh_user":"root","ssh_key":"KEY"}`},
		{"ssh host with port and user", map[string]any{"name": "edge", "host": "203.0.113.7", "port": 2222, "ssh_user": "ubuntu", "ssh_key": "KEY"},
			`{"name":"edge","type":"vps","host":"203.0.113.7","port":2222,"ssh_user":"ubuntu","ssh_key":"KEY"}`},
		{"environments", map[string]any{"name": "edge", "kubeconfig": "k", "environments": []string{"development", "production"}},
			`{"name":"edge","type":"kubernetes","kubeconfig":"k","environments":["development","production"]}`},
		{"kubeconfig", map[string]any{"name": "edge", "kubeconfig": "apiVersion: v1\n"},
			`{"name":"edge","type":"kubernetes","kubeconfig":"apiVersion: v1\n"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			posted = nil
			res := call(t, cs, "goship_create_cluster", c.args)
			if res.IsError {
				t.Fatal(text(res))
			}
			var want, got map[string]any
			json.Unmarshal([]byte(c.want), &want) //nolint:errcheck
			if len(posted) != 1 || json.Unmarshal([]byte(posted[0]), &got) != nil || !jsonEqual(got, want) {
				t.Errorf("posted %v, want %s", posted, c.want)
			}
		})
	}

	for _, c := range []struct {
		name, want string
		args       map[string]any
	}{
		{"no path", "exactly one of cloud_account, host or kubeconfig", map[string]any{"name": "edge"}},
		{"two paths", "exactly one of cloud_account, host or kubeconfig", map[string]any{"name": "edge", "host": "h", "ssh_key": "K", "kubeconfig": "k"}},
		{"region without a cloud account", "region only applies with cloud_account", map[string]any{"name": "edge", "host": "h", "ssh_key": "K", "region": "nyc3"}},
		{"ssh user without a host", "ssh_user only applies with host", map[string]any{"name": "edge", "kubeconfig": "k", "ssh_user": "u"}},
		{"cloud without a size", "size is required with cloud_account (goship_list_cloud_sizes)", map[string]any{"name": "edge", "cloud_account": "prod-do", "region": "nyc3"}},
		{"cloud without a region", "region is required with cloud_account (goship_list_cloud_regions)", map[string]any{"name": "edge", "cloud_account": "prod-do", "size": "s"}},
		{"host without a key", "ssh_key is required with host", map[string]any{"name": "edge", "host": "h"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			posted = nil
			res := call(t, cs, "goship_create_cluster", c.args)
			if !res.IsError || !strings.Contains(text(res), c.want) || len(posted) != 0 {
				t.Errorf("isError=%v %q, posted %v; want %q", res.IsError, text(res), posted, c.want)
			}
		})
	}
}

func jsonEqual(a, b map[string]any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}

// A resize the cloud account must be reconnected for links the dashboard's
// resize page for the cluster.
func TestResizeClusterPointsAtTheDashboard(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/clusters":
			w.Write([]byte(`[{"id":"c-home","name":"vulcan"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"reconnect the DigitalOcean account to resize","code":"reconnect_required"}`)) //nolint:errcheck
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_resize_cluster", map[string]any{"cluster": "vulcan", "size": "s-2vcpu-4gb"})
	if !res.IsError || !strings.Contains(text(res), "reconnect the DigitalOcean account to resize") || !strings.Contains(text(res), "/orgs/acme/clusters/c-home/resize") {
		t.Errorf("isError=%v %q", res.IsError, text(res))
	}
}

// The hint keeps the API's answer in the chain, so the agent still gets the
// code to branch on.
func TestResizeClusterKeepsTheReconnectCode(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/clusters":
			w.Write([]byte(`[{"id":"c-home","name":"vulcan"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"reconnect the DigitalOcean account to resize","code":"reconnect_required"}`)) //nolint:errcheck
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme"}, Options{})
	res := call(t, cs, "goship_resize_cluster", map[string]any{"cluster": "vulcan", "size": "s-2vcpu-4gb"})
	if got := lastLine(text(res)); !res.IsError || got != "code: reconnect_required" {
		t.Errorf("isError=%v last line %q, want code: reconnect_required", res.IsError, got)
	}
}
