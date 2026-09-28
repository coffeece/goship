package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
// failing outright. A package var so tests, whose stdin is a strings.Reader,
// can force it either way.
var stdinIsTTY = func(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func newNodeCreateCmd(app *App) *cobra.Command {
	var cloudRef, region, size, host, sshUser, sshKeyFile string
	var port int
	var noWait bool

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

			// A single bufio.Reader shared for the life of the command: a fresh
			// one per prompt would swallow whatever the previous prompt's read
			// already buffered off stdin (see prompt's doc comment), which would
			// lose the answer to --size right after --region was asked.
			// bufio.NewReader is a no-op wrap once this has already run once,
			// since it returns the same *bufio.Reader back when it is already
			// one of sufficient size.
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
					if !stdinIsTTY(cmd) {
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
					if !stdinIsTTY(cmd) {
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
					"Node %s requested; not waiting for it to come up (--no-wait). `goship node info %s` shows progress.",
					created.Name, created.Name)
			}

			ctx, stop := interruptible(cmd.Context())
			defer stop()
			return followNode(ctx, app.Portal(), cmd.OutOrStdout(), app.Global.Verbose, org, created.ID)
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

	return cmd
}

// regionChoiceLabel is a portal.CloudRegion as one line of a `pick` prompt.
func regionChoiceLabel(r portal.CloudRegion) string {
	return fmt.Sprintf("%s — %s, %s", r.Slug, r.Label, r.Country)
}

// sizeChoiceLabel is a portal.CloudSize as one line of a `pick` prompt,
// reusing the same row shown by `goship cloud sizes` so the two never drift.
func sizeChoiceLabel(s portal.CloudSize) string {
	row := newSizeRow(s)
	return fmt.Sprintf("%s — %d vCPU, %s RAM, %s disk, %s/mo (GoShip %s/mo)", row.Slug, row.VCPU, row.Memory, row.Disk, row.Provider, row.GoShip)
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

// followNode polls a newly created node until it comes up or fails, driving a
// progress view the same way a deploy's steps are drawn: one line per stage.
func followNode(ctx context.Context, client *portal.Client, out io.Writer, verbose bool, org, id string) error {
	p := newProgress(out, verbose)
	last := ""
	ticker := time.NewTicker(nodePollInterval)
	defer ticker.Stop()

	for {
		n, err := client.Node(ctx, org, id)
		if err != nil {
			p.Finish(err)
			return err
		}

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
			fmt.Fprintf(out, "Node %s is ready. Pool %s.\n", n.Name, n.PoolName)
			return nil
		case "error":
			p.Finish(errors.New(n.ErrorMessage))
			// The reason belongs in the follow output itself, not only in the
			// error cobra prints once the command unwinds.
			fmt.Fprintln(out, n.ErrorMessage)
			return fmt.Errorf("node %s failed while %s: %s", n.Name, stageTitle(n.Stage), n.ErrorMessage)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// stageTitle is the reader-facing name for one of a node's provisioning
// stages — the same words progress.Begin/Done show while it runs.
func stageTitle(stage string) string {
	if t, ok := stepTitles[stage]; ok {
		return t
	}
	return stage
}
