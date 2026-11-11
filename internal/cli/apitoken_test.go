package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// stdout must carry the token and nothing else, so it can be captured with
// $(goship token create ci).
func TestTokenCreatePrintsOnlyTheToken(t *testing.T) {
	var body map[string]any
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/auth/tokens" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"t1","name":"ci","prefix":"gsp_abcdefgh","token":"gsp_secret"}`)) //nolint:errcheck
	})

	out, err := run(t, "", "token", "create", "ci", "--expires-in-days", "30")
	if err != nil {
		t.Fatal(err)
	}
	if out != "gsp_secret\n" {
		t.Errorf("stdout = %q, want only the token", out)
	}
	if body["name"] != "ci" || body["expires_in_days"] != float64(30) {
		t.Errorf("body = %v", body)
	}
}

func TestTokenRmResolvesANameToItsID(t *testing.T) {
	var deleted string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte(`[{"id":"t1","name":"ci"},{"id":"t2","name":"laptop"}]`)) //nolint:errcheck
	})

	if _, err := run(t, "", "token", "rm", "laptop"); err != nil {
		t.Fatal(err)
	}
	if deleted != "/api/v1/auth/tokens/t2" {
		t.Errorf("deleted = %q, want the id behind the name", deleted)
	}
}

func TestTokenRmRefusesAnAmbiguousOrUnknownName(t *testing.T) {
	var deletes int
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			return
		}
		w.Write([]byte(`[{"id":"t1","name":"ci"},{"id":"t2","name":"ci"}]`)) //nolint:errcheck
	})

	if _, err := run(t, "", "token", "rm", "ci"); err == nil || !strings.Contains(err.Error(), "revoke by id") {
		t.Errorf("two tokens share the name: error = %v, want a pointer to the id", err)
	}
	if _, err := run(t, "", "token", "rm", "nope"); err == nil || !strings.Contains(err.Error(), "you have: ci, ci") {
		t.Errorf("unknown name: error = %v, want the list of names", err)
	}
	if deletes != 0 {
		t.Errorf("%d tokens revoked on a refused command", deletes)
	}
}

// A deploy token is created in the organization, confined to exactly what the
// command line names, and still printed alone on stdout.
func TestTokenCreateDeployConfinesToTheFlagsGiven(t *testing.T) {
	var path string
	var body map[string]any
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		body = nil
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"t1","name":"ci","prefix":"gsp_abcdefgh","project":"web","environment":"production","token":"gsp_deploy"}`)) //nolint:errcheck
	})

	out, err := run(t, "", "token", "create", "ci", "--deploy", "--org", "menlo", "--project", "web", "--environment", "production")
	if err != nil {
		t.Fatal(err)
	}
	if out != "gsp_deploy\n" {
		t.Errorf("stdout = %q, want only the token", out)
	}
	if path != "POST /api/v1/orgs/menlo/tokens" {
		t.Errorf("request = %s", path)
	}
	if body["name"] != "ci" || body["project"] != "web" || body["environment"] != "production" || body["expires_in_days"] != float64(90) {
		t.Errorf("body = %v", body)
	}

	if _, err := run(t, "", "token", "create", "ci", "--deploy", "--org", "menlo"); err != nil {
		t.Fatal(err)
	}
	if _, has := body["project"]; has {
		t.Errorf("body = %v; no --project must not confine the token to one", body)
	}
	if _, has := body["environment"]; has {
		t.Errorf("body = %v; no --environment must not confine the token to one", body)
	}
}

// --project without --deploy would silently mint a token that reaches
// everything its user can.
func TestTokenCreateRefusesAScopeOnAPersonalToken(t *testing.T) {
	var calls int
	stubAPI(t, func(http.ResponseWriter, *http.Request) { calls++ })

	_, err := run(t, "", "token", "create", "ci", "--project", "web")
	if err == nil || !strings.Contains(err.Error(), "--deploy") {
		t.Errorf("error = %v, want a pointer to --deploy", err)
	}
	if calls != 0 {
		t.Errorf("%d API calls on a refused command", calls)
	}
}

func TestTokensDeployListsTheOrganizationsAndRmRevokesByName(t *testing.T) {
	var seen []string
	stubAPI(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte(`[{"id":"t1","name":"web-prod","project":"web","environment":"production","created_by":"ana@menlo.test"},{"id":"t2","name":"everything"}]`)) //nolint:errcheck
	})

	out, err := run(t, "", "tokens", "--deploy", "--org", "menlo")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"web-prod", "production", "ana@menlo.test", "everything", "(all)"} {
		if !strings.Contains(out, want) {
			t.Errorf("list does not contain %q:\n%s", want, out)
		}
	}

	if _, err := run(t, "", "token", "rm", "everything", "--deploy", "--org", "menlo", "--project", "web"); err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /api/v1/orgs/menlo/tokens", "GET /api/v1/orgs/menlo/tokens?project=web", "DELETE /api/v1/orgs/menlo/tokens/t2"}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests = %v, want %v", seen, want)
	}
}
