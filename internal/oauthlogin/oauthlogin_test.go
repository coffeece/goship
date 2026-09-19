package oauthlogin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// fakePortal plays both halves of the portal: the browser visit to
// /oauth/authorize (answered by sending the "browser" straight back, as a
// successful sign-in would) and the token exchange.
type fakePortal struct {
	challenge   string
	gotVerifier string
	gotRedirect string
	tamperState bool
}

func (f *fakePortal) visit(authURL string) error {
	u, err := url.Parse(authURL)
	if err != nil {
		return err
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
		return errors.New("authorize URL is missing PKCE or response_type")
	}
	f.challenge = q.Get("code_challenge")
	state := q.Get("state")
	if f.tamperState {
		state = "someone-elses-state"
	}
	resp, err := http.Get(q.Get("redirect_uri") + "?code=the-code&state=" + url.QueryEscape(state))
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (f *fakePortal) ExchangeCode(_ context.Context, code, redirectURI, verifier string) (string, error) {
	f.gotVerifier, f.gotRedirect = verifier, redirectURI
	if code != "the-code" {
		return "", errors.New("wrong code")
	}
	return "jwt-from-portal", nil
}

func TestRunCompletesTheLoopbackFlowWithPKCE(t *testing.T) {
	portal := &fakePortal{}
	var out strings.Builder

	token, err := Run(context.Background(), "https://api.example.test", portal, Options{Open: portal.visit, Out: &out})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if token != "jwt-from-portal" {
		t.Errorf("token = %q", token)
	}

	// The verifier sent to the token endpoint must be the one behind the
	// challenge shown to the browser.
	sum := sha256.Sum256([]byte(portal.gotVerifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != portal.challenge {
		t.Error("the verifier does not match the challenge")
	}
	if len(portal.gotVerifier) < 43 {
		t.Errorf("verifier is %d chars, RFC 7636 asks for at least 43", len(portal.gotVerifier))
	}
	if !strings.HasPrefix(portal.gotRedirect, "http://127.0.0.1:") || !strings.HasSuffix(portal.gotRedirect, "/callback") {
		t.Errorf("redirect_uri = %q", portal.gotRedirect)
	}
	if !strings.Contains(out.String(), "https://api.example.test/oauth/authorize?") {
		t.Errorf("the URL was not printed for a machine with no browser: %q", out.String())
	}
}

// A callback carrying another flow's state is somebody else's login.
func TestRunRejectsACallbackWithTheWrongState(t *testing.T) {
	portal := &fakePortal{tamperState: true}

	_, err := Run(context.Background(), "https://api.example.test", portal, Options{Open: portal.visit})
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Errorf("error = %v, want a state mismatch", err)
	}
	if portal.gotVerifier != "" {
		t.Error("the code was exchanged despite the state mismatch")
	}
}

func TestRunGivesUpWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Run(ctx, "https://api.example.test", &fakePortal{}, Options{Open: func(string) error { return nil }})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
