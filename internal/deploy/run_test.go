package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

// heard is a Reporter that keeps what it was told, in order.
type heard struct{ lines []string }

func (h *heard) Header(f string, a ...any) {
	h.lines = append(h.lines, "header: "+fmt.Sprintf(f, a...))
}
func (h *heard) Note(f string, a ...any)         { h.lines = append(h.lines, "note: "+fmt.Sprintf(f, a...)) }
func (h *heard) Begin(key string)                { h.lines = append(h.lines, "begin: "+key) }
func (h *heard) Done(key, _ string)              { h.lines = append(h.lines, "done: "+key) }
func (h *heard) Event(portal.ReleaseEvent) error { return nil }

// fakePortal serves acme (default environment production) with projects clave
// and mova; web exists in the "project/environment" pairs named by holders.
// Every request is recorded, and a created app's body is kept.
type fakePortal struct {
	holders []string
	asked   []string
	created portal.CreateAppRequest
}

func (f *fakePortal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.asked = append(f.asked, r.Method+" "+r.URL.Path)
	const acme = "/api/v1/orgs/acme/projects/"
	switch {
	case r.URL.Path == "/api/v1/orgs":
		w.Write([]byte(`[{"slug":"acme","default_environment":"production"}]`)) //nolint:errcheck
		return
	case r.URL.Path == "/api/v1/orgs/acme/projects":
		w.Write([]byte(`[{"slug":"clave"},{"slug":"mova"}]`)) //nolint:errcheck
		return
	case strings.HasSuffix(r.URL.Path, "/available-plans"):
		w.Write([]byte(`[{"slug":"app-free","is_active":true}]`)) //nolint:errcheck
		return
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/apps"):
		json.NewDecoder(r.Body).Decode(&f.created) //nolint:errcheck
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"name":"web"}`)) //nolint:errcheck
		return
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/env"):
		return
	}
	for _, h := range f.holders {
		project, env, _ := strings.Cut(h, "/")
		if r.Method == http.MethodGet && r.URL.Path == acme+project+"/environments/"+env+"/apps/web" {
			w.Write([]byte(`{"name":"web","platform":"go"}`)) //nolint:errcheck
			return
		}
	}
	http.NotFound(w, r)
}

// stubBuild replaces the upload with one that records where it went.
func stubBuild(t *testing.T) *Target {
	t.Helper()
	var built Target
	saved := Build
	Build = func(_ context.Context, _ *portal.Client, a BuildArgs, _ Reporter) error {
		built = a.Target
		return nil
	}
	t.Cleanup(func() { Build = saved })
	return &built
}

func TestRunPrintsTheTargetFirst(t *testing.T) {
	cases := []struct {
		name, env, want string
	}{
		{"a chosen environment", "staging", "staging"},
		{"nothing chosen: the org's default", "", "production"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakePortal{holders: []string{"clave/staging", "clave/production"}}
			srv := httptest.NewServer(fake)
			defer srv.Close()
			built := stubBuild(t)

			h := &heard{}
			res, err := Run(context.Background(), portal.New(srv.URL, "t"), Options{
				Dir: t.TempDir(), App: "web", Org: "acme", Project: "clave", Environment: tc.env,
				Env: map[string]string{"LOG_LEVEL": "debug"},
			}, h)
			if err != nil {
				t.Fatal(err)
			}
			if len(h.lines) == 0 || h.lines[0] != "note: Deploying to acme/clave/"+tc.want {
				t.Errorf("reported %q, want the target first", h.lines)
			}
			if res.Environment != tc.want || built.Environment != tc.want {
				t.Errorf("result %q, built in %q; want %q", res.Environment, built.Environment, tc.want)
			}
			if !slices.Contains(h.lines, "begin: variables") {
				t.Errorf("reported %q: the variables step is keyed variables", h.lines)
			}
			if !slices.Contains(fake.asked, "POST /api/v1/orgs/acme/projects/clave/environments/"+tc.want+"/apps/web/env") {
				t.Errorf("asked %q", fake.asked)
			}
		})
	}
}

// The same app in another environment of its project is that environment's
// app, not a sign the user meant another place: the deploy creates it here.
func TestRunCreatesTheAppInItsEnvironment(t *testing.T) {
	fake := &fakePortal{holders: []string{"clave/production"}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	built := stubBuild(t)

	h := &heard{}
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module web\n")
	res, err := Run(context.Background(), portal.New(srv.URL, "t"), Options{
		Dir: dir, App: "web", Org: "acme", Project: "clave", Environment: "staging",
	}, h)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || !slices.Contains(fake.asked, "POST /api/v1/orgs/acme/projects/clave/environments/staging/apps") {
		t.Fatalf("created %v, asked %q", res.Created, fake.asked)
	}
	if fake.created.Plan != "app-free" || built.Environment != "staging" {
		t.Errorf("created %+v, built in %q", fake.created, built.Environment)
	}
}

// An app found elsewhere is reported with the flags that reach it, the
// environment among them.
func TestRunNamesTheEnvironmentOfAnAppElsewhere(t *testing.T) {
	fake := &fakePortal{holders: []string{"mova/production"}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	stubBuild(t)

	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module web\n")
	_, err := Run(context.Background(), portal.New(srv.URL, "t"), Options{
		Dir: dir, App: "web", Org: "acme", Project: "clave", Environment: "staging",
	}, &heard{})
	if err == nil || !strings.Contains(err.Error(), "acme/mova/production") || !strings.Contains(err.Error(), "--environment production") {
		t.Fatalf("err = %v", err)
	}
}

// The default environment and the search for the app elsewhere both read the
// org list; a deploy asks for it once, and not at all when the caller holds it.
func TestRunAsksForTheOrgListOnce(t *testing.T) {
	fake := &fakePortal{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	stubBuild(t)
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module web\n")
	opts := Options{Dir: dir, App: "web", Org: "acme", Project: "clave"}

	if _, err := Run(context.Background(), portal.New(srv.URL, "t"), opts, &heard{}); err != nil {
		t.Fatal(err)
	}
	if n := countOf(fake.asked, "GET /api/v1/orgs"); n != 1 {
		t.Errorf("GET /orgs asked %d times, want 1: %q", n, fake.asked)
	}

	fake.asked = nil
	held := &orgList{orgs: []portal.Org{{Slug: "acme", DefaultEnvironment: "staging"}}}
	opts.Orgs = held.list
	res, err := Run(context.Background(), portal.New(srv.URL, "t"), opts, &heard{})
	if err != nil {
		t.Fatal(err)
	}
	if countOf(fake.asked, "GET /api/v1/orgs") != 0 || held.calls != 1 || res.Environment != "staging" {
		t.Errorf("asked %q, the caller's list %d times; environment %q", fake.asked, held.calls, res.Environment)
	}
}

func countOf(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}
