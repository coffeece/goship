package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

func TestResolveCluster(t *testing.T) {
	clusters := `[{"id":"c-goship","name":"goship","type":"kubernetes","status":"active","shared":true},
	              {"id":"c-home","name":"homolog","type":"vps","status":"active"},
	              {"id":"c-a","name":"twin","type":"vps","status":"active"},
	              {"id":"c-b","name":"twin","type":"vps","status":"active","shared":true}]`
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Write([]byte(clusters)) //nolint:errcheck
	}))
	defer srv.Close()
	client := portal.New(srv.URL, "t")
	ctx := context.Background()

	for _, tc := range []struct{ ref, want string }{
		{"c-home", "c-home"},
		{"homolog", "c-home"},
		// A shared cluster is named like any other.
		{"goship", "c-goship"},
		// An id wins over a name.
		{"c-b", "c-b"},
	} {
		got, err := ResolveCluster(ctx, client, "acme", tc.ref)
		if err != nil || got != tc.want {
			t.Errorf("ResolveCluster(%q) = %q, %v; want %q", tc.ref, got, err, tc.want)
		}
	}
	if asked != "/api/v1/orgs/acme/clusters" {
		t.Errorf("asked %s", asked)
	}

	_, err := ResolveCluster(ctx, client, "acme", "twin")
	if err == nil || !strings.Contains(err.Error(), "c-a") || !strings.Contains(err.Error(), "c-b (shared)") {
		t.Errorf("an ambiguous name should name the candidates, got %v", err)
	}

	_, err = ResolveCluster(ctx, client, "acme", "nope")
	if err == nil || !strings.Contains(err.Error(), `no cluster "nope"`) || !strings.Contains(err.Error(), "`goship clusters` (goship_list_clusters)") {
		t.Errorf("an unknown ref should say how to list them, got %v", err)
	}
}
