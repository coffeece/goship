package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

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

			in, errOut := cmd.InOrStdin(), cmd.ErrOrStderr()
			reader := bufio.NewReader(in)
			if email == "" {
				var err error
				if email, err = prompt(reader, errOut, "Email: "); err != nil {
					return err
				}
			}
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
	cmd.Flags().StringVar(&email, "email", "", "email to authenticate with (prompted when omitted)")

	return cmd
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
