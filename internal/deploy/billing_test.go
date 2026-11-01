package deploy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coffeece/goship/internal/portal"
)

func TestBillingNote(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	at := func(s string) *time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return &v
	}
	for _, tc := range []struct {
		name string
		at   *time.Time
		want string
	}{
		{"nothing held", nil, ""},
		{"in the future", at("2026-11-20T15:00:00Z"), "Billing starts when the trial ends on 20 Nov 2026."},
		// 02:30 UTC is still the evening before in São Paulo.
		{"a date in São Paulo, not in UTC", at("2026-11-20T02:30:00Z"), "Billing starts when the trial ends on 19 Nov 2026."},
		{"in the past", at("2026-10-31T12:00:00Z"), "Billing starts shortly."},
		{"right now", at("2026-11-01T12:00:00Z"), "Billing starts shortly."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BillingNote(tc.at, now); got != tc.want {
				t.Errorf("BillingNote = %q, want %q", got, tc.want)
			}
		})
	}
}

// A machine without tzdata still shows the date in São Paulo.
func TestSaoPauloZoneWithoutTzdata(t *testing.T) {
	loc := zoneSaoPaulo(func(string) (*time.Location, error) { return nil, errors.New("unknown time zone") })
	at := time.Date(2026, 11, 20, 2, 30, 0, 0, time.UTC)
	if got := at.In(loc).Format("2 Jan 2006"); got != "19 Nov 2026" {
		t.Errorf("date = %q, want 19 Nov 2026", got)
	}
}

// A deploy reports when billing starts for the app: from the app it just
// created, then from the app it fetches once the release is done.
func TestRunCarriesWhenBillingStarts(t *testing.T) {
	for _, tc := range []struct {
		name, created, deployed string
		want                    string
	}{
		{"held", `{"name":"web","billing_starts_at":"2026-11-20T12:00:00Z"}`, `{"name":"web","billing_starts_at":"2026-11-20T12:00:00Z"}`, "2026-11-20T12:00:00Z"},
		{"billed", `{"name":"web"}`, `{"name":"web"}`, ""},
		// The app as it stands after the deploy decides.
		{"released from the trial meanwhile", `{"name":"web","billing_starts_at":"2026-11-20T12:00:00Z"}`, `{"name":"web"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var created bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				const app = "/api/v1/orgs/acme/projects/atlas/environments/production/apps"
				switch {
				case r.Method == http.MethodGet && r.URL.Path == app+"/web" && !created:
					http.NotFound(w, r)
				case r.Method == http.MethodGet && r.URL.Path == app+"/web":
					w.Write([]byte(tc.deployed)) //nolint:errcheck
				case r.Method == http.MethodPost && r.URL.Path == app:
					created = true
					w.WriteHeader(http.StatusCreated)
					w.Write([]byte(tc.created)) //nolint:errcheck
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs":
					w.Write([]byte(`[{"slug":"acme","default_environment":"production"}]`)) //nolint:errcheck
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/environments":
					w.Write([]byte(`[{"slug":"production"}]`)) //nolint:errcheck
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/projects":
					w.Write([]byte(`[{"slug":"atlas"}]`)) //nolint:errcheck
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			stubBuild(t)

			dir := t.TempDir()
			writeFile(t, dir+"/go.mod", "module web\n")
			res, err := Run(context.Background(), portal.New(srv.URL, "t"), Options{
				Dir: dir, App: "web", Org: "acme", Project: "atlas", Environment: "production", Plan: "app-free",
			}, &heard{})
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if res.BillingStartsAt != nil {
				got = res.BillingStartsAt.UTC().Format(time.RFC3339)
			}
			if got != tc.want || !res.Created {
				t.Errorf("billing starts %q, created %v; want %q, true", got, res.Created, tc.want)
			}
		})
	}
}
