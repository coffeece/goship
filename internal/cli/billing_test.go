package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The trial ends far enough ahead, or far enough back, that the test does not
// depend on today's date.
const (
	billingLater   = "2099-01-02T12:00:00Z"
	billingEarlier = "2020-01-02T12:00:00Z"
	laterLine      = "Billing starts when the trial ends on 2 Jan 2099."
	earlierLine    = "Billing starts shortly."
)

// billingAPI answers the one request a command makes with reply, and fails
// the test when it is not the exact method and path expected.
func billingAPI(t *testing.T, method, path, reply string) {
	t.Helper()
	stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) //nolint:errcheck
		if r.Method != method || r.URL.Path != path {
			t.Errorf("got %s %s, want %s %s", r.Method, r.URL.Path, method, path)
		}
		w.Write([]byte(reply)) //nolint:errcheck
	})
}

const (
	appsPath      = "/api/v1/orgs/acme/projects/atlas/environments/production/apps"
	instancesPath = "/api/v1/orgs/acme/projects/atlas/environments/production/services/postgresql/instances"
)

func TestAppCreateSaysWhenBillingStarts(t *testing.T) {
	for _, tc := range []struct {
		name, reply, want string
	}{
		{"held", `{"name":"api","billing_starts_at":"` + billingLater + `"}`, laterLine},
		{"trial already over", `{"name":"api","billing_starts_at":"` + billingEarlier + `"}`, earlierLine},
		{"not held", `{"name":"api"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billingAPI(t, http.MethodPost, appsPath, tc.reply)
			out, err := run(t, "", "app", "create", "api", "--platform", "go")
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if strings.Contains(out, "Billing") {
					t.Errorf("output mentions billing:\n%s", out)
				}
				return
			}
			if !strings.HasSuffix(out, tc.want+"\n") || strings.Count(out, "Billing") != 1 {
				t.Errorf("output lacks the billing line %q:\n%s", tc.want, out)
			}
		})
	}
}

// -o json is one document: the field is in the app, with no line added.
func TestAppCreateJSONPassesBillingStartsAtThrough(t *testing.T) {
	billingAPI(t, http.MethodPost, appsPath, `{"name":"api","billing_starts_at":"`+billingLater+`"}`)
	out, err := run(t, "", "app", "create", "api", "--platform", "go", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["billing_starts_at"] != billingLater {
		t.Errorf("stdout is not the held app: %v\n%s", err, out)
	}
}

func TestAppPlanSaysWhenBillingStarts(t *testing.T) {
	const path = appsPath + "/api/plan"
	for _, tc := range []struct {
		name, reply, want string
	}{
		{"held", `{"status":"updated","billing_starts_at":"` + billingLater + `"}`, laterLine},
		{"trial already over", `{"status":"updated","billing_starts_at":"` + billingEarlier + `"}`, earlierLine},
		{"not held", `{"status":"updated"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billingAPI(t, http.MethodPut, path, tc.reply)
			out, err := run(t, "", "app", "plan", "api", "app-small")
			if err != nil {
				t.Fatal(err)
			}
			want := "App api moved to plan app-small.\n"
			if tc.want != "" {
				want += tc.want + "\n"
			}
			if out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
		})
	}
}

func TestAppPlanJSONCarriesBillingStartsAt(t *testing.T) {
	billingAPI(t, http.MethodPut, appsPath+"/api/plan", `{"status":"updated","billing_starts_at":"`+billingLater+`"}`)
	out, err := run(t, "", "app", "plan", "api", "app-small", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["billing_starts_at"] != billingLater || got["message"] != "App api moved to plan app-small." {
		t.Errorf("stdout = %s (%v)", out, err)
	}

	// Not held: the document is the message alone, as it always was.
	billingAPI(t, http.MethodPut, appsPath+"/api/plan", `{"status":"updated"}`)
	if out, err = run(t, "", "app", "plan", "api", "app-small", "-o", "json"); err != nil || out != `{"message":"App api moved to plan app-small."}`+"\n" {
		t.Errorf("stdout = %q (%v)", out, err)
	}
}

func TestDBCreateSaysWhenBillingStarts(t *testing.T) {
	for _, tc := range []struct {
		name, reply, want string
	}{
		{"held", `{"status":"created","billing_starts_at":"` + billingLater + `"}`, laterLine},
		{"trial already over", `{"status":"created","billing_starts_at":"` + billingEarlier + `"}`, earlierLine},
		{"not held", `{"status":"created"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billingAPI(t, http.MethodPost, instancesPath, tc.reply)
			out, err := run(t, "", "db", "create", "shop", "--plan", "db-micro")
			if err != nil {
				t.Fatal(err)
			}
			want := "Database shop requested on plan db-micro.\n"
			if tc.want != "" {
				want += tc.want + "\n"
			}
			if out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
		})
	}
}

func TestDBCreateJSONCarriesBillingStartsAt(t *testing.T) {
	billingAPI(t, http.MethodPost, instancesPath, `{"status":"created","billing_starts_at":"`+billingLater+`"}`)
	out, err := run(t, "", "db", "create", "shop", "--plan", "db-micro", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["billing_starts_at"] != billingLater || got["message"] != "Database shop requested on plan db-micro." {
		t.Errorf("stdout = %s (%v)", out, err)
	}
}

// The line comes before the address, which stays the last thing printed.
func TestDeploySaysWhenBillingStarts(t *testing.T) {
	for _, tc := range []struct {
		name, held, want string
	}{
		{"held", `,"billing_starts_at":"` + billingLater + `"`, laterLine},
		{"trial already over", `,"billing_starts_at":"` + billingEarlier + `"`, earlierLine},
		{"not held", ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubAPIWithOrg(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == appsPath+"/quake/deploys":
					io.Copy(io.Discard, r.Body) //nolint:errcheck
					w.Write([]byte(stepStream)) //nolint:errcheck
				case r.Method == http.MethodGet && r.URL.Path == appsPath+"/quake":
					w.Write([]byte(`{"name":"quake","platform":"go","addresses":["https://quake-x1.apps.goship.sh"]` + tc.held + `}`)) //nolint:errcheck
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
			})

			out, err := run(t, "", "deploy", quakeDir(t))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if last := lines[len(lines)-1]; last != "https://quake-x1.apps.goship.sh" {
				t.Errorf("the address is not the last line:\n%s", out)
			}
			if tc.want == "" {
				if strings.Contains(out, "Billing") {
					t.Errorf("output mentions billing:\n%s", out)
				}
				return
			}
			if len(lines) < 2 || lines[len(lines)-2] != tc.want || strings.Count(out, "Billing") != 1 {
				t.Errorf("output lacks %q before the address:\n%s", tc.want, out)
			}
		})
	}
}
