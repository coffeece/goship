package portal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "tok")
}

func TestRequestsAreAuthenticatedAndVersioned(t *testing.T) {
	var gotPath, gotAuth string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Write([]byte(`[]`)) //nolint:errcheck
	})

	if _, err := c.Orgs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/orgs" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("authorization = %q", gotAuth)
	}
}

func TestAPIErrorsCarryTheServerMessage(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"you are not a member of this organization"}`)) //nolint:errcheck
	})

	_, err := c.Orgs(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "not a member") {
		t.Errorf("the server's message should reach the user, got %q", err)
	}
}

func TestErrorsAreClassifiable(t *testing.T) {
	for _, tc := range []struct {
		status int
		check  func(error) bool
		name   string
	}{
		{http.StatusNotFound, IsNotFound, "not found"},
		{http.StatusUnauthorized, IsUnauthorized, "unauthorized"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})
			_, err := c.Orgs(context.Background())
			if !tc.check(err) {
				t.Errorf("%v not classified as %s", err, tc.name)
			}
		})
	}
}

// A bare status with no JSON body still has to produce something readable —
// nginx and the ingress answer that way.
func TestNonJSONErrorsStillReadWell(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	_, err := c.Orgs(context.Background())
	if err == nil || err.Error() == "" {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "%!") {
		t.Errorf("mangled message: %q", err)
	}
}

func TestTraceLogsOneLinePerRequest(t *testing.T) {
	var trace strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[]`)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "tok", WithTrace(&trace))
	if _, err := c.Orgs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trace.String(), "GET /api/v1/orgs") {
		t.Errorf("trace = %q", trace.String())
	}
}

func TestAvailablePlansKeepsTheServersOrder(t *testing.T) {
	for _, tc := range []struct{ kind, wantQuery string }{{"app", "kind=app"}, {"database", "kind=database"}, {"", ""}} {
		var gotQuery string
		c := serve(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Write([]byte(`[{"slug":"b","kind":"app","is_active":true,"default":true},{"slug":"x","kind":"app","is_active":false},{"slug":"a","kind":"app","is_active":true,"sort_order":1}]`)) //nolint:errcheck
		})

		plans, err := c.AvailablePlans(context.Background(), "acme", "clave", "staging", tc.kind)
		if err != nil {
			t.Fatal(err)
		}
		if gotQuery != tc.wantQuery {
			t.Errorf("kind %q: query = %q, want %q", tc.kind, gotQuery, tc.wantQuery)
		}
		// The environment's cluster decides the order (first = default
		// on a customer cluster); sort_order is not applied, and an
		// inactive plan is dropped.
		if len(plans) != 2 || plans[0].Slug != "b" || !plans[0].Default || plans[1].Slug != "a" {
			t.Errorf("kind %q: got %+v", tc.kind, plans)
		}
	}
}

// The contract's JSON tags: a renamed field here decodes to a zero value and
// nothing fails, so the shapes are pinned against a sample of the wire format.
func TestEnvironmentAndClusterShapes(t *testing.T) {
	var envs []ProjectEnvironment
	err := json.Unmarshal([]byte(`[{"environment":{"id":"e1","name":"Staging","slug":"staging","cluster_id":"c1","cluster_name":"homolog","db_server_id":"s1","is_default":false,"position":2,"created_at":"2026-10-31T10:00:00Z","updated_at":"2026-10-31T11:00:00Z"},"cluster":{"id":"c1","name":"homolog","shared":false},"pinned":true,"apps":3,"databases":1,"volumes":2}]`), &envs)
	if err != nil {
		t.Fatal(err)
	}
	want := ProjectEnvironment{
		Environment: Environment{ID: "e1", Name: "Staging", Slug: "staging", ClusterID: "c1", ClusterName: "homolog", DBServerID: "s1", Position: 2,
			CreatedAt: time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC)},
		Cluster: ClusterRef{ID: "c1", Name: "homolog"}, Pinned: true, Apps: 3, Databases: 1, Volumes: 2,
	}
	if len(envs) != 1 || !reflect.DeepEqual(envs[0], want) {
		t.Errorf("got %+v, want %+v", envs, want)
	}

	// A shared cluster of another organization is only the narrow view.
	var cl Cluster
	if err := json.Unmarshal([]byte(`{"id":"goship","name":"GoShip","type":"kubernetes","status":"active","shared":true,"namespaced":true}`), &cl); err != nil {
		t.Fatal(err)
	}
	if cl.ID != "goship" || cl.Name != "GoShip" || !cl.Shared || !cl.Namespaced || cl.Host != "" {
		t.Errorf("got %+v", cl)
	}

	var app App
	if err := json.Unmarshal([]byte(`{"name":"api","environment":"staging","cluster":{"id":"c1","name":"homolog","shared":false}}`), &app); err != nil {
		t.Fatal(err)
	}
	if app.Environment != "staging" || app.Cluster != (ClusterRef{ID: "c1", Name: "homolog"}) {
		t.Errorf("got %+v", app)
	}

	var vol Volume
	if err := json.Unmarshal([]byte(`{"name":"data","cluster":{"id":"c1","name":"homolog","shared":false}}`), &vol); err != nil {
		t.Fatal(err)
	}
	if vol.Cluster.ID != "c1" {
		t.Errorf("got %+v", vol)
	}

	var orgs []Org
	if err := json.Unmarshal([]byte(`[{"slug":"acme","default_environment":"prod"}]`), &orgs); err != nil || orgs[0].DefaultEnvironment != "prod" {
		t.Errorf("got %+v, err %v", orgs, err)
	}
}

func TestDecodeErrorKeepsCode(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"m","code":"reconnect_required"}`)) //nolint:errcheck
	})
	_, err := c.Orgs(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "reconnect_required" || apiErr.Message != "m" {
		t.Fatalf("err = %#v", err)
	}
}

func TestDashboardURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://api.goship.sh":  "https://goship.sh",
		"https://api.goship.sh/": "https://goship.sh",
		"http://localhost:8888":  "http://localhost:8888",
	} {
		if got := New(base, "t").DashboardURL(); got != want {
			t.Errorf("DashboardURL(%q) = %q, want %q", base, got, want)
		}
	}
}
