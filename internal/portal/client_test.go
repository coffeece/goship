package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestAvailablePlansAsksForTheNodesPool(t *testing.T) {
	var gotQuery string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`[{"slug":"b","kind":"app","is_active":true,"default":true},{"slug":"a","kind":"app","is_active":true,"sort_order":1}]`)) //nolint:errcheck
	})

	plans, err := c.AvailablePlans(context.Background(), "acme", "app", "node 1")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "node=node+1" {
		t.Errorf("query = %q", gotQuery)
	}
	// For a node the platform's order is the contract; sort_order is ignored.
	if plans[0].Slug != "b" || !plans[0].Default {
		t.Errorf("got %+v", plans)
	}
}
