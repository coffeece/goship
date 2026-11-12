package mcptools

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const projectDomainsJSON = `[{"id":"d1","domain":"example.com","environments":[{"environment":"staging","example_host":"<app>.stg.example.com","record_name":"*.stg.example.com","record_value":"lb.goship.sh","dns":"pending"}]},{"id":"d2","domain":"other.dev","environments":[]}]`

const stagingRuleJSON = `{"id":"d1","domain":"example.com","environments":[{"environment":"staging","example_host":"<app>.stg.example.com","record_name":"*.stg.example.com","record_value":"lb.goship.sh","dns":"pending","missing_apps":2}]}`

func TestProjectDomainToolsCallTheRightRoutes(t *testing.T) {
	base := "/api/v1/orgs/acme/projects/atlas/domains"
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET " + base:
			w.Write([]byte(projectDomainsJSON)) //nolint:errcheck
		case "POST " + base:
			b, _ := io.ReadAll(r.Body)
			if string(b) != `{"domain":"example.com"}` {
				t.Errorf("add body = %s", b)
			}
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
		{"goship_list_project_domains", nil, `"record_name":"*.stg.example.com"`},
		{"goship_list_project_domains", nil, `"record_value":"lb.goship.sh"`},
		{"goship_add_project_domain", map[string]any{"domain": "example.com"}, "Attached example.com"},
		{"goship_remove_project_domain", map[string]any{"domain": "other.dev"}, "Detached other.dev"},
		{"goship_set_project_domain_environment", map[string]any{"domain": "example.com", "environment": "staging", "subdomain": "stg"},
			"New apps in staging get <app>.stg.example.com.\nCreate this DNS record at the domain's DNS provider: *.stg.example.com CNAME lb.goship.sh\n2 existing apps do not have it yet"},
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
			b, _ := io.ReadAll(r.Body)
			put = string(b)
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

func requiredArgs(t *testing.T, tl *mcp.Tool) []string {
	t.Helper()
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema(t, tl), &s); err != nil {
		t.Fatal(err)
	}
	return s.Required
}

func TestProjectDomainRuleToolsRequireTheEnvironment(t *testing.T) {
	tools := toolsByName(t, Options{})
	for _, name := range []string{"goship_set_project_domain_environment", "goship_unset_project_domain_environment", "goship_apply_project_domain"} {
		if !slices.Contains(requiredArgs(t, tools[name]), "environment") {
			t.Errorf("%s does not require environment", name)
		}
	}
	for _, name := range []string{"goship_list_project_domains", "goship_add_project_domain", "goship_remove_project_domain"} {
		if _, ok := properties(t, tools[name])["environment"]; ok {
			t.Errorf("%s takes an environment", name)
		}
	}
}

func TestProjectDomainRuleToolsRejectAnEmptyEnvironment(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	for _, name := range []string{"goship_set_project_domain_environment", "goship_unset_project_domain_environment", "goship_apply_project_domain"} {
		for _, env := range []string{"", "  "} {
			res := call(t, cs, name, map[string]any{"domain": "example.com", "environment": env})
			if !res.IsError || !strings.Contains(text(res), "environment is required") {
				t.Errorf("%s env %q: isError=%v %q", name, env, res.IsError, text(res))
			}
		}
	}
}

func TestSetProjectDomainExplainsAnEmptyRecordValue(t *testing.T) {
	rule := `{"id":"d1","domain":"example.com","environments":[{"environment":"staging","example_host":"<app>.stg.example.com","record_name":"*.stg.example.com","record_value":"","dns":"unavailable"}]}`
	for _, c := range []struct{ name, put, want, not string }{
		{"empty value", rule, "New apps in staging get <app>.stg.example.com.\nThe DNS record *.stg.example.com gets its value once the first app exists in staging. List the project's domains again then.", "Create this DNS record"},
		{"no rule", `{"id":"d1","domain":"example.com","environments":[]}`, "Saved.", ""},
	} {
		url := api(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPut {
				w.Write([]byte(c.put)) //nolint:errcheck
				return
			}
			w.Write([]byte(projectDomainsJSON)) //nolint:errcheck
		})
		cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
		res := call(t, cs, "goship_set_project_domain_environment", map[string]any{"domain": "example.com", "environment": "staging"})
		var out struct {
			Message string `json:"message"`
		}
		json.Unmarshal([]byte(text(res)), &out) //nolint:errcheck
		if res.IsError || !strings.Contains(out.Message, c.want) || (c.not != "" && strings.Contains(out.Message, c.not)) {
			t.Errorf("%s: isError=%v %q", c.name, res.IsError, out.Message)
		}
	}
}

func TestSetProjectDomainAsksBeforeApplying(t *testing.T) {
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.Write([]byte(stagingRuleJSON)) //nolint:errcheck
			return
		}
		w.Write([]byte(projectDomainsJSON)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	res := call(t, cs, "goship_set_project_domain_environment", map[string]any{"domain": "example.com", "environment": "staging"})
	if !strings.Contains(text(res), "ask before calling goship_apply_project_domain") {
		t.Errorf("message = %q", text(res))
	}
}

func TestProjectDomainToolsDescribeWhatTheyReach(t *testing.T) {
	tools := toolsByName(t, Options{})
	for name, want := range map[string]string{
		"goship_apply_project_domain":           "no host under the rule's suffix, including apps created with no_domain on purpose; ask the user first.",
		"goship_set_project_domain_environment": "The result names the wildcard DNS record the domain owner must create.",
	} {
		if !strings.Contains(tools[name].Description, want) {
			t.Errorf("%s description = %q", name, tools[name].Description)
		}
	}
}

func TestApplyProjectDomainResultHasNoNulls(t *testing.T) {
	for _, c := range []struct{ name, body string }{
		{"both", `{"attached":["a"],"failed":[{"app":"c","reason":"another app already uses this domain"}]}`},
		{"none", `{"attached":null,"failed":null}`},
	} {
		url := api(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/apply") {
				w.Write([]byte(c.body)) //nolint:errcheck
				return
			}
			w.Write([]byte(projectDomainsJSON)) //nolint:errcheck
		})
		cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
		res := call(t, cs, "goship_apply_project_domain", map[string]any{"domain": "example.com", "environment": "staging"})
		if strings.Contains(text(res), "null") {
			t.Errorf("%s: null in %s", c.name, text(res))
		}
		if c.name == "both" {
			var out struct {
				Attached []string `json:"attached"`
				Failed   []struct{ App, Reason string }
			}
			json.Unmarshal([]byte(text(res)), &out) //nolint:errcheck
			if len(out.Attached) != 1 || out.Attached[0] != "a" || len(out.Failed) != 1 || out.Failed[0].App != "c" || out.Failed[0].Reason != "another app already uses this domain" {
				t.Errorf("structured = %s", text(res))
			}
		}
	}
}

func TestProjectDomainToolsHonourTheProjectArgument(t *testing.T) {
	var path string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write([]byte(`[]`)) //nolint:errcheck
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})
	call(t, cs, "goship_list_project_domains", map[string]any{"project": "other", "org": "beta"})
	if path != "/api/v1/orgs/beta/projects/other/domains" {
		t.Errorf("path = %q", path)
	}
}

func TestCreateAppPassesNoDomain(t *testing.T) {
	var posted string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			posted = string(b)
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
