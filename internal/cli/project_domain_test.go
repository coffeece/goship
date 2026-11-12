package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const domainsJSON = `[{"id":"d1","domain":"example.com","environments":[` +
	`{"environment":"staging","subdomain":"stg","example_host":"<app>.stg.example.com","record_name":"*.stg.example.com","record_value":"lb.goship.sh","dns":"pending"}]},` +
	`{"id":"d2","domain":"other.dev","environments":[]}]`

const stagingRule = `{"id":"d1","domain":"example.com","environments":[{"environment":"staging","subdomain":"stg","example_host":"<app>.stg.example.com","record_name":"*.stg.example.com","record_value":"lb.goship.sh","dns":"pending","missing_apps":2}]}`

func TestProjectDomainList(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/orgs/acme/projects/atlas/domains" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Write([]byte(domainsJSON)) //nolint:errcheck
	})
	out, err := run(t, "", "project", "domain", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("output:\n%s", out)
	}
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "DOMAIN ENVIRONMENT APPS GET DNS RECORD DNS" {
		t.Errorf("header = %q", lines[0])
	}
	if got := strings.Join(strings.Fields(lines[1]), " "); got != "example.com staging <app>.stg.example.com *.stg.example.com CNAME lb.goship.sh pending" {
		t.Errorf("row = %q", got)
	}
	if got := strings.Join(strings.Fields(lines[2]), " "); got != "other.dev - - - -" {
		t.Errorf("bare row = %q", got)
	}
}

func TestProjectDomainAdd(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/acme/projects/atlas/domains" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"d1","domain":"example.com","environments":[]}`)) //nolint:errcheck
	})
	if _, err := run(t, "", "project", "domain", "add", "example.com"); err != nil {
		t.Fatal(err)
	}
	if body["domain"] != "example.com" {
		t.Errorf("body = %v", body)
	}
}

func TestProjectDomainSetPrintsTheRecordAndTheMissingApps(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/atlas/domains":
			w.Write([]byte(domainsJSON)) //nolint:errcheck
		case "PUT /api/v1/orgs/acme/projects/atlas/domains/d1/environments/staging":
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			w.Write([]byte(stagingRule))          //nolint:errcheck
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := run(t, "", "project", "domain", "set", "example.com", "--environment", "staging", "--subdomain", "stg")
	if err != nil {
		t.Fatal(err)
	}
	if body["subdomain"] != "stg" {
		t.Errorf("body = %v", body)
	}
	want := "New apps in staging get <app>.stg.example.com.\n" +
		"Create this DNS record: *.stg.example.com CNAME lb.goship.sh\n" +
		"2 existing apps do not have it yet: goship project domain apply example.com --environment staging\n"
	if out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
}

func TestProjectDomainSingularWording(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(domainsJSON)) //nolint:errcheck
		case http.MethodPut:
			w.Write([]byte(`{"id":"d1","domain":"example.com","environments":[{"environment":"staging","example_host":"<app>.example.com","record_name":"*.example.com","record_value":"","dns":"unknown","missing_apps":1}]}`)) //nolint:errcheck
		default:
			w.Write([]byte(`{"attached":[],"failed":[{"app":"c","reason":"no"}]}`)) //nolint:errcheck
		}
	})
	out, _ := run(t, "", "project", "domain", "set", "example.com", "--environment", "staging")
	if !strings.Contains(out, "1 existing app does not have it yet: ") {
		t.Errorf("set output:\n%s", out)
	}
	_, err := run(t, "", "project", "domain", "apply", "example.com", "--environment", "staging")
	if err == nil || err.Error() != "1 app could not be attached" {
		t.Errorf("err = %v", err)
	}
}

func TestProjectDomainSetWithoutSubdomainSendsNone(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(domainsJSON)) //nolint:errcheck
			return
		}
		json.NewDecoder(r.Body).Decode(&body)                                                                                                                                                               //nolint:errcheck
		w.Write([]byte(`{"id":"d1","domain":"example.com","environments":[{"environment":"staging","example_host":"<app>.example.com","record_name":"*.example.com","record_value":"","dns":"unknown"}]}`)) //nolint:errcheck
	})
	out, err := run(t, "", "project", "domain", "set", "d1", "--environment", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if body["subdomain"] != "" {
		t.Errorf("body = %v", body)
	}
	if strings.Contains(out, "Create this DNS record") || strings.Contains(out, "existing apps") {
		t.Errorf("output:\n%s", out)
	}
}

func TestProjectDomainNeedsAnEnvironment(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected call %s", r.URL.Path) })
	for _, verb := range []string{"set", "unset", "apply"} {
		_, err := run(t, "", "project", "domain", verb, "example.com")
		if err == nil || !strings.Contains(err.Error(), "--environment") {
			t.Errorf("%s: err = %v", verb, err)
		}
	}
}

func TestProjectDomainUnset(t *testing.T) {
	var deleted string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/atlas/domains":
			w.Write([]byte(domainsJSON)) //nolint:errcheck
		case "DELETE /api/v1/orgs/acme/projects/atlas/domains/d1/environments/staging":
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if _, err := run(t, "", "project", "domain", "unset", "example.com", "--environment", "staging"); err != nil || deleted == "" {
		t.Fatalf("err = %v, deleted = %q", err, deleted)
	}
}

func TestProjectDomainRm(t *testing.T) {
	var deleted string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/atlas/domains":
			w.Write([]byte(domainsJSON)) //nolint:errcheck
		case "DELETE /api/v1/orgs/acme/projects/atlas/domains/d2":
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if _, err := run(t, "", "project", "domain", "rm", "other.dev"); err != nil || deleted == "" {
		t.Fatalf("err = %v, deleted = %q", err, deleted)
	}
}

func TestProjectDomainApply(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/projects/atlas/domains":
			w.Write([]byte(domainsJSON)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/projects/atlas/domains/d1/environments/staging/apply":
			w.Write([]byte(`{"attached":["a","b"],"failed":[{"app":"c","reason":"another app already uses this domain"}]}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := run(t, "", "project", "domain", "apply", "example.com", "--environment", "staging")
	if err == nil {
		t.Error("a failed attach must be an error")
	}
	want := "Attached to 2 apps.\ncould not attach c: another app already uses this domain\n"
	if out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
}

func TestProjectDomainApplyAllAttached(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(domainsJSON)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"attached":["a"],"failed":[]}`)) //nolint:errcheck
	})
	out, err := run(t, "", "project", "domain", "apply", "example.com", "--environment", "staging")
	if err != nil || out != "Attached to 1 app.\n" {
		t.Fatalf("err = %v, output %q", err, out)
	}
}

func TestProjectDomainUnknown(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(domainsJSON)) }) //nolint:errcheck
	_, err := run(t, "", "project", "domain", "rm", "x")
	if err == nil || err.Error() != `no domain "x" attached to project "atlas"` {
		t.Fatalf("err = %v", err)
	}
}

func TestAppCreateNoDomain(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"name":"api"}`)) //nolint:errcheck
	})
	if _, err := run(t, "", "app", "create", "api", "--platform", "go", "--no-domain"); err != nil {
		t.Fatal(err)
	}
	if body["no_domain"] != true {
		t.Errorf("body = %v", body)
	}
}
