package deploy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coffeece/goship/internal/portal"
)

func TestResolveCloudAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"prod-do"},` + //nolint:errcheck
				`{"id":"acc-2","provider":"digitalocean","label":"staging-do"},{"id":"acc-3","provider":"gcp","label":"gcp-main"}]`))
		default:
			w.Write([]byte(`[{"name":"hetzner","label":"Hetzner"}]`)) //nolint:errcheck
		}
	}))
	defer srv.Close()
	client := portal.New(srv.URL, "t")
	ctx := context.Background()

	for _, tc := range []struct{ ref, want string }{
		{"acc-2", "acc-2"},
		{"staging-do", "acc-2"},
		// A provider names its account when it has only one.
		{"gcp", "acc-3"},
	} {
		got, err := ResolveCloudAccount(ctx, client, "acme", tc.ref)
		if err != nil || got.ID != tc.want {
			t.Errorf("ResolveCloudAccount(%q) = %v, %v; want %s", tc.ref, got, err, tc.want)
		}
	}

	_, err := ResolveCloudAccount(ctx, client, "acme", "digitalocean")
	if err == nil || !strings.Contains(err.Error(), "prod-do") || !strings.Contains(err.Error(), "staging-do") {
		t.Fatalf("ambiguous ref should list both labels, got %v", err)
	}

	_, err = ResolveCloudAccount(ctx, client, "acme", "hetzner")
	if err == nil || !strings.Contains(err.Error(), "no Hetzner account connected") || !strings.Contains(err.Error(), "goship cloud connect hetzner") {
		t.Fatalf("a known provider with no account should point at connect, got %v", err)
	}
	_, err = ResolveCloudAccount(ctx, client, "acme", "nope")
	if err == nil || !strings.Contains(err.Error(), "goship cloud connect") {
		t.Fatalf("missing ref should point at connect, got %v", err)
	}
}
