package cli

import (
	"net/http"
	"strings"
	"testing"
)

// The password comes back once, so the table must show it.
func TestDBUserAddShowsThePasswordOnce(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/orgs/acme/databases/shop/users" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"user":{"id":"u1","username":"reader","access_mode":"ro"},"password":"s3cret"}`)) //nolint:errcheck
	})

	out, err := run(t, "", "db", "user-add", "shop", "reader", "--access", "ro")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"USERNAME", "PASSWORD", "reader", "ro", "s3cret"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}
