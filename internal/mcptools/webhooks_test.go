package mcptools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The list answers the webhooks and the events to choose from in one call;
// creating returns the secret; removing asks for confirmation like every
// destructive tool.
func TestWebhookToolsCallTheAPI(t *testing.T) {
	var calls []string
	url := api(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/webhooks"):
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":"w2","url":"https://hooks.example.com/y","events":["deploy.failed"],"secret":"whsec_abc"}`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/deliveries"):
			w.Write([]byte(`{"deliveries":[{"id":"d1","event":"ping","status":"delivered","attempts":1,"response_status":200}]}`)) //nolint:errcheck
		case strings.HasSuffix(r.URL.Path, "/ping"):
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"queued"}`)) //nolint:errcheck
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Write([]byte(`{"webhooks":[{"id":"w1","url":"https://hooks.example.com/x","events":["deploy.failed"]}],"events":["deploy.started","deploy.failed"]}`)) //nolint:errcheck
		}
	})
	cs := session(t, fakeClients{url: url, token: "tok", org: "acme", project: "atlas"}, Options{})

	res := call(t, cs, "goship_list_webhooks", map[string]any{})
	var list WebhookList
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &list); res.IsError || err != nil || len(list.Webhooks) != 1 || len(list.Events) != 2 {
		t.Fatalf("list: %s %v %q", raw, err, text(res))
	}

	res = call(t, cs, "goship_create_webhook", map[string]any{"url": "https://hooks.example.com/y", "events": []string{"deploy.failed"}})
	if res.IsError || !strings.Contains(text(res), "whsec_abc") {
		t.Errorf("create: %q", text(res))
	}
	if res = call(t, cs, "goship_ping_webhook", map[string]any{"id": "w1"}); res.IsError {
		t.Errorf("ping: %q", text(res))
	}
	if res = call(t, cs, "goship_list_webhook_deliveries", map[string]any{"id": "w1", "limit": 3}); res.IsError || !strings.Contains(text(res), "delivered") {
		t.Errorf("deliveries: %q", text(res))
	}
	if res = call(t, cs, "goship_delete_webhook", map[string]any{"id": "w1", "org": "other"}); res.IsError {
		t.Errorf("delete: %q", text(res))
	}
	want := "GET /api/v1/orgs/acme/webhooks|POST /api/v1/orgs/acme/webhooks|POST /api/v1/orgs/acme/webhooks/w1/ping|GET /api/v1/orgs/acme/webhooks/w1/deliveries?limit=3|DELETE /api/v1/orgs/other/webhooks/w1"
	if got := strings.Join(calls, "|"); got != want {
		t.Errorf("calls = %s", got)
	}
}
