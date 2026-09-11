package render

import (
	"encoding/json"
	"strings"
	"testing"
)

type app struct {
	Name    string   `json:"name" table:"NAME"`
	Status  string   `json:"status" table:"STATUS"`
	Domains []string `json:"domains" table:"DOMAINS"`
	OrgID   string   `json:"org_id"`
}

func render(t *testing.T, format string, v any) string {
	t.Helper()
	var out strings.Builder
	if err := New(&out, format).Render(v); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestTableUsesTaggedFieldsInOrder(t *testing.T) {
	got := render(t, Table, []app{
		{Name: "api", Status: "running", Domains: []string{"api.example"}, OrgID: "hidden"},
		{Name: "worker", Status: "stopped"},
	})

	if !strings.HasPrefix(got, "NAME") || !strings.Contains(got, "DOMAINS") {
		t.Errorf("missing headers:\n%s", got)
	}
	if strings.Contains(got, "hidden") || strings.Contains(got, "ORG_ID") {
		t.Errorf("untagged fields must stay out of the table:\n%s", got)
	}
	if !strings.Contains(got, "worker") {
		t.Errorf("missing row:\n%s", got)
	}
}

func TestTableRendersEmptyValuesAsADash(t *testing.T) {
	got := render(t, Table, []app{{Name: "worker"}})
	if strings.Count(got, "-") < 2 {
		t.Errorf("empty status and domains should both render as -:\n%s", got)
	}
}

func TestEmptySliceSaysSo(t *testing.T) {
	if got := render(t, Table, []app{}); !strings.Contains(got, "No results") {
		t.Errorf("got %q", got)
	}
}

// JSON is the agent-facing contract: it carries the full API shape, not the
// curated subset the table shows.
func TestJSONKeepsUntaggedFields(t *testing.T) {
	got := render(t, JSON, []app{{Name: "api", OrgID: "org-1"}})

	var back []map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatal(err)
	}
	if back[0]["org_id"] != "org-1" {
		t.Errorf("org_id missing from JSON output: %s", got)
	}
}

func TestMessageIsStructuredInJSONMode(t *testing.T) {
	var out strings.Builder
	if err := New(&out, JSON).Message("app %s created", "api"); err != nil {
		t.Fatal(err)
	}

	var back map[string]string
	if err := json.Unmarshal([]byte(out.String()), &back); err != nil {
		t.Fatalf("message must stay parseable in json mode: %v", err)
	}
	if back["message"] != "app api created" {
		t.Errorf("got %q", back["message"])
	}
}

func TestSingleStructRendersAsOneRow(t *testing.T) {
	got := render(t, Table, app{Name: "api", Status: "running"})
	if lines := strings.Count(strings.TrimSpace(got), "\n"); lines != 1 {
		t.Errorf("want header + one row, got:\n%s", got)
	}
}
