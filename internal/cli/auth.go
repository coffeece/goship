package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/coffeece/goship/internal/tsuru"
	"github.com/spf13/cobra"
	tsuruauth "github.com/tsuru/tsuru-client/tsuru/auth"
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

			token, err := app.Portal().Login(cmd.Context(), email, password)
			if err != nil {
				return err
			}

			app.Config.Token = token
			if err := app.Config.Save(); err != nil {
				return err
			}
			return app.Renderer().Message("Logged in as %s.", email)
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "sign in with a password instead of the browser")

	return cmd
}

// browserLogin runs the platform's OpenID Connect flow. The access token it
// returns was issued by the GoShip portal, so storing it here authenticates
// both the GoShip API and the streaming commands, which read tsuru-client's
// own credentials. One login, both surfaces.
func browserLogin(cmd *cobra.Command, app *App) error {
	if err := tsuru.Setup(app.Config.Tsuru, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
		return err
	}
	defer tsuru.Flush()

	if err := tsuru.Run(&tsuruauth.Login{}, nil, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
		return err
	}

	token, err := tsuru.Token()
	if err != nil {
		return err
	}
	app.Config.Token = token
	if err := app.Config.Save(); err != nil {
		return err
	}

	me, err := app.Portal().Me(cmd.Context())
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
		if err := app.Renderer().Message(
			"Logged in as %s. You are not a member of %q, so it is no longer selected — pick one with `goship org use`.",
			me.Email, dropped); err != nil {
			return err
		}
		return nil
	}
	return app.Renderer().Message("Logged in as %s.", me.Email)
}

func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Discard the stored token",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app.Config.Token = ""
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
