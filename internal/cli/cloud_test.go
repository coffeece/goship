package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coffeece/goship/internal/config"
	"github.com/coffeece/goship/internal/portal"
)

func TestCloudProvidersLists(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Method + " " + r.URL.Path; got != "GET /api/v1/cloud/providers" {
			t.Errorf("request = %q", got)
		}
		w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"available"}]`)) //nolint:errcheck
	})

	out, err := run(t, "", "cloud", "providers")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "DigitalOcean") {
		t.Errorf("output = %q", out)
	}
}

func TestCloudConnectAPIKeyReadsStdin(t *testing.T) {
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"hetzner","label":"Hetzner","kind":"api_key","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts":
			json.NewDecoder(r.Body).Decode(&body)                                    //nolint:errcheck
			w.Write([]byte(`{"id":"acc-1","provider":"hetzner","label":"Hetzner"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "hz-secret\n", "cloud", "connect", "hetzner", "--api-key-stdin")
	if err != nil {
		t.Fatal(err)
	}
	if body["api_key"] != "hz-secret" {
		t.Errorf("body = %v", body)
	}
	if !strings.Contains(out, "Connected") {
		t.Errorf("output = %q", out)
	}
}

// The key must never show up anywhere the terminal keeps scrollback.
func TestCloudConnectAPIKeyNeverEchoesKey(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"hetzner","label":"Hetzner","kind":"api_key","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`{"id":"acc-1","provider":"hetzner","label":"Hetzner"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "hz-secret\n", "cloud", "connect", "hetzner", "--api-key-stdin")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "hz-secret") {
		t.Errorf("output leaked the key: %q", out)
	}
}

func TestCloudConnectOAuthOpensURLAndPollsTicket(t *testing.T) {
	origInterval := ticketPollInterval
	ticketPollInterval = time.Millisecond
	t.Cleanup(func() { ticketPollInterval = origInterval })

	var openedURL string
	origOpen := openURL
	openURL = func(url string) error { openedURL = url; return nil }
	t.Cleanup(func() { openURL = origOpen })

	polls := 0
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts/digitalocean/connect":
			w.Write([]byte(`{"authorize_url":"https://do.example/oauth","ticket":"tix-1"}`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/connect/tix-1":
			polls++
			if polls < 2 {
				w.Write([]byte(`{"status":"pending"}`)) //nolint:errcheck
				return
			}
			w.Write([]byte(`{"status":"done","account":{"id":"acc-1","provider":"digitalocean","label":"prod-do"}}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "", "cloud", "connect", "digitalocean")
	if err != nil {
		t.Fatal(err)
	}
	if openedURL != "https://do.example/oauth" {
		t.Errorf("openURL got %q", openedURL)
	}
	if polls < 2 {
		t.Errorf("polls = %d, want it to see pending before done", polls)
	}
	if !strings.Contains(out, "Connected") || !strings.Contains(out, "prod-do") {
		t.Errorf("output = %q", out)
	}
}

func TestCloudConnectOAuthFailedTicketReportsReason(t *testing.T) {
	origInterval := ticketPollInterval
	ticketPollInterval = time.Millisecond
	t.Cleanup(func() { ticketPollInterval = origInterval })

	origOpen := openURL
	openURL = func(string) error { return nil }
	t.Cleanup(func() { openURL = origOpen })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts/digitalocean/connect":
			w.Write([]byte(`{"authorize_url":"https://do.example/oauth","ticket":"tix-1"}`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/connect/tix-1":
			w.Write([]byte(`{"status":"failed","error":"acesso negado"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	_, err := run(t, "", "cloud", "connect", "digitalocean")
	if err == nil || !strings.Contains(err.Error(), "acesso negado") {
		t.Fatalf("got %v", err)
	}
}

func TestCloudConnectSoonProviderErrors(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"aws","label":"AWS","kind":"oauth","status":"soon"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	_, err := run(t, "", "cloud", "connect", "aws")
	if err == nil || !strings.Contains(err.Error(), "AWS is coming soon") {
		t.Fatalf("got %v", err)
	}
}

func TestCloudConnectUnavailableProviderErrors(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"unavailable"}]`)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	_, err := run(t, "", "cloud", "connect", "digitalocean")
	if err == nil || !strings.Contains(err.Error(), "not available right now") {
		t.Fatalf("got %v", err)
	}
}

func TestCloudConnectOAuthTimesOut(t *testing.T) {
	origTimeout := oauthConnectTimeout
	oauthConnectTimeout = 20 * time.Millisecond
	t.Cleanup(func() { oauthConnectTimeout = origTimeout })

	origInterval := ticketPollInterval
	ticketPollInterval = time.Millisecond
	t.Cleanup(func() { ticketPollInterval = origInterval })

	origOpen := openURL
	openURL = func(string) error { return nil }
	t.Cleanup(func() { openURL = origOpen })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts/digitalocean/connect":
			w.Write([]byte(`{"authorize_url":"https://do.example/oauth","ticket":"tix-1"}`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/connect/tix-1":
			w.Write([]byte(`{"status":"pending"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	start := time.Now()
	_, err := run(t, "", "cloud", "connect", "digitalocean")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %s, want well under a second", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("got %v", err)
	}
}

// A ticket can go "done" without an account when the API has nothing more to
// say — that must not panic on a nil Account.
func TestCloudConnectOAuthDoneWithNoAccountDoesNotPanic(t *testing.T) {
	origInterval := ticketPollInterval
	ticketPollInterval = time.Millisecond
	t.Cleanup(func() { ticketPollInterval = origInterval })

	origOpen := openURL
	openURL = func(string) error { return nil }
	t.Cleanup(func() { openURL = origOpen })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts/digitalocean/connect":
			w.Write([]byte(`{"authorize_url":"https://do.example/oauth","ticket":"tix-1"}`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/connect/tix-1":
			w.Write([]byte(`{"status":"done"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "", "cloud", "connect", "digitalocean")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Connected DigitalOcean.") {
		t.Errorf("output = %q", out)
	}
}

func TestCloudSizesFormatsNonWholePricesAndOtherCurrencies(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"prod-do"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/acc-1/sizes":
			w.Write([]byte(`[
				{"slug":"s-usd","vcpu":1,"memory_mb":1024,"disk_gb":25,"price_monthly":39.9,"currency":"USD","band":{"slug":"b1","price_cents":5990,"currency":"BRL"}},
				{"slug":"s-eur","vcpu":1,"memory_mb":1024,"disk_gb":25,"price_monthly":4.51,"currency":"EUR"},
				{"slug":"s-gbp","vcpu":1,"memory_mb":1024,"disk_gb":25,"price_monthly":10,"currency":"GBP"}
			]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "", "cloud", "sizes", "prod-do", "--region", "nyc3")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"US$ 39.90/mo", "€ 4.51/mo", "GBP 10/mo", "R$ 59,90/mo"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestCloudSizesRendersPricesAndDash(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"prod-do"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/acc-1/sizes":
			if got := r.URL.Query().Get("region"); got != "nyc3" {
				t.Errorf("region = %q", got)
			}
			w.Write([]byte(`[
				{"slug":"s-2vcpu-4gb","vcpu":2,"memory_mb":4096,"disk_gb":80,"price_monthly":48,"currency":"USD","band":{"slug":"b1","price_cents":9900,"currency":"BRL"}},
				{"slug":"s-1vcpu-1gb","vcpu":1,"memory_mb":1024,"disk_gb":25,"price_monthly":null,"currency":"USD"}
			]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "", "cloud", "sizes", "prod-do", "--region", "nyc3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "US$ 48") {
		t.Errorf("missing the provider price: %q", out)
	}
	if !strings.Contains(out, "-") {
		t.Errorf("missing the dash for the size with no price: %q", out)
	}
}

func TestCloudDisconnectConfirms(t *testing.T) {
	called := false
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"prod-do"}]`)) //nolint:errcheck
		case http.MethodDelete:
			called = true
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	if _, err := run(t, "n\n", "cloud", "disconnect", "prod-do"); err == nil {
		t.Fatal("expected the command to abort")
	}
	if called {
		t.Error("account was deleted without consent")
	}
}

func TestResolveCloudAccountAmbiguousAndMissing(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"prod-do"},` +
			`{"id":"acc-2","provider":"digitalocean","label":"staging-do"}]`)) //nolint:errcheck
	})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	client := portal.New(cfg.Endpoint(), "")

	_, err = resolveCloudAccount(context.Background(), client, "acme", "digitalocean")
	if err == nil || !strings.Contains(err.Error(), "prod-do") || !strings.Contains(err.Error(), "staging-do") {
		t.Fatalf("ambiguous ref should list both labels, got %v", err)
	}

	_, err = resolveCloudAccount(context.Background(), client, "acme", "hetzner")
	if err == nil || !strings.Contains(err.Error(), "goship cloud connect") {
		t.Fatalf("missing ref should point at connect, got %v", err)
	}
}

func TestCloudSizesJSONKeepsRawNumbers(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"prod-do"}]`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/acc-1/sizes":
			w.Write([]byte(`[{"slug":"s-2vcpu-4gb","vcpu":2,"memory_mb":4096,"disk_gb":80,"price_monthly":48,"currency":"USD"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	out, err := run(t, "", "cloud", "sizes", "prod-do", "--region", "nyc3", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"price_monthly"`, `"memory_mb"`, "4096"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in JSON:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/mo") {
		t.Errorf("JSON carries display strings:\n%s", out)
	}
}

// Only admins may list an org's cloud accounts; --all must not fail on the
// orgs where you are only a member.
func TestCloudListAllSkipsForbiddenOrgs(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"},{"id":"2","slug":"globex"}]`)) //nolint:errcheck
		case "/api/v1/orgs/games/cloud-accounts":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"admins only"}`)) //nolint:errcheck
		case "/api/v1/orgs/globex/cloud-accounts":
			w.Write([]byte(`[{"id":"acc-1","provider":"digitalocean","label":"globex-do"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), "cloud", "list", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "globex") || !strings.Contains(out, "globex-do") {
		t.Errorf("output should show the org that answered:\n%s", out)
	}
	if !strings.Contains(stderr, "games") {
		t.Errorf("stderr should say which org was skipped: %q", stderr)
	}
}

// Every other lister still fails on a 403: skipping is opt-in.
func TestOrgListAllStillFailsOnForbiddenElsewhere(t *testing.T) {
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs":
			w.Write([]byte(`[{"id":"1","slug":"games"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	})
	os.Unsetenv("GOSHIP_ORG")

	if _, err := run(t, "", "apps", "--all"); err == nil {
		t.Fatal("apps --all should still fail on a 403")
	}
}

// oauthAPI answers `cloud connect digitalocean`, handing each ticket poll to
// poll.
func oauthAPI(t *testing.T, poll http.HandlerFunc) {
	t.Helper()
	origInterval := ticketPollInterval
	ticketPollInterval = time.Millisecond
	t.Cleanup(func() { ticketPollInterval = origInterval })

	origOpen := openURL
	openURL = func(string) error { return nil }
	t.Cleanup(func() { openURL = origOpen })

	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts/digitalocean/connect":
			w.Write([]byte(`{"authorize_url":"https://goship.example/orgs/acme/settings/cloud-accounts/connect/tix-1","ticket":"tix-1"}`)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/connect/tix-1":
			poll(w, r)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
}

const doneTicket = `{"status":"done","account":{"id":"acc-1","provider":"digitalocean","label":"prod-do"}}`

func TestCloudConnectOAuthRidesOutTransientErrors(t *testing.T) {
	polls := 0
	oauthAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		polls++
		if polls <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte(doneTicket)) //nolint:errcheck
	})

	out, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), "cloud", "connect", "digitalocean")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prod-do") {
		t.Errorf("output = %q", out)
	}
	if strings.Count(stderr, "retrying") != 2 {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "Sign in to GoShip in that browser if asked, then open the link again.") {
		t.Errorf("hint should explain the sign-in detour: %q", stderr)
	}
}

func TestCloudConnectOAuthGivesUpAfterRepeatedErrors(t *testing.T) {
	polls := 0
	oauthAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		polls++
		w.WriteHeader(http.StatusBadGateway)
	})

	if _, err := run(t, "", "cloud", "connect", "digitalocean"); err == nil {
		t.Fatal("expected an error after repeated 502s")
	}
	if polls != maxPollRetries+1 {
		t.Errorf("polls = %d, want %d", polls, maxPollRetries+1)
	}
}

func TestCloudConnectOAuthStopsAtOnceOnClientError(t *testing.T) {
	polls := 0
	oauthAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		polls++
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"ticket not found"}`)) //nolint:errcheck
	})

	_, err := run(t, "", "cloud", "connect", "digitalocean")
	if err == nil || !strings.Contains(err.Error(), "ticket not found") {
		t.Fatalf("got %v", err)
	}
	if polls != 1 {
		t.Errorf("polls = %d, want a 404 to end the wait at once", polls)
	}
}

func TestCloudConnectOAuthCtrlCSaysCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oauthAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		w.Write([]byte(`{"status":"pending"}`)) //nolint:errcheck
	})

	_, _, err := runWithStderr(t, ctx, strings.NewReader(""), "cloud", "connect", "digitalocean")
	if err == nil || !strings.Contains(err.Error(), "cancelled") || strings.Contains(err.Error(), "timed out") {
		t.Fatalf("got %v", err)
	}
}

func TestCloudConnectOAuthJSONRendersTheAccount(t *testing.T) {
	oauthAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(doneTicket)) //nolint:errcheck
	})

	out, err := run(t, "", "cloud", "connect", "digitalocean", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var account portal.CloudAccount
	if err := json.Unmarshal([]byte(out), &account); err != nil || account.ID != "acc-1" {
		t.Errorf("want the account as JSON, got %q (%v)", out, err)
	}
}

func TestCloudConnectOAuthNotesIgnoredLabel(t *testing.T) {
	oauthAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(doneTicket)) //nolint:errcheck
	})

	_, stderr, err := runWithStderr(t, context.Background(), strings.NewReader(""), "cloud", "connect", "digitalocean", "--label", "mine")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "label is set from your DigitalOcean team; rename it in the dashboard") {
		t.Errorf("stderr = %q", stderr)
	}
}

func hetznerAPI(t *testing.T, posted *bool) {
	t.Helper()
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"hetzner","label":"Hetzner","kind":"api_key","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts":
			*posted = true
			w.Write([]byte(`{"id":"acc-1","provider":"hetzner","label":"Hetzner"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestCloudConnectAPIKeyRejectsEmptyKey(t *testing.T) {
	for _, stdin := range []string{"", "\n", "   \n"} {
		posted := false
		hetznerAPI(t, &posted)
		_, err := run(t, stdin, "cloud", "connect", "hetzner", "--api-key-stdin")
		if err == nil || err.Error() != "api key is empty" {
			t.Errorf("stdin %q: got %v", stdin, err)
		}
		if posted {
			t.Errorf("stdin %q: an empty key reached the API", stdin)
		}
	}
}

func TestCloudConnectAPIKeyJSONRendersTheAccount(t *testing.T) {
	posted := false
	hetznerAPI(t, &posted)

	out, err := run(t, "hz-secret\n", "cloud", "connect", "hetzner", "--api-key-stdin", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var account portal.CloudAccount
	if err := json.Unmarshal([]byte(out), &account); err != nil || account.ID != "acc-1" {
		t.Errorf("want the account as JSON, got %q (%v)", out, err)
	}
}

func TestResolveCloudAccountNamesAKnownProvider(t *testing.T) {
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/orgs/acme/cloud-accounts":
			w.Write([]byte(`[]`)) //nolint:errcheck
		case "/api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"digitalocean","label":"DigitalOcean","kind":"oauth","status":"available"}]`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	_, err := run(t, "", "cloud", "regions", "digitalocean")
	want := "no DigitalOcean account connected; run `goship cloud connect digitalocean`"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

const awsProviderJSON = `[{"name":"aws","label":"AWS","kind":"federated","status":"available","accepts_api_key":true}]`

func awsAPI(t *testing.T, setupStatus int, connect func(body map[string]any) (int, string)) *map[string]any {
	t.Helper()
	var body map[string]any
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(awsProviderJSON)) //nolint:errcheck
		case "GET /api/v1/orgs/acme/cloud-accounts/aws/setup":
			w.WriteHeader(setupStatus)
			if setupStatus == http.StatusOK {
				w.Write([]byte(`{"launch_url":"https://console.aws.amazon.com/cloudformation/home#/stacks/quickcreate?x=1","external_id":"goship-org-1","goship_account_id":"111122223333"}`)) //nolint:errcheck
			} else {
				w.Write([]byte(`{"error":"not found"}`)) //nolint:errcheck
			}
		case "POST /api/v1/orgs/acme/cloud-accounts":
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			status, resp := connect(body)
			w.WriteHeader(status)
			w.Write([]byte(resp)) //nolint:errcheck
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return &body
}

func okAccount(map[string]any) (int, string) {
	return http.StatusCreated, `{"id":"acc-aws","provider":"aws","label":"123456789012","kind":"federated"}`
}

func TestCloudConnectAWSRoleARNFlagPostsTheRole(t *testing.T) {
	body := awsAPI(t, http.StatusNotFound, okAccount)
	out, err := run(t, "", "cloud", "connect", "aws", "--role-arn", "arn:aws:iam::123456789012:role/GoShipNodeRole")
	if err != nil {
		t.Fatal(err)
	}
	if (*body)["role_arn"] != "arn:aws:iam::123456789012:role/GoShipNodeRole" || (*body)["provider"] != "aws" {
		t.Errorf("body = %v", *body)
	}
	if _, has := (*body)["api_key"]; has {
		t.Error("api_key must not be sent")
	}
	if !strings.Contains(out, "Connected AWS") {
		t.Errorf("output = %q", out)
	}
}

func TestCloudConnectAWSLabelIsLeftToThePortalUnlessGiven(t *testing.T) {
	body := awsAPI(t, http.StatusNotFound, okAccount)
	arn := "arn:aws:iam::123456789012:role/GoShipNodeRole"
	if _, err := run(t, "", "cloud", "connect", "aws", "--role-arn", arn); err != nil {
		t.Fatal(err)
	}
	if label, has := (*body)["label"]; !has || label != "" {
		t.Errorf("label = %v (present %v), want empty so the portal defaults it", label, has)
	}
	if _, err := run(t, "", "cloud", "connect", "aws", "--role-arn", arn, "--label", "prod-aws"); err != nil {
		t.Fatal(err)
	}
	if (*body)["label"] != "prod-aws" {
		t.Errorf("label = %v, want prod-aws", (*body)["label"])
	}
}

func TestCloudConnectAWSAccessKeysStdinPostsBothKeys(t *testing.T) {
	body := awsAPI(t, http.StatusNotFound, okAccount)
	out, err := run(t, "AKIA1\nverysecret\n", "cloud", "connect", "aws", "--access-keys-stdin")
	if err != nil {
		t.Fatal(err)
	}
	if (*body)["access_key_id"] != "AKIA1" || (*body)["secret_access_key"] != "verysecret" {
		t.Errorf("body = %v", *body)
	}
	if strings.Contains(out, "verysecret") {
		t.Error("the secret was echoed")
	}
}

func TestCloudConnectAWSAccessKeysStdinNeedsTwoLines(t *testing.T) {
	awsAPI(t, http.StatusNotFound, okAccount)
	for _, stdin := range []string{"AKIA1\n", "AKIA1\n\n", ""} {
		_, err := run(t, stdin, "cloud", "connect", "aws", "--access-keys-stdin")
		if err == nil || !strings.Contains(err.Error(), "two lines") {
			t.Errorf("stdin %q: error = %v, want 'two lines'", stdin, err)
		}
	}
}

func TestCloudConnectAWSWithoutTTYNeedsAFlag(t *testing.T) {
	awsAPI(t, http.StatusOK, okAccount)
	_, err := run(t, "", "cloud", "connect", "aws")
	if err == nil || !strings.Contains(err.Error(), "--role-arn") || !strings.Contains(err.Error(), "--access-keys-stdin") {
		t.Fatalf("error = %v, want both flags named", err)
	}
}

func TestCloudConnectAWSRejectsBothFlags(t *testing.T) {
	awsAPI(t, http.StatusNotFound, okAccount)
	_, err := run(t, "AKIA1\ns\n", "cloud", "connect", "aws", "--role-arn", "arn:aws:iam::123456789012:role/GoShipNodeRole", "--access-keys-stdin")
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("error = %v", err)
	}
}

func TestCloudConnectAWSSurfacesTheAPIMessage(t *testing.T) {
	awsAPI(t, http.StatusNotFound, func(map[string]any) (int, string) {
		return http.StatusBadRequest, `{"error":"a AWS recusou a função: confira se a stack GoShip foi criada com o external id goship-org-1"}`
	})
	_, err := run(t, "", "cloud", "connect", "aws", "--role-arn", "arn:aws:iam::123456789012:role/GoShipNodeRole")
	if err == nil || !strings.Contains(err.Error(), "external id goship-org-1") {
		t.Fatalf("error = %v, want the API message verbatim", err)
	}
}

func TestBareARNTakesTheLastFieldOfAPastedRow(t *testing.T) {
	for in, want := range map[string]string{
		"arn:aws:iam::123456789012:role/GoShipNodeRole":              "arn:aws:iam::123456789012:role/GoShipNodeRole",
		"  RoleArn\tarn:aws:iam::123456789012:role/GoShipNodeRole\n": "arn:aws:iam::123456789012:role/GoShipNodeRole",
		"": "",
	} {
		if got := bareARN(in); got != want {
			t.Errorf("bareARN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCloudConnectAWSAcceptsAPastedOutputRow(t *testing.T) {
	body := awsAPI(t, http.StatusNotFound, okAccount)
	if _, err := run(t, "", "cloud", "connect", "aws", "--role-arn", "RoleArn  arn:aws:iam::123456789012:role/GoShipNodeRole"); err != nil {
		t.Fatal(err)
	}
	if (*body)["role_arn"] != "arn:aws:iam::123456789012:role/GoShipNodeRole" {
		t.Errorf("role_arn = %v", (*body)["role_arn"])
	}
}

func TestCloudConnectOtherFederatedProviderIsUnsupported(t *testing.T) {
	posted := false
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/cloud/providers":
			w.Write([]byte(`[{"name":"gcp","label":"GCP","kind":"federated","status":"available"}]`)) //nolint:errcheck
		case "POST /api/v1/orgs/acme/cloud-accounts":
			posted = true
		}
	})
	_, err := run(t, "", "cloud", "connect", "gcp", "--role-arn", "arn:aws:iam::123456789012:role/X")
	if err == nil || !strings.Contains(err.Error(), "unsupported connection kind") {
		t.Fatalf("error = %v", err)
	}
	if posted {
		t.Error("nothing should be posted for an unsupported kind")
	}
}
