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
