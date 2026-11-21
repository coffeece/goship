package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const webhookListJSON = `{"webhooks":[{"id":"w1","url":"https://hooks.example.com/x","events":["deploy.failed"],"created_at":"2026-11-21T12:00:00Z"}],"events":["deploy.started","deploy.succeeded","deploy.failed"]}`

// stdout must carry the signing secret and nothing else, so a script can
// capture it; --event all asks for every event the API offers.
func TestWebhookAddPrintsOnlyTheSecret(t *testing.T) {
	var body struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/orgs/acme/webhooks" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Method == http.MethodGet {
			w.Write([]byte(webhookListJSON)) //nolint:errcheck
			return
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"w2","url":"https://hooks.example.com/y","events":["deploy.started","deploy.succeeded","deploy.failed"],"secret":"whsec_abc"}`)) //nolint:errcheck
	})

	out, err := run(t, "", "webhook", "add", "https://hooks.example.com/y", "--event", "all")
	if err != nil {
		t.Fatal(err)
	}
	if out != "whsec_abc\n" {
		t.Errorf("stdout = %q, want only the secret", out)
	}
	if body.URL != "https://hooks.example.com/y" || strings.Join(body.Events, ",") != "deploy.started,deploy.succeeded,deploy.failed" {
		t.Errorf("body = %+v", body)
	}
}

func TestWebhookAddSendsTheNamedEvents(t *testing.T) {
	var events []string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("named events need no lookup, got %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Events []string `json:"events"`
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		events = body.Events
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"w2","secret":"whsec_abc"}`)) //nolint:errcheck
	})
	if _, err := run(t, "", "webhook", "add", "https://hooks.example.com/y", "-e", "deploy.failed,app.stopped", "-e", "domain.verified"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "deploy.failed,app.stopped,domain.verified" {
		t.Errorf("events = %v", events)
	}
	if _, err := run(t, "", "webhook", "add", "https://hooks.example.com/y"); err == nil {
		t.Error("a webhook with no event was accepted")
	}
}

func TestWebhookListPingDeliveriesAndRemoveCallTheAPI(t *testing.T) {
	var calls []string
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch {
		case strings.HasSuffix(r.URL.Path, "/deliveries"):
			w.Write([]byte(`{"deliveries":[{"id":"d1","event":"ping","status":"failed","attempts":1,"response_status":500,"error":"the endpoint answered 500","created_at":"2026-11-21T12:00:00Z"}]}`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/ping"):
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"queued"}`)) //nolint:errcheck
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Write([]byte(webhookListJSON)) //nolint:errcheck
		}
	})

	list, err := run(t, "", "webhooks")
	if err != nil || !strings.Contains(list, "https://hooks.example.com/x") || !strings.Contains(list, "deploy.failed") {
		t.Fatalf("webhooks = %q, %v", list, err)
	}
	events, err := run(t, "", "webhook", "events")
	if err != nil || events != "deploy.started\ndeploy.succeeded\ndeploy.failed\n" {
		t.Errorf("events = %q, %v", events, err)
	}
	if _, err := run(t, "", "webhook", "ping", "w1"); err != nil {
		t.Fatal(err)
	}
	ds, err := run(t, "", "webhook", "deliveries", "w1", "-n", "5")
	if err != nil || !strings.Contains(ds, "ping") || !strings.Contains(ds, "failed") || !strings.Contains(ds, "the endpoint answered 500") {
		t.Errorf("deliveries = %q, %v", ds, err)
	}
	if _, err := run(t, "", "webhook", "rm", "w1"); err != nil {
		t.Fatal(err)
	}
	want := "GET /api/v1/orgs/acme/webhooks|GET /api/v1/orgs/acme/webhooks|POST /api/v1/orgs/acme/webhooks/w1/ping|GET /api/v1/orgs/acme/webhooks/w1/deliveries?limit=5|DELETE /api/v1/orgs/acme/webhooks/w1"
	if got := strings.Join(calls, "|"); got != want {
		t.Errorf("calls = %s", got)
	}
}
