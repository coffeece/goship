package cli

import "testing"

func TestResolveTokenPrefersTheEnvironment(t *testing.T) {
	t.Setenv("GOSHIP_TOKEN", "")
	if got := resolveToken("stored"); got != "stored" {
		t.Errorf("got %q, want the stored token", got)
	}

	t.Setenv("GOSHIP_TOKEN", "gsp_from_env")
	if got := resolveToken("stored"); got != "gsp_from_env" {
		t.Errorf("got %q, want GOSHIP_TOKEN to win", got)
	}
}
