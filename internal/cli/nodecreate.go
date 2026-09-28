package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// nodePollInterval is how often followNode checks a provisioning node; a
// package var so tests don't wait for the real thing.
var nodePollInterval = 3 * time.Second

// stdinIsTTY says whether standard input is a terminal, which is what tells
// `node create` it may prompt for a missing --region or --size instead of
// failing outright. It must run before stdin is wrapped in a bufio.Reader,
// which hides the *os.File. A package var so tests, whose stdin is a
// strings.Reader, can force it either way.
var stdinIsTTY = func(cmd *cobra.Command) bool {
	in := cmd.InOrStdin()
	if in == os.Stdin {
		return term.IsTerminal(int(os.Stdin.Fd()))
	}
	f, ok := in.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func newNodeCreateCmd(app *App) *cobra.Command {
	var cloudRef, region, size, host, sshUser, sshKeyFile string
	var port int
	var noWait bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a node: --cloud provisions one on a connected account, --host connects a machine you already have",
		Long: "Creates a node either by provisioning a fresh machine on a connected cloud\n" +
			"account (--cloud), or by connecting one you already run (--host). Exactly\n" +
			"one of the two is required.\n\n" +
			"With --cloud on a terminal, a missing --region or --size is asked for\n" +
			"interactively from the account's live catalog; piped in or scripted, both\n" +
			"must be passed.",
		Args: cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			if (cloudRef == "") == (host == "") {
				return errors.New("pass exactly one of --cloud or --host")
			}
			if err := rejectMixedFlags(cmd, cloudRef != ""); err != nil {
				return err
			}

			// Asked before the wrap below, which hides the *os.File the check
			// needs.
			tty := stdinIsTTY(cmd)

			// A single bufio.Reader shared for the life of the command: a fresh
			// one per prompt would swallow whatever the previous prompt's read
			// already buffered off stdin (see prompt's doc comment), which would
			// lose the answer to --size right after --region was asked.
			cmd.SetIn(bufio.NewReader(cmd.InOrStdin()))

			name := args[0]
			req := portal.CreateNodeRequest{Name: name}
			var accountLabel, headerRegion, headerSize string

			switch {
			case cloudRef != "":
				account, err := resolveCloudAccount(cmd.Context(), app.Portal(), org, cloudRef)
				if err != nil {
					return err
				}
				req.CloudAccountID = account.ID
				accountLabel = account.Label

				if region == "" {
					if !tty {
						return errors.New("--region is required (not a terminal to prompt)")
					}
					regions, err := app.Portal().CloudRegions(cmd.Context(), org, account.ID)
					if err != nil {
						return err
					}
					chosen, err := pick(cmd, "region", regions, regionChoiceLabel)
					if err != nil {
						return err
					}
					region = chosen.Slug
				}
				req.Region = region
				headerRegion = region

				if size == "" {
					if !tty {
						return errors.New("--size is required (not a terminal to prompt)")
					}
					sizes, err := app.Portal().CloudSizes(cmd.Context(), org, account.ID, region)
					if err != nil {
						return err
					}
					chosen, err := pick(cmd, "size", sizes, sizeChoiceLabel)
					if err != nil {
						return err
					}
					size = chosen.Slug
				}
				req.Size = size
				headerSize = size

			case host != "":
				if sshKeyFile == "" {
					return errors.New("--ssh-key is required with --host")
				}
				key, err := os.ReadFile(sshKeyFile)
				if err != nil {
					return err
				}
				req.Type = "vps"
				req.Host = host
				req.Port = port
				req.SSHUser = sshUser
				req.SSHKey = string(key)
				accountLabel = "your machine"
				headerRegion = fmt.Sprintf("%s:%d", host, port)
				headerSize = sshUser
			}

			created, err := app.Portal().CreateNode(cmd.Context(), org, req)
			if err != nil {
				return err
			}

			// JSON is for scripts: hand back the created node, nothing more.
			if app.Global.Output == render.JSON {
				return app.Renderer().Render(created)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Creating node %s on %s (%s, %s)\n", name, accountLabel, headerRegion, headerSize)
			if noWait {
				return app.Renderer().Message(
					"Node %s requested; not waiting for it to come up (--no-wait). `goship nodes` shows its status.",
					created.Name)
			}

			ctx, stop := interruptible(cmd.Context())
			defer stop()
			return followNode(ctx, app.Portal(), followOpts{
				out: cmd.OutOrStdout(), errw: cmd.ErrOrStderr(), verbose: app.Global.Verbose,
				org: org, id: created.ID, name: created.Name, timeout: timeout,
			})
		}),
	}

	f := cmd.Flags()
	f.StringVar(&cloudRef, "cloud", "", "cloud account id or label to provision the machine on")
	f.StringVar(&region, "region", "", "region slug (see `goship cloud regions`); prompted on a terminal when omitted")
	f.StringVar(&size, "size", "", "size slug (see `goship cloud sizes`); prompted on a terminal when omitted")
	f.StringVar(&host, "host", "", "hostname or IP of a machine you already have")
	f.IntVar(&port, "port", 22, "SSH port")
	f.StringVar(&sshUser, "ssh-user", "root", "SSH user")
	f.StringVar(&sshKeyFile, "ssh-key", "", "path to a private key with access to the host")
	f.BoolVar(&noWait, "no-wait", false, "return once the machine is requested, without following provisioning")
	f.DurationVar(&timeout, "timeout", 30*time.Minute, "how long to follow provisioning before giving up; 0 follows until it ends (the node keeps provisioning either way)")

	return cmd
}

// regionChoiceLabel is a portal.CloudRegion as one line of a `pick` prompt.
func regionChoiceLabel(r portal.CloudRegion) string {
	return fmt.Sprintf("%s — %s, %s", r.Slug, r.Label, r.Country)
}

// sizeChoiceLabel is a portal.CloudSize as one line of a `pick` prompt,
// reusing the same row shown by `goship cloud sizes` so the two never drift.
// A price the catalog lacks is left out rather than shown as a bare dash.
func sizeChoiceLabel(s portal.CloudSize) string {
	row := newSizeRow(s)
	label := fmt.Sprintf("%s — %d vCPU, %s RAM, %s disk", row.Slug, row.VCPU, row.Memory, row.Disk)
	if s.PriceMonthly != nil {
		label += ", " + row.Provider
	}
	if s.Band != nil {
		label += " (GoShip " + row.GoShip + ")"
	}
	return label
}

// rejectMixedFlags refuses flags that only mean something on the other path,
// rather than silently ignoring them.
func rejectMixedFlags(cmd *cobra.Command, cloud bool) error {
	other, only := []string{"port", "ssh-user", "ssh-key"}, "--host"
	if !cloud {
		other, only = []string{"region", "size"}, "--cloud"
	}
	for _, name := range other {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s only applies with %s", name, only)
		}
	}
	return nil
}

// pick shows items numbered on stderr and reads a choice from stdin.
func pick[T any](cmd *cobra.Command, label string, items []T, show func(T) string) (T, error) {
	var zero T
	if len(items) == 0 {
		return zero, fmt.Errorf("no %s available", label)
	}
	errw := cmd.ErrOrStderr()
	fmt.Fprintf(errw, "%s:\n", label)
	for i, it := range items {
		fmt.Fprintf(errw, "  %2d) %s\n", i+1, show(it))
	}
	ans, err := prompt(bufio.NewReader(cmd.InOrStdin()), errw, fmt.Sprintf("Choose a %s [1-%d]: ", label, len(items)))
	if err != nil {
		return zero, err
	}
	n, err := strconv.Atoi(ans)
	if err != nil || n < 1 || n > len(items) {
		return zero, fmt.Errorf("invalid choice %q", ans)
	}
	return items[n-1], nil
}

// maxPollRetries is how many transient failures in a row a polling loop
// rides out before it gives up.
const maxPollRetries = 3

// pollRetrier lets a polling loop ride out a flaky network or a 5xx from the
// API, while anything the API refused outright (a 4xx) still ends it at once.
type pollRetrier struct {
	errw   io.Writer
	failed int
}

// retry reports whether err is worth another poll, noting it on stderr.
func (r *pollRetrier) retry(err error) bool {
	if !transient(err) || r.failed >= maxPollRetries {
		return false
	}
	r.failed++
	fmt.Fprintf(r.errw, "%v; retrying…\n", err)
	return true
}

func (r *pollRetrier) ok() { r.failed = 0 }

func transient(err error) bool {
	var apiErr *portal.Error
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 500
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// errFollowTimeout is the cause a follow's context carries when --timeout,
// not Ctrl-C, ended it.
var errFollowTimeout = errors.New("follow timed out")

type followOpts struct {
	out, errw io.Writer
	verbose   bool
	org, id   string
	name      string
	timeout   time.Duration
}

// followNode polls a newly created node until it comes up or fails, driving a
// progress view the same way a deploy's steps are drawn: one line per stage.
// Ending the follow early — Ctrl-C or --timeout — leaves the node
// provisioning, so neither marks the running stage failed.
func followNode(ctx context.Context, client *portal.Client, o followOpts) error {
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, o.timeout, errFollowTimeout)
		defer cancel()
	}

	p := newProgress(o.out, o.verbose)
	retrier := &pollRetrier{errw: o.errw}
	last := ""
	ticker := time.NewTicker(nodePollInterval)
	defer ticker.Stop()

	stopped := func() error {
		p.Finish(portal.ErrStreamCut)
		if errors.Is(context.Cause(ctx), errFollowTimeout) {
			return fmt.Errorf("stopped following node %s after %s (--timeout); it keeps provisioning — run `goship node info %s`", o.name, o.timeout, o.name)
		}
		fmt.Fprintf(o.errw, "Stopped following; the node keeps provisioning — run `goship node info %s`\n", o.name)
		return nil
	}

	for {
		n, err := client.Node(ctx, o.org, o.id)
		switch {
		case err != nil && ctx.Err() != nil:
			return stopped()
		case err != nil && retrier.retry(err):
		case err != nil:
			p.Finish(err)
			return err
		default:
			retrier.ok()
			if n.Stage != last {
				if last != "" {
					p.Done(last, "")
				}
				if n.Stage != "" {
					p.Begin(n.Stage)
				}
				last = n.Stage
			}

			switch n.Status {
			case "active":
				p.Done(last, "")
				p.Finish(nil)
				fmt.Fprintf(o.out, "Node %s is ready. Pool %s.\n", n.Name, n.PoolName)
				return nil
			case "error":
				p.Finish(errors.New(n.ErrorMessage))
				// The reason belongs in the follow output itself, not only in
				// the error cobra prints once the command unwinds.
				if n.ErrorMessage != "" {
					fmt.Fprintln(o.out, n.ErrorMessage)
				}
				return nodeFailure(n)
			}
		}

		select {
		case <-ctx.Done():
			return stopped()
		case <-ticker.C:
		}
	}
}

func nodeFailure(n *portal.Node) error {
	msg := "node " + n.Name + " failed"
	if n.Stage != "" {
		msg += " while " + stageTitle(n.Stage)
	}
	if n.ErrorMessage != "" {
		msg += ": " + n.ErrorMessage
	}
	return errors.New(msg)
}

// stageTitle is the reader-facing name for one of a node's provisioning
// stages — the same words progress.Begin/Done show while it runs.
func stageTitle(stage string) string {
	if t, ok := stepTitles[stage]; ok {
		return t
	}
	return stage
}
