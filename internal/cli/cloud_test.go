package cli

import (
	"context"
	"encoding/json"
	"net/http"
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
