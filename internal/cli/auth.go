package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/coffeece/goship/internal/oauthlogin"
	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLoginCmd(app *App) *cobra.Command {
	var email string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store a token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if os.Getenv("GOSHIP_TOKEN") != "" {
				return fmt.Errorf("GOSHIP_TOKEN is set and takes precedence over a stored login; unset it first")
			}
			if email == "" {
				return browserLogin(cmd, app)
			}

			in, errOut := cmd.InOrStdin(), cmd.ErrOrStderr()
			reader := bufio.NewReader(in)
			password, err := promptPassword(reader, in, errOut)
			if err != nil {
				return err
			}

			jwt, err := app.Portal().Login(cmd.Context(), email, password)
			if err != nil {
				return err
			}
			return establishSession(cmd, app, jwt)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "sign in with a password instead of the browser")

	return cmd
}

// browserLogin signs in through the browser, against the GoShip portal itself.
func browserLogin(cmd *cobra.Command, app *App) error {
	jwt, err := loginInBrowser(cmd, app)
	if err != nil {
		return err
	}
	return establishSession(cmd, app, jwt)
}

// loginInBrowser is a package var so tests can stand in for the browser.
var loginInBrowser = func(cmd *cobra.Command, app *App) (string, error) {
	ctx, stop := interruptible(cmd.Context())
	defer stop()
	return oauthlogin.Run(ctx, app.Config.API, app.anonymousPortal(), oauthlogin.Options{Out: cmd.ErrOrStderr()})
}

// sessionDays is how long a login lasts before the person is asked again.
const sessionDays = 90

// establishSession turns a fresh sign-in into what the CLI keeps. The portal's
// own token lasts hours and cannot be revoked; an API token lasts months, shows
// up in the profile page under this machine's name, and `goship logout` can
// kill it.
func establishSession(cmd *cobra.Command, app *App, jwt string) error {
	client := app.portalWithToken(jwt)

	host, _ := os.Hostname()
	name := "goship CLI"
	if host != "" {
		name += " on " + host
	}
	app.Config.Token, app.Config.TokenID = jwt, ""
	if created, err := client.CreateAPIToken(cmd.Context(), name, sessionDays); err == nil {
		app.Config.Token, app.Config.TokenID = created.Token, created.ID
	} else if portal.IsUnauthorized(err) || portal.IsForbidden(err) {
		return err
	}
	// Any other failure keeps the short-lived token: signed in for a few
	// hours beats not signed in.
	if err := app.Config.Save(); err != nil {
		return err
	}

	me, err := client.Me(cmd.Context())
	if err != nil {
		return err
	}

	// The selected org is stored per config, not per identity, so signing in as
	// someone else leaves a pin they may have no access to — and every
	// org-scoped command then answers "forbidden" for no visible reason.
	if dropped := app.Config.ForgetOrgUnlessMember(me.Groups); dropped != "" {
		if err := app.Config.Save(); err != nil {
			return err
		}
		return app.Renderer().Message(
			"Logged in as %s. You are not a member of %q, so it is no longer selected — pick one with `goship org use`.",
			me.Email, dropped)
	}
	return app.Renderer().Message("Logged in as %s.", me.Email)
}

func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Sign out and revoke this machine's token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Revoking is best effort: offline, or a token already revoked
			// from the profile page, must not keep someone signed in locally.
			if app.Config.TokenID != "" && app.Config.Token != "" {
				_ = app.portalWithToken(app.Config.Token).RevokeAPIToken(cmd.Context(), app.Config.TokenID)
			}
			app.Config.Token, app.Config.TokenID = "", ""
			if err := app.Config.Save(); err != nil {
				return err
			}
			return app.Renderer().Message("Logged out.")
		},
	}
}

func newWhoamiCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the authenticated user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			me, err := app.Portal().Me(cmd.Context())
			if err != nil {
				return err
			}
			return app.Renderer().Render(me)
		},
	}
}

// prompt reads one line. The reader is shared across prompts: a fresh
// bufio.Reader per call would swallow whatever the previous one buffered,
// which loses the password when both are piped in.
func prompt(r *bufio.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptPassword hides the input when stdin is a terminal, and reads a plain
// line when it is not so `goship login --email … < secret` still works.
func promptPassword(r *bufio.Reader, in io.Reader, out io.Writer) (string, error) {
	f, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) || r.Buffered() > 0 {
		return prompt(r, out, "Password: ")
	}

	fmt.Fprint(out, "Password: ")
	data, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(out)
	return string(data), err
}
