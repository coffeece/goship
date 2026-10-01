package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/coffeece/goship/internal/oauthlogin"
	"github.com/coffeece/goship/internal/portal"
	"github.com/coffeece/goship/internal/render"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// openURL is a package var so tests can stand in for the browser.
var openURL = oauthlogin.OpenBrowser

// ticketPollInterval is how often `cloud connect` checks an OAuth ticket's
// outcome; a package var so tests don't wait for the real thing.
var ticketPollInterval = 2 * time.Second

// oauthConnectTimeout bounds how long a person is made to wait on the
// browser round trip before the CLI gives up; a package var so tests don't
// wait for the real thing.
var oauthConnectTimeout = 10 * time.Minute

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

	// Only an org's admins may see its cloud accounts, so --all skips the orgs
	// where you are a member rather than failing on the first.
	list := newOrgListCmd(app, "list", "List cloud accounts connected in the current organization, or --all of them", (*portal.Client).CloudAccounts, skipForbiddenOrgs)

	cmd.AddCommand(providers, list, newCloudConnectCmd(app), newCloudRegionsCmd(app), newCloudSizesCmd(app), newCloudDisconnectCmd(app))
	return cmd
}

func newCloudConnectCmd(app *App) *cobra.Command {
	var label string
	var apiKeyStdin bool
	var roleARN string
	var accessKeysStdin bool

	cmd := &cobra.Command{
		Use:   "connect <provider>",
		Short: "Connect a cloud account",
		Long: "Connects a cloud account so `goship node create --cloud` can provision\n" +
			"machines on it. An OAuth provider opens the browser; an API-key provider\n" +
			"reads the key from stdin (--api-key-stdin) or prompts for it.\n" +
			"AWS with no flags opens the console with the GoShip stack prefilled and waits for it\n" +
			"(when this GoShip supports it); otherwise it connects through an IAM role (--role-arn,\n" +
			"or a guided flow in a terminal) or, as a fallback, access keys read from stdin (--access-keys-stdin).",
		Args: cobra.ExactArgs(1),
	}
	cmd.Flags().StringVar(&label, "label", "", "name for the account (defaults to the provider's name; for AWS, the account id)")
	cmd.Flags().BoolVar(&apiKeyStdin, "api-key-stdin", false, "read the API key from stdin instead of prompting")
	cmd.Flags().StringVar(&roleARN, "role-arn", "", "AWS: the RoleArn output of the GoShip CloudFormation stack")
	cmd.Flags().BoolVar(&accessKeysStdin, "access-keys-stdin", false, "AWS: read the access key id and secret from stdin, one per line")

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
			if label != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "--label ignored: label is set from your %s team; rename it in the dashboard.\n", p.Label)
			}
			return connectCloudOAuth(cmd, app, org, p)
		case "api_key":
			return connectCloudAPIKey(cmd, app, org, p, accountLabel, apiKeyStdin)
		case "federated":
			if p.Name == "aws" {
				return connectCloudAWS(cmd, app, org, p, label, roleARN, accessKeysStdin)
			}
			return fmt.Errorf("cloud provider %s has an unsupported connection kind %q", p.Name, p.Kind)
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
	fmt.Fprintf(cmd.ErrOrStderr(), "Opening your browser to connect %s. If nothing opens, visit:\n\n  %s\n\n"+
		"Sign in to GoShip in that browser if asked, then open the link again.\n", p.Label, authorizeURL)
	_ = openURL(authorizeURL)
	return waitForTicket(ctx, cmd, app, org, p, ticket)
}

// waitForTicket polls a connect ticket until it settles; ctx carries the
// caller's deadline and Ctrl-C. "pending" and "connecting" both mean keep
// waiting.
func waitForTicket(ctx context.Context, cmd *cobra.Command, app *App, org string, p *portal.CloudProvider, ticket string) error {
	// The deadline or Ctrl-C can land mid-request, so the HTTP call fails
	// with its own wrapped context error rather than the select below ever
	// seeing ctx.Done(); both are reported the same way.
	ended := func() error {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out waiting to connect %s: %w", p.Label, ctx.Err())
		}
		return fmt.Errorf("cancelled connecting %s", p.Label)
	}

	retrier := &pollRetrier{errw: cmd.ErrOrStderr()}
	ticker := time.NewTicker(ticketPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ended()
		case <-ticker.C:
			t, err := app.Portal().CloudConnectTicket(ctx, org, ticket)
			switch {
			case err != nil && ctx.Err() != nil:
				return ended()
			case err != nil && retrier.retry(err):
				continue
			case err != nil:
				return err
			}
			retrier.ok()
			switch t.Status {
			case "done":
				if t.Account == nil {
					return app.Renderer().Message("Connected %s.", p.Label)
				}
				if app.Global.Output == render.JSON {
					return app.Renderer().Render(t.Account)
				}
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
	apiKey, err := readCloudAPIKey(cmd, p, stdinKey)
	if err != nil {
		return err
	}
	if apiKey == "" {
		return errors.New("api key is empty")
	}
	account, err := app.Portal().ConnectCloudAPIKey(cmd.Context(), org, p.Name, label, apiKey)
	if err != nil {
		return err
	}
	return reportConnected(cmd, app, p, account)
}

// readCloudAPIKey never lets the key touch the terminal's scrollback: on a
// terminal it is a hidden prompt (even under --api-key-stdin, since a
// terminal is not a pipe), otherwise it is read from stdin under
// --api-key-stdin. Neither is available, it says so instead of guessing.
func readCloudAPIKey(cmd *cobra.Command, p *portal.CloudProvider, stdinKey bool) (string, error) {
	if in, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(in.Fd())) {
		errw := cmd.ErrOrStderr()
		if p.KeyDocsURL != "" {
			fmt.Fprintf(errw, "Create a %s API key at %s\n", p.Label, p.KeyDocsURL)
		}
		fmt.Fprintf(errw, "%s API key: ", p.Label)
		data, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(errw)
		return strings.TrimSpace(string(data)), err
	}
	if !stdinKey {
		return "", errors.New("pass --api-key-stdin")
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
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
		// JSON is for scripts: the raw numbers, not the table's display strings.
		if app.Global.Output == render.JSON {
			return app.Renderer().Render(sizes)
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
		if providers, err := client.CloudProviders(ctx); err == nil {
			for _, p := range providers {
				if p.Name == ref {
					return nil, fmt.Errorf("no %s account connected; run `goship cloud connect %s`", p.Label, p.Name)
				}
			}
		}
		return nil, fmt.Errorf("no cloud account %q in org %q; run `goship cloud connect <provider>`", ref, org)
	default:
		labels := make([]string, len(byProvider))
		for i, a := range byProvider {
			labels[i] = a.Label
		}
		return nil, fmt.Errorf("more than one %s account in org %q: %s; pass the account's id or label", ref, org, strings.Join(labels, ", "))
	}
}

// connectCloudAWS connects AWS. Flags win; in a terminal with no flag it
// walks the person through the role path (browser + pasted ARN) or, when
// this GoShip cannot assume roles, prompts for access keys.
func connectCloudAWS(cmd *cobra.Command, app *App, org string, p *portal.CloudProvider, label, roleARN string, keysStdin bool) error {
	errw := cmd.ErrOrStderr()
	roleARN = bareARN(roleARN)
	if roleARN != "" && keysStdin {
		return errors.New("pass --role-arn or --access-keys-stdin, not both")
	}

	connectRole := func(arn string) error {
		account, err := app.Portal().ConnectCloudAWSRole(cmd.Context(), org, label, arn)
		if err != nil {
			return err
		}
		return reportConnected(cmd, app, p, account)
	}
	connectKeys := func(id, secret string) error {
		account, err := app.Portal().ConnectCloudAWSKeys(cmd.Context(), org, label, id, secret)
		if err != nil {
			return err
		}
		return reportConnected(cmd, app, p, account)
	}

	switch {
	case roleARN != "":
		return connectRole(roleARN)
	case keysStdin:
		id, secret, err := readAccessKeys(cmd, true)
		if err != nil {
			return err
		}
		return connectKeys(id, secret)
	}

	if !stdinIsTTY(cmd) {
		return errors.New("pass --role-arn (the stack's RoleArn output) or --access-keys-stdin")
	}
	setup, err := app.Portal().CloudAWSSetup(cmd.Context(), org)
	if err != nil {
		return err
	}
	if setup != nil && setup.OneClick {
		// The stack names the account; the begin call takes no label.
		if label != "" {
			return errors.New("--label only works with --role-arn or --access-keys-stdin")
		}
		return connectCloudAWSOneClick(cmd, app, org, p, setup)
	}

	if setup == nil {
		fmt.Fprintln(errw, "IAM role connect is not available on this GoShip; using access keys.")
		id, secret, err := readAccessKeys(cmd, false)
		if err != nil {
			return err
		}
		return connectKeys(id, secret)
	}

	fmt.Fprintf(errw, "Opening the AWS console to create the GoShip role. If nothing opens, visit:\n\n  %s\n\n"+
		"External ID: %s\nGoShip account: %s\n\nCreate the stack, then paste its RoleArn output here.\n", setup.LaunchURL, setup.ExternalID, setup.GoShipAccountID)
	_ = openURL(setup.LaunchURL)
	fmt.Fprint(errw, "Role ARN: ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("reading the role ARN: %w", err)
	}
	arn := bareARN(line)
	if arn == "" {
		return errors.New("role ARN is empty")
	}
	return connectRole(arn)
}

var awsConnectTimeout = 30 * time.Minute

// connectCloudAWSOneClick opens the console with the GoShip stack prefilled
// and waits for the stack to report back — nothing to paste.
func connectCloudAWSOneClick(cmd *cobra.Command, app *App, org string, p *portal.CloudProvider, setup *portal.AWSSetup) error {
	ctx, stop := interruptible(cmd.Context())
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, awsConnectTimeout)
	defer cancel()

	launchURL, ticket, err := app.Portal().BeginCloudAWSConnect(ctx, org)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Opening the AWS console. If nothing opens, visit:\n\n  %s\n\n"+
		"Tick \"I acknowledge that AWS CloudFormation might create IAM resources with custom names\" and click Create stack.\n"+
		"The stack lives in Ohio (us-east-2); the role works in every region.\n"+
		"Your company blocks us-east-2? Create the stack in another region: %s\n"+
		"and run goship cloud connect aws --role-arn <RoleArn>.\n\n"+
		"Waiting for AWS (about a minute)…\n", launchURL, setup.LaunchURL)
	_ = openURL(launchURL)
	return waitForTicket(ctx, cmd, app, org, p, ticket)
}

// reportConnected prints the outcome the way the other connect paths do.
func reportConnected(cmd *cobra.Command, app *App, p *portal.CloudProvider, account *portal.CloudAccount) error {
	if app.Global.Output == render.JSON {
		return app.Renderer().Render(account)
	}
	return app.Renderer().Message("Connected %s as %q.", p.Label, account.Label)
}

// bareARN takes the ARN out of whatever was pasted: a bare ARN, or a whole
// "RoleArn  arn:aws:..." row copied from the stack's Outputs tab.
func bareARN(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	for _, f := range fields {
		if strings.HasPrefix(f, "arn:aws:iam::") {
			return f
		}
	}
	return fields[len(fields)-1]
}

// readAccessKeys reads the key id and the secret: hidden prompts on a
// terminal, else exactly two lines from stdin under --access-keys-stdin.
func readAccessKeys(cmd *cobra.Command, stdin bool) (id, secret string, err error) {
	if in, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(in.Fd())) {
		errw := cmd.ErrOrStderr()
		fmt.Fprint(errw, "AWS access key ID: ")
		idBytes, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(errw)
		if err != nil {
			return "", "", err
		}
		fmt.Fprint(errw, "AWS secret access key: ")
		secretBytes, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(errw)
		if err != nil {
			return "", "", err
		}
		id, secret = strings.TrimSpace(string(idBytes)), strings.TrimSpace(string(secretBytes))
	} else {
		if !stdin {
			return "", "", errors.New("pass --access-keys-stdin")
		}
		sc := bufio.NewScanner(cmd.InOrStdin())
		var lines []string
		for sc.Scan() && len(lines) < 2 {
			lines = append(lines, strings.TrimSpace(sc.Text()))
		}
		if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
			return "", "", errors.New("expected two lines on stdin: the access key id, then the secret access key")
		}
		id, secret = lines[0], lines[1]
	}
	if id == "" || secret == "" {
		return "", "", errors.New("the access key id and the secret access key are both required")
	}
	return id, secret, nil
}
