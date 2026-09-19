// Package oauthlogin signs a person in through their browser: the
// authorization-code flow for native apps (RFC 8252) with PKCE (RFC 7636),
// against the GoShip portal.
package oauthlogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

// Timeout is how long a login waits for the browser to come back.
const Timeout = 5 * time.Minute

// Exchanger trades an authorization code for an access token.
type Exchanger interface {
	ExchangeCode(ctx context.Context, code, redirectURI, verifier string) (string, error)
}

// Options are the injectable parts of a login.
type Options struct {
	// Open shows a URL to the person. Nil opens the system browser.
	Open func(url string) error
	// Out receives the instructions, including the URL for when no browser
	// could be opened.
	Out io.Writer
}

// Run performs the login against the portal at base and returns the access
// token it issued.
func Run(ctx context.Context, base string, ex Exchanger, opts Options) (string, error) {
	if opts.Open == nil {
		opts.Open = OpenBrowser
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}

	// The loopback port is picked by the OS; the portal allows any port on
	// 127.0.0.1, as RFC 8252 §7.3 asks of it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("opening a local port for the login to return to: %w", err)
	}
	defer ln.Close() //nolint:errcheck
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	state, err := randomString(24)
	if err != nil {
		return "", err
	}
	verifier, err := randomString(48)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	type callback struct {
		code string
		err  error
	}
	done := make(chan callback, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			var cb callback
			switch {
			case subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1:
				cb.err = errors.New("the login came back with the wrong state; it was not started by this command")
			case q.Get("code") == "":
				cb.err = errors.New("the login came back without a code")
			default:
				cb.code = q.Get("code")
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if cb.err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, page("Login failed", "Go back to the terminal for details."))
			} else {
				fmt.Fprint(w, page("You are signed in", "You can close this tab and go back to the terminal."))
			}
			select {
			case done <- cb:
			default:
			}
		}),
	}
	go srv.Serve(ln)  //nolint:errcheck
	defer srv.Close() //nolint:errcheck

	authURL := base + "/oauth/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {"goship-cli"},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	fmt.Fprintf(opts.Out, "Opening your browser to sign in. If nothing opens, visit:\n\n  %s\n\n", authURL)
	if err := opts.Open(authURL); err != nil {
		fmt.Fprintf(opts.Out, "(could not open a browser: %v)\n", err)
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("gave up waiting for the browser login: %w", ctx.Err())
	case cb := <-done:
		if cb.err != nil {
			return "", cb.err
		}
		return ex.ExchangeCode(ctx, cb.code, redirectURI, verifier)
	}
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func page(title, body string) string {
	return `<!doctype html><meta charset="utf-8"><title>GoShip</title>` +
		`<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:20vh auto;padding:0 1rem">` +
		`<h1 style="font-size:1.4rem">` + title + `</h1><p>` + body + `</p></body>`
}

// OpenBrowser opens url in the system browser.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
