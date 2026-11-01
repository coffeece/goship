package portal

import (
	"context"
	"net/http"
	"testing"
	"time"
)

var trialEnd = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// billing_starts_at is on an app and a database only while a trial holds
// them, and is nil otherwise.
func TestBillingStartsAtIsOnlyThereWhileHeld(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects/clave/environments/staging/apps":
			w.Write([]byte(`[{"name":"held","billing_starts_at":"2026-11-20T12:00:00Z"},{"name":"billed"}]`)) //nolint:errcheck
		case "/api/v1/orgs/acme/projects/clave/environments/staging/databases":
			w.Write([]byte(`[{"name":"held","billing_starts_at":"2026-11-20T12:00:00Z"},{"name":"billed"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	})

	apps, err := c.Apps(context.Background(), "acme", "clave", "staging")
	if err != nil || len(apps) != 2 {
		t.Fatalf("apps = %v, err = %v", apps, err)
	}
	if got := apps[0].BillingStartsAt; got == nil || !got.Equal(trialEnd) || apps[1].BillingStartsAt != nil {
		t.Errorf("apps billing_starts_at = %v and %v", apps[0].BillingStartsAt, apps[1].BillingStartsAt)
	}

	dbs, err := c.Databases(context.Background(), "acme", "clave", "staging")
	if err != nil || len(dbs) != 2 {
		t.Fatalf("databases = %v, err = %v", dbs, err)
	}
	if got := dbs[0].BillingStartsAt; got == nil || !got.Equal(trialEnd) || dbs[1].BillingStartsAt != nil {
		t.Errorf("databases billing_starts_at = %v and %v", dbs[0].BillingStartsAt, dbs[1].BillingStartsAt)
	}
}

// A create and a plan change answer with their status and, while a trial
// holds the resource, when billing starts.
func TestChangesReturnWhenBillingStarts(t *testing.T) {
	const held = `{"status":"created","billing_starts_at":"2026-11-20T12:00:00Z"}`
	const appPlan = "/api/v1/orgs/acme/projects/clave/environments/staging/apps/api/plan"
	const dbInstances = "/api/v1/orgs/acme/projects/clave/environments/staging/services/postgresql/instances"
	for _, tc := range []struct {
		name, method, path, reply string
		call                      func(*Client) (Change, error)
		want                      string
		starts                    bool
	}{
		{"app plan, held", "PUT", appPlan, held, func(c *Client) (Change, error) {
			return c.SetAppPlan(context.Background(), "acme", "clave", "staging", "api", "app-small")
		}, "created", true},
		{"app plan, billed", "PUT", appPlan, `{"status":"updated"}`, func(c *Client) (Change, error) {
			return c.SetAppPlan(context.Background(), "acme", "clave", "staging", "api", "app-small")
		}, "updated", false},
		{"create database, held", "POST", dbInstances, held, func(c *Client) (Change, error) {
			return c.CreateDatabase(context.Background(), "acme", "clave", "staging", "shop", "db-micro", "", "", nil)
		}, "created", true},
		{"create database, billed", "POST", dbInstances, `{"status":"created"}`, func(c *Client) (Change, error) {
			return c.CreateDatabase(context.Background(), "acme", "clave", "staging", "shop", "db-micro", "", "", nil)
		}, "created", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("got %s %s, want %s %s", r.Method, r.URL.Path, tc.method, tc.path)
				}
				w.Write([]byte(tc.reply)) //nolint:errcheck
			})
			got, err := tc.call(c)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Errorf("status = %q, want %q", got.Status, tc.want)
			}
			if tc.starts != (got.BillingStartsAt != nil) || (tc.starts && !got.BillingStartsAt.Equal(trialEnd)) {
				t.Errorf("billing_starts_at = %v, want %v", got.BillingStartsAt, tc.starts)
			}
		})
	}
}

// The owner's trial carries the allowance; another member's only when it
// ends; an organization whose owner is on no trial has none.
func TestOrgTrialOfTheOwnerAndOfAMember(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[` + //nolint:errcheck
			`{"slug":"menlo","trial":{"ends_at":"2026-11-20T12:00:00Z","held_apps":2,"held_databases":0,"apps_limit":3,"databases_limit":2,"max_units":1,"max_price_cents":1990}},` +
			`{"slug":"mova","trial":{"ends_at":"2026-11-20T12:00:00Z"}},` +
			`{"slug":"acme"}]`))
	})
	orgs, err := c.Orgs(context.Background())
	if err != nil || len(orgs) != 3 {
		t.Fatalf("orgs = %v, err = %v", orgs, err)
	}

	owner := orgs[0].Trial
	if owner == nil || !owner.EndsAt.Equal(trialEnd) {
		t.Fatalf("owner trial = %+v", owner)
	}
	for name, got := range map[string]*int{
		"held_apps": owner.HeldApps, "held_databases": owner.HeldDatabases, "apps_limit": owner.AppsLimit,
		"databases_limit": owner.DatabasesLimit, "max_units": owner.MaxUnits, "max_price_cents": owner.MaxPriceCents,
	} {
		if got == nil {
			t.Errorf("owner trial lacks %s", name)
		}
	}
	// Zero held is a count, not an absent one.
	if owner.HeldDatabases == nil || *owner.HeldDatabases != 0 || *owner.AppsLimit != 3 || *owner.MaxPriceCents != 1990 {
		t.Errorf("owner trial = %+v", owner)
	}

	member := orgs[1].Trial
	if member == nil || !member.EndsAt.Equal(trialEnd) || member.HeldApps != nil || member.AppsLimit != nil || member.MaxPriceCents != nil {
		t.Errorf("member trial = %+v, want only ends_at", member)
	}
	if orgs[2].Trial != nil {
		t.Errorf("org with no trial has %+v", orgs[2].Trial)
	}
}

// A change that was made stays a success when the answer has no body; reads
// still fail on one.
func TestAppPlanWithAnEmptyAnswerIsDone(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	got, err := c.SetAppPlan(context.Background(), "acme", "clave", "staging", "api", "app-small")
	if err != nil || got.BillingStartsAt != nil {
		t.Errorf("change = %+v, err = %v; want a success with no billing_starts_at", got, err)
	}
	if _, err := c.App(context.Background(), "acme", "clave", "staging", "api"); err == nil {
		t.Error("a GET with an empty body succeeded")
	}
}

func TestCreateDatabaseWithAnEmptyAnswerIsDone(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	got, err := c.CreateDatabase(context.Background(), "acme", "clave", "staging", "shop", "db-micro", "", "", nil)
	if err != nil || got.BillingStartsAt != nil {
		t.Errorf("change = %+v, err = %v; want a success with no billing_starts_at", got, err)
	}
}

// A connection that dies before any answer says nothing about whether the
// change was made: that EOF is still an error.
func TestAChangeWhoseConnectionDiesIsAnError(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close() //nolint:errcheck
	})
	if _, err := c.SetAppPlan(context.Background(), "acme", "clave", "staging", "api", "app-small"); err == nil {
		t.Error("SetAppPlan succeeded on a dropped connection")
	}
	if _, err := c.CreateDatabase(context.Background(), "acme", "clave", "staging", "shop", "db-micro", "", "", nil); err == nil {
		t.Error("CreateDatabase succeeded on a dropped connection")
	}
}
