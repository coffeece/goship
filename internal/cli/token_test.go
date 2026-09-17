package cli

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func jwt(t *testing.T, exp time.Time) string {
	t.Helper()
	payload, err := json.Marshal(map[string]int64{"exp": exp.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"
}

func TestExpiry(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)

	if !expired(jwt(t, past)) {
		t.Error("a past exp should read as expired")
	}
	if expired(jwt(t, future)) {
		t.Error("a future exp should read as live")
	}

	// The server is the authority. Guessing "expired" on something we cannot
	// parse would refuse a perfectly good opaque token.
	for _, tok := range []string{"", "not-a-jwt", "a.b", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".c"} {
		if expired(tok) {
			t.Errorf("%q should not be treated as expired", tok)
		}
	}
}

// The bug this resolution exists for: `goship login` saves a copy of the token
// into our config, tsuru-client keeps refreshing its own, and the copy goes
// stale. Preferring whichever is still valid means a plain `tsuru login` heals
// the CLI instead of leaving it with a dead token.
func TestResolveTokenPrefersTheValidOne(t *testing.T) {
	t.Setenv("GOSHIP_TOKEN", "")
	stale, fresh := jwt(t, time.Now().Add(-time.Hour)), jwt(t, time.Now().Add(time.Hour))

	t.Run("env always wins", func(t *testing.T) {
		t.Setenv("GOSHIP_TOKEN", "from-env")
		if got := resolveToken(fresh); got != "from-env" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("a valid stored token is used when the platform has none", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir()) // no ~/.tsuru
		if got := resolveToken(fresh); got != fresh {
			t.Errorf("got %q, want the stored token", got)
		}
	})

	t.Run("an expired stored token is still returned when there is nothing better", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if got := resolveToken(stale); got != stale {
			t.Errorf("got %q — with no alternative, let the server reject it and say so", got)
		}
	})
}
