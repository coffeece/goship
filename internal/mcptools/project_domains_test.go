package mcptools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const projectDomainsJSON = `[{"id":"d1","domain":"example.com","environments":[]},{"id":"d2","domain":"other.dev","environments":[]}]`

const stagingRuleJSON = `{"id":"d1","domain":"example.com","environments":[{"environment":"staging","example_host":"<app>.stg.example.com","record_name":"*.stg.example.com","record_value":"lb.goship.sh","dns":"pending","missing_apps":2}]}`

func TestProjectDomainToolsCallTheRightRoutes(t *testing.T) {
	base := "/api/v1/orgs/acme/projects/atlas/domains"
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET " + base:
			w.Write([]byte(projectDomainsJSON)) //nolint:errcheck
		case "POST " + base:
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"d1","domain":"example.com","environments":[]}`)) //nolint:errcheck
		case "PUT " + base + "/d1/environments/staging":
			w.Write([]byte(stagingRuleJSON)) //nolint:errcheck
		case "POST " + base + "/d1/environments/staging/apply":
			w.Write([]byte(`{"attached":["a"],"failed":[{"app":"c","reason":"another app already uses this domain"}]}`)) //nolint:errcheck
		case "DELETE " + base + "/d1/environments/staging", "DELETE " + base + "/d2":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})

	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"goship_list_project_domains", nil, "other.dev"},
		{"goship_add_project_domain", map[string]any{"domain": "example.com"}, "Attached example.com"},
		{"goship_remove_project_domain", map[string]any{"domain": "other.dev"}, "Detached other.dev"},
		{"goship_set_project_domain_environment", map[string]any{"domain": "example.com", "environment": "staging", "subdomain": "stg"},
			"New apps in staging get <app>.stg.example.com.\nThe customer must create this DNS record: *.stg.example.com CNAME lb.goship.sh\n2 existing apps do not have it yet"},
		{"goship_unset_project_domain_environment", map[string]any{"domain": "example.com", "environment": "staging"}, "no longer get a host"},
		{"goship_apply_project_domain", map[string]any{"domain": "example.com", "environment": "staging"},
			"Attached to 1 app.\ncould not attach c: another app already uses this domain"},
	} {
		res := call(t, cs, c.tool, c.args)
		var out struct {
			Message string `json:"message"`
		}
		json.Unmarshal([]byte(text(res)), &out) //nolint:errcheck
		got := out.Message
		if c.tool == "goship_list_project_domains" {
			got = text(res)
		}
		if res.IsError || !strings.Contains(got, c.want) {
			t.Errorf("%s: isError=%v %q, want %q", c.tool, res.IsError, text(res), c.want)
		}
	}

	res := call(t, cs, "goship_remove_project_domain", map[string]any{"domain": "nope.dev"})
	if !res.IsError || !strings.Contains(text(res), `no domain "nope.dev"`) {
		t.Errorf("unknown domain: isError=%v %q", res.IsError, text(res))
	}
}

func TestSetProjectDomainSendsTheSubdomain(t *testing.T) {
	var put string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b := make([]byte, 200)
			n, _ := r.Body.Read(b)
			put = string(b[:n])
			w.Write([]byte(stagingRuleJSON)) //nolint:errcheck
			return
		}
		w.Write([]byte(projectDomainsJSON)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	call(t, cs, "goship_set_project_domain_environment", map[string]any{"domain": "example.com", "environment": "staging", "subdomain": "stg"})
	if put != `{"subdomain":"stg"}` {
		t.Errorf("put body = %q", put)
	}
}

func TestProjectDomainToolsNeedAnEnvironmentOnlyForRules(t *testing.T) {
	tools := toolsByName(t, Options{})
	for _, name := range []string{"goship_set_project_domain_environment", "goship_unset_project_domain_environment", "goship_apply_project_domain"} {
		if _, ok := properties(t, tools[name])["environment"]; !ok {
			t.Errorf("%s has no environment argument", name)
		}
	}
	for _, name := range []string{"goship_list_project_domains", "goship_add_project_domain", "goship_remove_project_domain"} {
		if _, ok := properties(t, tools[name])["environment"]; ok {
			t.Errorf("%s takes an environment", name)
		}
	}
}

func TestCreateAppPassesNoDomain(t *testing.T) {
	var posted string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b := make([]byte, 300)
			n, _ := r.Body.Read(b)
			posted = string(b[:n])
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"name":"api"}`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_create_app", map[string]any{"name": "api", "platform": "go", "plan": "p", "environment": "staging", "no_domain": true})
	if res.IsError || !strings.Contains(posted, `"no_domain":true`) {
		t.Errorf("isError=%v posted %q", res.IsError, posted)
	}
}
