package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// noJSON refuses --output json on commands whose output is a live stream of
// text: there is nothing to format, and ignoring the flag would hide a mistake.
func noJSON(app *App, cmd *cobra.Command) error {
	if app.Global.Output == render.JSON {
		return fmt.Errorf("%s streams its output; --output json is not supported", cmd.Name())
	}
	return nil
}

// interruptible returns a context that ends on Ctrl-C, so a followed log or a
// long command stops cleanly instead of being killed mid-line.
func interruptible(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
}

func newLogsCmd(app *App) *cobra.Command {
	var (
		appName string
		opts    portal.LogOptions
		noDate  bool
	)
	cmd := &cobra.Command{
		Use:   "logs --app <app>",
		Short: "Show an app's logs",
		Args:  cobra.NoArgs,
	}
	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (required)")
	f.IntVarP(&opts.Lines, "lines", "l", 100, "how many past lines to show")
	f.BoolVarP(&opts.Follow, "follow", "f", false, "keep printing new lines until interrupted")
	f.StringVarP(&opts.Source, "source", "s", "", "only lines from this source, e.g. web")
	f.StringSliceVarP(&opts.Units, "unit", "u", nil, "only lines from this unit (repeatable)")
	f.BoolVar(&noDate, "no-date", false, "leave the timestamp out")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, _ []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		ctx, stop := interruptible(cmd.Context())
		defer stop()

		out := cmd.OutOrStdout()
		err := app.Portal().Logs(ctx, t.Org, t.Project, t.Environment, appName, opts, func(l portal.LogEntry) error {
			prefix := fmt.Sprintf("[%s][%s]", l.Source, l.Unit)
			if !noDate {
				prefix = l.Date.Local().Format("2006-01-02 15:04:05") + " " + prefix
			}
			_, err := fmt.Fprintf(out, "%s: %s\n", prefix, strings.TrimRight(l.Message, "\n"))
			return err
		})
		// Ctrl-C is how a follow is meant to end.
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return explainStreamError(err, "log stream", false)
	})
	return cmd
}

func newRunCmd(app *App) *cobra.Command {
	var (
		appName        string
		once, isolated bool
	)
	cmd := &cobra.Command{
		Use:   "run --app <app> -- <command> [args...]",
		Short: "Run a one-off command in the app's image",
		Long: "Runs a command where the app's code and environment are. By default it\n" +
			"runs in every unit; --once picks one, --isolated starts a fresh container.\n\n" +
			"Arguments reach the app as typed. For pipes, && or $VARS expanded in the app,\n" +
			"pass the whole line as one quoted argument: goship run -a api 'cd x && make'.",
		Args: cobra.MinimumNArgs(1),
	}
	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (required)")
	f.BoolVar(&once, "once", false, "run in a single unit instead of all of them")
	f.BoolVar(&isolated, "isolated", false, "run in a new container rather than a serving unit")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		ctx, stop := interruptible(cmd.Context())
		defer stop()
		err := app.Portal().Run(ctx, t.Org, t.Project, t.Environment, appName, shellJoin(args), once, isolated, cmd.OutOrStdout())
		return explainStreamError(err, "command", false)
	})
	return cmd
}

// shellJoin turns the arguments back into the line the platform's shell runs.
// A single argument is that line already; several are quoted one by one so
// the remote shell splits them where the local one did.
func shellJoin(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./_-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func newReleasesCmd(app *App) *cobra.Command {
	var (
		appName string
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "releases --app <app>",
		Short: "List an app's releases",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVarP(&appName, "app", "a", "", "app name (required)")
	cmd.Flags().IntVar(&limit, "limit", 20, "how many releases to show")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, _ []string) error {
		deploys, err := app.Portal().Deploys(cmd.Context(), t.Org, t.Project, t.Environment, appName, limit)
		if err != nil {
			return err
		}
		return app.Renderer().Render(deploys)
	})
	cmd.AddCommand(newReleaseLogCmd(app))
	return cmd
}

// releaseLogHistory is how far back `releases log` looks for a version.
const releaseLogHistory = 100

func newReleaseLogCmd(app *App) *cobra.Command {
	var appName string
	cmd := &cobra.Command{
		Use:   "log [version] --app <app>",
		Short: "Print what a release wrote while it built and started",
		Long: "Prints the stored output of a deploy or rollback: the newest release of the app, or the version given,\n" +
			"e.g. `goship releases log v12 -a api`. A deploy that failed before becoming a version is the newest\n" +
			"release until the next one, so `goship releases log -a api` right after a failed deploy shows why it failed.",
		Args: cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringVarP(&appName, "app", "a", "", "app name (required)")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		which := ""
		if len(args) == 1 {
			which = args[0]
		}
		deploys, err := app.Portal().Deploys(cmd.Context(), t.Org, t.Project, t.Environment, appName, releaseLogHistory)
		if err != nil {
			return err
		}
		d, ok := portal.FindDeploy(deploys, which)
		if !ok {
			if which == "" {
				return fmt.Errorf("%s has no releases yet", appName)
			}
			return fmt.Errorf("%s has no release %s in its last %d — `goship releases -a %s` lists them", appName, which, releaseLogHistory, appName)
		}
		detail, err := app.Portal().DeployDetail(cmd.Context(), t.Org, t.Project, t.Environment, appName, d.ID)
		if err != nil {
			return err
		}
		if app.Global.Output == render.JSON {
			return app.Renderer().Render(detail)
		}
		switch {
		case detail.LogRestricted:
			return fmt.Errorf("release logs of %s are shown to people who can deploy it; ask a project admin for the developer role", appName)
		case !detail.LogAvailable:
			return fmt.Errorf("no log was kept for %s of %s: it was released before GoShip started keeping release logs", releaseName(detail.Deploy), appName)
		}
		// The log goes to stdout alone, so it can be piped; what it belongs
		// to goes to stderr.
		fmt.Fprintf(cmd.ErrOrStderr(), "%s of %s · %s\n", releaseName(detail.Deploy), appName, releaseOutcome(detail.Deploy))
		printPhases(cmd.ErrOrStderr(), detail.Phases)
		if detail.LogTruncated {
			fmt.Fprintln(cmd.ErrOrStderr(), "This log was long, so its middle was left out. The beginning and the end are complete.")
		}
		_, err = io.WriteString(cmd.OutOrStdout(), printableLog(detail.Log))
		return err
	})
	return cmd
}

// phaseRows is the display order of a release's phases; depth indents a
// phase under its parent.
var phaseRows = []struct {
	key, label string
	depth      int
}{
	{"build", "Build", 0},
	{"build.script", "Build script", 1},
	{"build.script.toolchain", "Go toolchain", 2},
	{"build.script.modules", "Dependencies", 2},
	{"build.script.compile", "Compile and link", 2},
	{"build.script.hooks", "Build hooks", 2},
	{"build.export", "Image export", 1},
	{"build.push", "Image push", 1},
	{"build.other", "Other", 1},
	{"release", "Release", 0},
	{"route", "Route", 0},
}

// printPhases writes the Timings block: one row per phase the release
// recorded, and nothing when it recorded none.
func printPhases(w io.Writer, phases map[string]float64) {
	type row struct{ label, dur string }
	var rows []row
	width := 0
	for _, p := range phaseRows {
		secs, ok := phases[p.key]
		if !ok {
			continue
		}
		label := strings.Repeat("  ", p.depth) + p.label
		rows = append(rows, row{label, phaseDuration(secs)})
		width = max(width, len(label))
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "Timings")
	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, r.label, r.dur)
	}
}

// phaseDuration reads 100 as "1m 40s" and 11.7 as "11.7s".
func phaseDuration(secs float64) string {
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	total := int(secs + 0.5)
	return fmt.Sprintf("%dm %ds", total/60, total%60)
}

// printableLog is a stored log as it is safe to print: a build writes what it
// likes, and whoever reads the log later may not be who ran the build, so
// escape sequences and control characters never reach their terminal.
func printableLog(log string) string {
	lines := strings.SplitAfter(log, "\n")
	var b strings.Builder
	for _, line := range lines {
		text := strings.TrimSuffix(line, "\n")
		b.WriteString(printable(text))
		if len(text) != len(line) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func releaseName(d portal.Deploy) string {
	if d.Version > 0 {
		return fmt.Sprintf("v%d", d.Version)
	}
	return "the deploy that did not become a version"
}

func releaseOutcome(d portal.Deploy) string {
	if d.Error != "" {
		return "failed: " + d.Error
	}
	return "succeeded"
}

func newRollbackCmd(app *App) *cobra.Command {
	var appName string
	cmd := &cobra.Command{
		Use:   "rollback <version> --app <app>",
		Short: "Release a previous version again",
		Long:  "Releases a version from `goship releases` again, e.g. `goship rollback v12 -a api`.",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringVarP(&appName, "app", "a", "", "app name (required)")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, args []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		version := args[0]
		// `goship releases` prints 12; the API speaks v12.
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		p := newProgress(cmd.OutOrStdout(), app.Global.Verbose)
		p.Header("Rolling back %s to %s", appName, version)
		err := app.Portal().Rollback(cmd.Context(), t.Org, t.Project, t.Environment, appName, version, p.Event)
		p.Finish(err)
		ran := isOperationError(err)
		err = explainStreamError(err, "rollback of "+appName, true)
		// Running a production rollback again is not how to read its log.
		if ran {
			err = fmt.Errorf("%w\nHistory: goship releases -a %s", err, appName)
		}
		return err
	})
	return cmd
}

// firstWrite passes writes through and closes seen on the first one.
type firstWrite struct {
	w    io.Writer
	seen chan struct{}
	once sync.Once
}

func (f *firstWrite) Write(p []byte) (int, error) {
	if len(p) > 0 {
		f.once.Do(func() { close(f.seen) })
	}
	return f.w.Write(p)
}

// pipedShellGrace is how long a shell fed from a pipe may keep running after
// its input ends. A script normally finishes with `exit`, which closes the
// shell from the other side well before this; the cap is for one that does not.
// It has to cover an isolated container's start-up, which happens after the
// input has already been sent.
var pipedShellGrace = 60 * time.Second

func newShellCmd(app *App) *cobra.Command {
	var (
		appName  string
		unit     string
		isolated bool
	)
	cmd := &cobra.Command{
		Use:   "shell --app <app>",
		Short: "Open a shell in one of the app's units",
		Long: "Opens an interactive shell where the app's code and environment are. It\n" +
			"lands in a serving unit; --isolated starts a fresh container instead, which\n" +
			"also works for an app that is paused or crashing.",
		Args: cobra.NoArgs,
	}
	f := cmd.Flags()
	f.StringVarP(&appName, "app", "a", "", "app name (required)")
	f.StringVarP(&unit, "unit", "u", "", "unit to open the shell in (see `goship app info`)")
	f.BoolVar(&isolated, "isolated", false, "open the shell in a new container rather than a serving unit")
	_ = cmd.MarkFlagRequired("app")

	cmd.RunE = targetRunE(app, func(cmd *cobra.Command, t Target, _ []string) error {
		if err := noJSON(app, cmd); err != nil {
			return err
		}
		opts := portal.ShellOptions{Unit: unit, Isolated: isolated, Term: os.Getenv("TERM")}

		// A real terminal is switched to raw mode so keys, not lines, travel;
		// piped input (`echo ls | goship shell`) is sent as it comes.
		in, _ := cmd.InOrStdin().(*os.File)
		interactive := in != nil && term.IsTerminal(int(in.Fd()))
		if interactive {
			opts.Width, opts.Height, _ = term.GetSize(int(in.Fd()))
		}

		conn, err := app.Portal().Shell(cmd.Context(), t.Org, t.Project, t.Environment, appName, opts)
		if err != nil {
			return err
		}
		defer conn.Close() //nolint:errcheck

		if interactive {
			state, err := term.MakeRaw(int(in.Fd()))
			if err != nil {
				return fmt.Errorf("switching the terminal to raw mode: %w", err)
			}
			defer term.Restore(int(in.Fd()), state) //nolint:errcheck
		}

		// Read once: the goroutines below may outlive the command, and must not
		// look at the package var after it has returned.
		grace := pipedShellGrace
		output := make(chan error, 1)
		input := make(chan error, 1)
		ready := &firstWrite{w: cmd.OutOrStdout(), seen: make(chan struct{})}
		go func() {
			_, err := io.Copy(ready, conn)
			output <- err
		}()
		go func() {
			if !interactive {
				// A script is sent in one go, and whatever reaches the unit
				// before its shell is up is lost — an isolated container takes
				// a while to start. The first output, the prompt, says it is
				// listening.
				select {
				case <-ready.seen:
				case <-time.After(grace):
				}
			}
			_, err := io.Copy(conn, cmd.InOrStdin())
			input <- err
		}()

		select {
		case err = <-output:
		case err = <-input:
			if err == nil && !interactive {
				// Piped input ran out: let the commands finish and the shell
				// close itself rather than hanging up on them mid-output.
				select {
				case err = <-output:
				case <-time.After(grace):
				}
			}
		}
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return err
	})
	return cmd
}

func newStreamCmds(app *App) []*cobra.Command {
	return []*cobra.Command{
		newLogsCmd(app),
		newRunCmd(app),
		newShellCmd(app),
		newReleasesCmd(app),
		newRollbackCmd(app),
	}
}
