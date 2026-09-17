// Package tsuru adapts tsuru-client commands for the handful of operations
// whose output is a stream rather than data: deploy, logs, run, shell and
// release rollback. Everything else in the CLI talks to the GoShip API and
// renders through internal/render.
//
// These keep tsuru-client's own credentials (TSURU_TOKEN or ~/.tsuru/token).
// The GoShip token is not interchangeable: Tsuru's oauth scheme resolves a
// bearer token against its own oauth2_tokens collection rather than
// revalidating it against the issuer, so only a token minted through Tsuru's
// own login is accepted.
package tsuru

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	goclient "github.com/tsuru/go-tsuruclient/pkg/client"
	goconfig "github.com/tsuru/go-tsuruclient/pkg/config"
	tsurucmd "github.com/tsuru/tsuru-client/tsuru/cmd"
	tsuruhttp "github.com/tsuru/tsuru-client/tsuru/http"
)

// ProtocolVersion is the tsuru-client compatibility level reported to the API.
// The API rejects clients below its supported floor, so GoShip's own release
// number must never be the one compared. Bump it with the tsuru-client
// dependency.
const ProtocolVersion = "1.31.0"

// Setup points tsuru-client at the target and installs the authenticated HTTP
// client its commands read from a package-level variable.
func Setup(target string, stdout, stderr io.Writer) error {
	if os.Getenv("TSURU_TARGET") == "" {
		if err := os.Setenv("TSURU_TARGET", target); err != nil {
			return err
		}
	}

	roundTripper, tokenProvider, err := goclient.RoundTripperAndTokenProvider()
	if err != nil {
		return fmt.Errorf("reading the platform credentials: %w", err)
	}
	tsuruhttp.AuthenticatedClient = tsuruhttp.NewTerminalClient(tsuruhttp.TerminalClientOptions{
		RoundTripper:  roundTripper,
		ClientName:    "goship",
		ClientVersion: ProtocolVersion,
		Stdout:        stdout,
		Stderr:        stderr,
	})
	goconfig.DefaultTokenProvider = tokenProvider
	return nil
}

// Flush persists any credential refresh the commands performed.
func Flush() { goconfig.SaveChangesWithTimeout() }

// Run executes a tsuru-client command against the given streams.
//
// Interrupting is forwarded to the command when it supports cancellation.
// Without this, Ctrl-C only kills the client: the platform keeps building and
// holds the app's event lock, so the next deploy fails with "event locked" and
// the user has no way to clear it.
func Run(c tsurucmd.Command, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	ctx := &tsurucmd.Context{Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr}

	if cancelable, ok := c.(tsurucmd.Cancelable); ok {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(signals)

		done := make(chan struct{})
		defer close(done)

		go func() {
			for {
				select {
				case <-done:
					return
				case <-signals:
					fmt.Fprintln(stderr, "\nCancelling on the server — do not kill this process, or the app stays locked.")
					if err := cancelable.Cancel(*ctx); err != nil {
						fmt.Fprintf(stderr, "Could not cancel: %v\n", err)
					}
				}
			}
		}()
	}

	return c.Run(ctx)
}

// Token returns the credential tsuru-client holds after a login. It is the
// access token the platform received from the GoShip portal, so it also
// authenticates against the GoShip API.
func Token() (string, error) {
	if v2, err := goconfig.ReadTokenV2(); err == nil && v2 != nil && v2.OAuth2Token != nil {
		if v2.OAuth2Token.AccessToken != "" {
			return v2.OAuth2Token.AccessToken, nil
		}
	}
	return goconfig.ReadTokenV1()
}
