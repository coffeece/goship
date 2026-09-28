package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coffeece/goship/internal/oauthlogin"
	"github.com/coffeece/goship/internal/portal"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// openURL is a package var so tests can stand in for the browser.
var openURL = oauthlogin.OpenBrowser

// ticketPollInterval is how often `cloud connect` checks an OAuth ticket's
// outcome; a package var so tests don't wait for the real thing.
var ticketPollInterval = 2 * time.Second

// oauthConnectTimeout bounds how long a person is made to wait on the
// browser round trip before the CLI gives up.
const oauthConnectTimeout = 10 * time.Minute

func newCloudCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Connect the clouds your nodes run on",
	}

	providers := &cobra.Command{
		Use:   "providers",
		Short: "List the cloud providers GoShip can connect",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := app.Portal().CloudProviders(cmd.Context())
			if err != nil {
				return err
			}
			return app.Renderer().Render(list)
		},
	}

	list := newOrgListCmd(app, "list", "List cloud accounts connected in the current organization, or --all of them", (*portal.Client).CloudAccounts)

	cmd.AddCommand(providers, list, newCloudConnectCmd(app), newCloudRegionsCmd(app), newCloudSizesCmd(app), newCloudDisconnectCmd(app))
	return cmd
}

func newCloudConnectCmd(app *App) *cobra.Command {
	var label string
	var apiKeyStdin bool

	cmd := &cobra.Command{
		Use:   "connect <provider>",
		Short: "Connect a cloud account",
		Long: "Connects a cloud account so `goship node create --cloud` can provision\n" +
			"machines on it. An OAuth provider opens the browser; an API-key provider\n" +
			"reads the key from stdin (--api-key-stdin) or prompts for it.",
		Args: cobra.ExactArgs(1),
	}
	cmd.Flags().StringVar(&label, "label", "", "name for the account (defaults to the provider's name)")
	cmd.Flags().BoolVar(&apiKeyStdin, "api-key-stdin", false, "read the API key from stdin instead of prompting")

	cmd.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		p, err := findCloudProvider(cmd.Context(), app.Portal(), args[0])
		if err != nil {
			return err
		}
		switch p.Status {
		case "soon":
			return fmt.Errorf("%s is coming soon", p.Label)
		case "unavailable":
			return fmt.Errorf("%s is not available right now on this GoShip; ask the operator", p.Label)
		}

		accountLabel := label
		if accountLabel == "" {
			accountLabel = p.Label
		}

		switch p.Kind {
		case "oauth":
			return connectCloudOAuth(cmd, app, org, p)
		case "api_key":
			return connectCloudAPIKey(cmd, app, org, p, accountLabel, apiKeyStdin)
		default:
			return fmt.Errorf("cloud provider %s has an unsupported connection kind %q", p.Name, p.Kind)
		}
	})
	return cmd
}

func findCloudProvider(ctx context.Context, client *portal.Client, name string) (*portal.CloudProvider, error) {
	providers, err := client.CloudProviders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range providers {
		if providers[i].Name == name {
			return &providers[i], nil
		}
	}
	return nil, fmt.Errorf("unknown cloud provider %q", name)
}

// connectCloudOAuth drives the browser round trip: open the authorize URL,
// then poll the ticket the API handed back until it settles or the 10-minute
// timeout (or Ctrl-C) ends the wait first.
func connectCloudOAuth(cmd *cobra.Command, app *App, org string, p *portal.CloudProvider) error {
	ctx, stop := interruptible(cmd.Context())
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, oauthConnectTimeout)
	defer cancel()

	authorizeURL, ticket, err := app.Portal().BeginCloudOAuth(ctx, org, p.Name)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Opening your browser to connect %s. If nothing opens, visit:\n\n  %s\n", p.Label, authorizeURL)
	_ = openURL(authorizeURL)

	ticker := time.NewTicker(ticketPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting to connect %s: %w", p.Label, ctx.Err())
		case <-ticker.C:
			t, err := app.Portal().CloudConnectTicket(ctx, org, ticket)
			if err != nil {
				return err
			}
			switch t.Status {
			case "done":
				return app.Renderer().Message("Connected %s as %q.", p.Label, t.Account.Label)
			case "failed":
				if t.Error != "" {
					return errors.New(t.Error)
				}
				return fmt.Errorf("connecting %s failed", p.Label)
			}
		}
	}
}

func connectCloudAPIKey(cmd *cobra.Command, app *App, org string, p *portal.CloudProvider, label string, stdinKey bool) error {
	apiKey, err := readCloudAPIKey(cmd, stdinKey)
	if err != nil {
		return err
	}
	account, err := app.Portal().ConnectCloudAPIKey(cmd.Context(), org, p.Name, label, apiKey)
	if err != nil {
		return err
	}
	return app.Renderer().Message("Connected %s as %q.", p.Label, account.Label)
}

// readCloudAPIKey never lets the key touch the terminal's scrollback: it
// comes from stdin under --api-key-stdin, or a hidden prompt on a TTY.
// Neither is available, it says so instead of guessing.
func readCloudAPIKey(cmd *cobra.Command, stdinKey bool) (string, error) {
	if stdinKey {
		line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}
	in, _ := cmd.InOrStdin().(*os.File)
	if in == nil || !term.IsTerminal(int(in.Fd())) {
		return "", errors.New("pass --api-key-stdin")
	}
	return promptPassword(bufio.NewReader(in), in, cmd.ErrOrStderr())
}

func newCloudRegionsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "regions <account>",
		Short: "List the regions available to a cloud account",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			account, err := resolveCloudAccount(cmd.Context(), app.Portal(), org, args[0])
			if err != nil {
				return err
			}
			regions, err := app.Portal().CloudRegions(cmd.Context(), org, account.ID)
			if err != nil {
				return err
			}
			return app.Renderer().Render(regions)
		}),
	}
}

// sizeRow is the table view of a portal.CloudSize. Memory, disk and both
// prices are pre-formatted strings rather than the raw numeric or pointer
// fields, so "no price" (a nil pointer) reads as the same "-" the renderer
// already uses for pointers, without also swallowing a genuinely zero one.
type sizeRow struct {
	Slug     string `table:"SLUG"`
	VCPU     int    `table:"VCPU"`
	Memory   string `table:"MEMORY"`
	Disk     string `table:"DISK"`
	Provider string `table:"PROVIDER"`
	GoShip   string `table:"GOSHIP"`
}

func newCloudSizesCmd(app *App) *cobra.Command {
	var region string
	cmd := &cobra.Command{
		Use:   "sizes <account> --region <slug>",
		Short: "List the machine sizes available to a cloud account, with prices",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringVar(&region, "region", "", "region slug (required, see `goship cloud regions`)")
	_ = cmd.MarkFlagRequired("region")

	cmd.RunE = orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
		account, err := resolveCloudAccount(cmd.Context(), app.Portal(), org, args[0])
		if err != nil {
			return err
		}
		sizes, err := app.Portal().CloudSizes(cmd.Context(), org, account.ID, region)
		if err != nil {
			return err
		}
		rows := make([]sizeRow, len(sizes))
		for i, s := range sizes {
			rows[i] = newSizeRow(s)
		}
		return app.Renderer().Render(rows)
	})
	return cmd
}

func newSizeRow(s portal.CloudSize) sizeRow {
	provider := "-"
	if s.PriceMonthly != nil {
		provider = fmt.Sprintf("%s %s/mo", currencySymbol(s.Currency), formatAmount(*s.PriceMonthly))
	}
	goship := "-"
	if s.Band != nil {
		goship = fmt.Sprintf("R$ %s/mo", formatCents(s.Band.PriceCents))
	}
	return sizeRow{
		Slug:     s.Slug,
		VCPU:     s.VCPU,
		Memory:   fmt.Sprintf("%d GB", s.MemoryMB/1024),
		Disk:     fmt.Sprintf("%d GB", s.DiskGB),
		Provider: provider,
		GoShip:   goship,
	}
}

func currencySymbol(currency string) string {
	switch currency {
	case "USD":
		return "US$"
	case "EUR":
		return "€"
	default:
		return currency
	}
}

// formatAmount shows decimals only when the price is not a whole number, so
// a catalog of "US$ 48/mo" and "US$ 39.90/mo" doesn't carry ".00" everywhere.
func formatAmount(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

// formatCents renders integer cents as pt-BR currency digits: 9900 -> "99,00".
func formatCents(cents int) string {
	return fmt.Sprintf("%d,%02d", cents/100, cents%100)
}

func newCloudDisconnectCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <account>",
		Short: "Disconnect a cloud account",
		Args:  cobra.ExactArgs(1),
		RunE: orgRunE(app, func(cmd *cobra.Command, org string, args []string) error {
			account, err := resolveCloudAccount(cmd.Context(), app.Portal(), org, args[0])
			if err != nil {
				return err
			}
			if err := confirm(cmd, app.Global.Yes, "Disconnect cloud account %q?", account.Label); err != nil {
				return err
			}
			if err := app.Portal().DeleteCloudAccount(cmd.Context(), org, account.ID); err != nil {
				return err
			}
			return app.Renderer().Message("Cloud account %s disconnected.", account.Label)
		}),
	}
}

// resolveCloudAccount turns what a person types into the account the API
// keys on. It matches, in order: the id, then an exact label, then the
// provider name — but only when exactly one account uses that provider.
func resolveCloudAccount(ctx context.Context, client *portal.Client, org, ref string) (*portal.CloudAccount, error) {
	accounts, err := client.CloudAccounts(ctx, org)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if accounts[i].ID == ref {
			return &accounts[i], nil
		}
	}
	for i := range accounts {
		if accounts[i].Label == ref {
			return &accounts[i], nil
		}
	}

	var byProvider []portal.CloudAccount
	for _, a := range accounts {
		if a.Provider == ref {
			byProvider = append(byProvider, a)
		}
	}
	switch len(byProvider) {
	case 1:
		return &byProvider[0], nil
	case 0:
		return nil, fmt.Errorf("no cloud account %q in org %q; run `goship cloud connect <provider>`", ref, org)
	default:
		labels := make([]string, len(byProvider))
		for i, a := range byProvider {
			labels[i] = a.Label
		}
		return nil, fmt.Errorf("more than one %s account in org %q: %s; pass the account's id or label", ref, org, strings.Join(labels, ", "))
	}
}
